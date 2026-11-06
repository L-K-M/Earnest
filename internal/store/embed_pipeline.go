package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// EmbedEntry is publisher-only input. Its owner partitions private cache keys.
type EmbedEntry struct {
	ID          int64
	OwnerUserID *int64
	Title       string
	Text        string
	ContentHash []byte
}

// EmbedPending returns bounded backfill work without ever joining annotations.
func (s *Store) EmbedPending(ctx context.Context, model string, limit int) ([]EmbedEntry, error) {
	return s.pipelinePending(ctx, `NOT EXISTS(SELECT 1 FROM embeddings x WHERE x.entry_id=e.id AND x.model=$1 AND x.content_hash=e.content_hash AND x.input_title=e.title)`, model, "", limit)
}

// ClassifyPending returns current entries not classified with this prompt/model,
// excluding failed inputs until their durable retry delay has elapsed.
func (s *Store) ClassifyPending(ctx context.Context, model, version string, limit int) ([]EmbedEntry, error) {
	return s.pipelinePending(ctx, `NOT EXISTS(SELECT 1 FROM classifier_results x WHERE x.entry_id=e.id AND x.model=$1 AND x.prompt_version=$3 AND x.content_hash=e.content_hash AND x.input_title=e.title)
 AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.unique_key='classify/'||e.id::text AND j.kind='classify'
 AND j.payload->>'model'=$1 AND j.payload->>'prompt_version'=$3
 AND j.payload->>'content_hash'=encode(e.content_hash,'hex') AND j.payload->>'input_title'=e.title
 AND j.run_at>now())`, model, version, limit)
}

func (s *Store) pipelinePending(ctx context.Context, predicate, model, version string, limit int) ([]EmbedEntry, error) {
	// The version parameter is referenced even for embedding jobs so argument
	// types stay fixed without interpolating any caller-supplied SQL.
	rows, err := s.pool.Query(ctx, `SELECT e.id,f.owner_user_id,e.title,c.text,e.content_hash
 FROM entries e JOIN feeds f ON f.id=e.feed_id
 JOIN LATERAL(SELECT text FROM contents WHERE entry_id=e.id AND content_hash=e.content_hash ORDER BY (source='extracted') DESC,sanitizer_version DESC LIMIT 1)c ON true
 WHERE e.deleted_at IS NULL AND (f.owner_user_id IS NULL OR f.llm_allowed)
 AND EXISTS(SELECT 1 FROM subscriptions s WHERE s.feed_id=f.id AND s.deleted_at IS NULL)
 AND $3::text IS NOT NULL AND `+predicate+` ORDER BY e.seq LIMIT $2`, model, limit, version)
	if err != nil {
		return nil, fmt.Errorf("load pipeline work: %w", err)
	}
	defer rows.Close()
	out := []EmbedEntry{}
	for rows.Next() {
		var e EmbedEntry
		if err := rows.Scan(&e.ID, &e.OwnerUserID, &e.Title, &e.Text, &e.ContentHash); err != nil {
			return nil, fmt.Errorf("scan pipeline work: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EmbedSave atomically stores a vector only while its source version is current.
func (s *Store) EmbedSave(ctx context.Context, e EmbedEntry, model string, inputHash []byte, dims int, vector []float32) error {
	literal, err := formatHalfvec(vector)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO embeddings(entry_id,model,dims,embedding,content_hash,input_hash,input_title)
 SELECT e.id,$2,$3,$4::halfvec,$5,$6,$7 FROM entries e JOIN feeds f ON f.id=e.feed_id
 WHERE e.id=$1 AND e.content_hash=$5 AND e.title=$7 AND e.deleted_at IS NULL AND (f.owner_user_id IS NULL OR f.llm_allowed)
 ON CONFLICT(entry_id,model) DO UPDATE SET dims=EXCLUDED.dims,embedding=EXCLUDED.embedding,content_hash=EXCLUDED.content_hash,input_hash=EXCLUDED.input_hash,input_title=EXCLUDED.input_title,created_at=now()`, e.ID, model, dims, literal, e.ContentHash, inputHash, e.Title)
	if err != nil {
		return fmt.Errorf("save pipeline embedding: %w", err)
	}
	return nil
}

// ClassifySave persists only allowlisted labels for the still-current source.
func (s *Store) ClassifySave(ctx context.Context, e EmbedEntry, model, version string, inputHash []byte, labels []string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO classifier_results(entry_id,model,prompt_version,content_hash,input_hash,input_title,labels)
 SELECT e.id,$2,$3,$4,$5,$6,$7 FROM entries e JOIN feeds f ON f.id=e.feed_id WHERE e.id=$1 AND e.content_hash=$4 AND e.title=$6 AND e.deleted_at IS NULL AND (f.owner_user_id IS NULL OR f.llm_allowed)
 ON CONFLICT(entry_id,model,prompt_version) DO UPDATE SET content_hash=EXCLUDED.content_hash,input_hash=EXCLUDED.input_hash,input_title=EXCLUDED.input_title,labels=EXCLUDED.labels,created_at=now()`, e.ID, model, version, e.ContentHash, inputHash, e.Title, labels)
	if err != nil {
		return fmt.Errorf("save classifier result: %w", err)
	}
	return nil
}

// EmbedConsent rechecks consent immediately before sending publisher input.
func (s *Store) EmbedConsent(ctx context.Context, id int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entries e JOIN feeds f ON f.id=e.feed_id WHERE e.id=$1 AND e.deleted_at IS NULL AND (f.owner_user_id IS NULL OR f.llm_allowed))`, id).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("check provider consent: %w", err)
	}
	return ok, nil
}

// SemanticQueryGet returns an already embedded user description.
func (s *Store) SemanticQueryGet(ctx context.Context, user int64, description, model string) ([]float32, bool, error) {
	var literal string
	err := s.pool.QueryRow(ctx, `SELECT embedding::text FROM semantic_queries WHERE user_id=$1 AND description=$2 AND model=$3`, user, description, model).Scan(&literal)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read semantic query: %w", err)
	}
	vector, err := parseHalfvec(literal)
	return vector, true, err
}

// SemanticQueryPut saves a description vector, partitioned by user and model.
func (s *Store) SemanticQueryPut(ctx context.Context, user int64, description, model string, vector []float32) error {
	literal, err := formatHalfvec(vector)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO semantic_queries(user_id,description,model,embedding) VALUES($1,$2,$3,$4::halfvec) ON CONFLICT DO NOTHING`, user, description, model, literal)
	if err != nil {
		return fmt.Errorf("save semantic query: %w", err)
	}
	return nil
}

// RetrievalTextLimit bounds the text projection of retrieval queries.
// Consumers render short snippets (the service keeps 200 runes), and whole
// article texts must not leave the database to build them.
const RetrievalTextLimit = 2048

// SemanticHit is publisher content with an independently computed cosine score.
type SemanticHit struct {
	ID, SubscriptionID, FeedID, Seq int64
	Title, URL, Text                string
	Score                           float64
}

// SemanticSearch applies subscription and privacy boundaries before ranking.
// The text projection is bounded to RetrievalTextLimit characters.
func (s *Store) SemanticSearch(ctx context.Context, user, subscription, folder, exclude int64, model string, vector []float32, limit int) ([]SemanticHit, error) {
	literal, err := formatHalfvec(vector)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id,s.id,e.feed_id,e.seq,e.title,coalesce(e.url,''),coalesce(left(c.text,2048),''),1-(x.embedding <=> $5::halfvec) AS score
 FROM embeddings x JOIN entries e ON e.id=x.entry_id AND x.content_hash=e.content_hash AND x.input_title=e.title
 JOIN feeds f ON f.id=e.feed_id JOIN subscriptions s ON s.feed_id=e.feed_id AND s.user_id=$1 AND s.deleted_at IS NULL
 LEFT JOIN LATERAL(SELECT text FROM contents WHERE entry_id=e.id AND content_hash=e.content_hash ORDER BY (source='extracted') DESC,sanitizer_version DESC LIMIT 1)c ON true
 WHERE x.model=$2 AND e.deleted_at IS NULL AND e.id<>$6 AND (f.owner_user_id IS NULL OR (f.owner_user_id=$1 AND f.llm_allowed))
 AND ($3::bigint=0 OR s.id=$3) AND ($4::bigint=0 OR s.folder_id=$4)
 ORDER BY x.embedding <=> $5::halfvec,e.seq DESC LIMIT $7`, user, model, subscription, folder, literal, exclude, limit)
	if err != nil {
		return nil, fmt.Errorf("semantic retrieval: %w", err)
	}
	defer rows.Close()
	out := []SemanticHit{}
	for rows.Next() {
		var h SemanticHit
		if err := rows.Scan(&h.ID, &h.SubscriptionID, &h.FeedID, &h.Seq, &h.Title, &h.URL, &h.Text, &h.Score); err != nil {
			return nil, fmt.Errorf("scan semantic hit: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ClassifierLabels returns only the current model's labels at the current hash.
func (s *Store) ClassifierLabels(ctx context.Context, id int64, model, version string) ([]string, bool, error) {
	var labels []string
	err := s.pool.QueryRow(ctx, `SELECT x.labels FROM classifier_results x JOIN entries e ON e.id=x.entry_id AND e.content_hash=x.content_hash AND e.title=x.input_title JOIN feeds f ON f.id=e.feed_id WHERE e.id=$1 AND x.model=$2 AND x.prompt_version=$3 AND (f.owner_user_id IS NULL OR f.llm_allowed)`, id, model, version).Scan(&labels)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read classifier labels: %w", err)
	}
	return labels, true, nil
}

// WatchCheckState is a reservation decision for one watch confirmation.
type WatchCheckState int

const (
	// WatchCheckRun inserted a reservation; make the provider call now.
	WatchCheckRun WatchCheckState = iota
	// WatchCheckCached carries a definite earlier answer in the result.
	WatchCheckCached
	// WatchCheckUncertain: an earlier attempt remains unmatched until the
	// content changes. An uncertain call is not automatically repeated.
	WatchCheckUncertain
	// WatchCheckBudget: the rule's daily check budget is exhausted.
	WatchCheckBudget
)

// WatchCheckReserve accounts attempts before a call, avoiding repeat spending
// after a crash. An uncertain attempt remains unmatched until content changes,
// which the state reports as WatchCheckUncertain so callers can distinguish it
// from budget exhaustion.
func (s *Store) WatchCheckReserve(ctx context.Context, rule, entry int64, hash []byte, model string, budget int, now time.Time) (WatchCheckState, *bool, error) {
	// Defaults to WatchCheckBudget: the budget early-return inside the
	// transaction leaves state untouched when the daily budget is spent.
	state := WatchCheckBudget
	var result *bool
	err := s.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, rule); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `SELECT matched FROM watch_checks WHERE rule_id=$1 AND entry_id=$2 AND content_hash=$3 AND model=$4`, rule, entry, hash, model).Scan(&result)
		if err == nil {
			if result == nil {
				state = WatchCheckUncertain
			} else {
				state = WatchCheckCached
			}
			return nil
		}
		if err != pgx.ErrNoRows {
			return err
		}
		var used int
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM watch_checks WHERE rule_id=$1 AND attempted_at >=$2 AND attempted_at<$3`, rule, start, start.AddDate(0, 0, 1)).Scan(&used); err != nil {
			return err
		}
		if used >= budget {
			state = WatchCheckBudget
			return nil
		}
		_, err = tx.Exec(ctx, `INSERT INTO watch_checks(rule_id,entry_id,content_hash,model,attempted_at) VALUES($1,$2,$3,$4,$5)`, rule, entry, hash, model, now)
		if err == nil {
			state = WatchCheckRun
		}
		return err
	})
	if err != nil {
		return WatchCheckBudget, nil, fmt.Errorf("reserve watch check: %w", err)
	}
	return state, result, nil
}

// EmbedCached reuses identical bounded publisher input within one privacy
// partition. Private vectors are never a cross-user cache.
func (s *Store) EmbedCached(ctx context.Context, owner *int64, inputHash []byte, model string) (int, []float32, bool, error) {
	var dims int
	var literal string
	err := s.pool.QueryRow(ctx, `SELECT x.dims,x.embedding::text FROM embeddings x JOIN entries e ON e.id=x.entry_id JOIN feeds f ON f.id=e.feed_id WHERE x.model=$1 AND x.input_hash=$2 AND f.owner_user_id IS NOT DISTINCT FROM $3::bigint AND (f.owner_user_id IS NULL OR f.llm_allowed) ORDER BY x.entry_id LIMIT 1`, model, inputHash, owner).Scan(&dims, &literal)
	if err == pgx.ErrNoRows {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, fmt.Errorf("read cached embedding: %w", err)
	}
	vector, err := parseHalfvec(literal)
	return dims, vector, true, err
}
