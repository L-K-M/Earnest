package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Wire DTOs mirror api/openapi.json and the service transport shapes. They are
// used for human rendering; --json echoes the server response verbatim so no
// field is lost when the API adds some.

type meResult struct {
	UserID      int64    `json:"user_id"`
	PrincipalID int64    `json:"principal_id"`
	Kind        string   `json:"kind"`
	Scopes      []string `json:"scopes"`
}

type revision struct {
	Generation string `json:"generation"`
	Rev        uint64 `json:"rev"`
}

type deviceStart struct {
	ClientName string `json:"client_name"`
}
type deviceResult struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int64  `json:"interval"`
	ExpiresIn       int64  `json:"expires_in"`
}
type devicePoll struct {
	DeviceCode string `json:"device_code"`
}
type deviceToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

// registerValue is the wire encoding of syncmodel.Value: a JSON boolean for
// the kept register, a string otherwise.
type registerValue struct {
	text    string
	boolean bool
	isBool  bool
}

func (v *registerValue) UnmarshalJSON(data []byte) error {
	if string(data) == "true" || string(data) == "false" {
		v.isBool = true
		v.boolean = string(data) == "true"
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("register value: %w", err)
	}
	v.text = text
	return nil
}

func (v registerValue) String() string {
	if v.isBool {
		if v.boolean {
			return "true"
		}
		return "false"
	}
	return v.text
}

// Bool reports the kept register value, defaulting to false.
func (v registerValue) Bool() bool { return v.isBool && v.boolean }

// MarshalJSON keeps the kept register boolean on the wire.
func (v registerValue) MarshalJSON() ([]byte, error) {
	if v.isBool {
		return json.Marshal(v.boolean)
	}
	return json.Marshal(v.text)
}

const (
	valueUnseen = "unseen"
	valueSeen   = "seen"
	shelfNone   = "none"
	shelfLater  = "later"
	shelfDone   = "done"
)

type register struct {
	Value      registerValue `json:"value"`
	Rev        uint64        `json:"rev"`
	OccurredAt time.Time     `json:"occurred_at"`
	Actor      int64         `json:"actor"`
	Source     string        `json:"source"`
}

type state struct {
	Principal int64    `json:"principal"`
	EntryID   int64    `json:"entry_id"`
	Seen      register `json:"seen"`
	Shelf     register `json:"shelf"`
	Kept      register `json:"kept"`
	StateRev  uint64   `json:"state_rev"`
}

// label summarises the three registers for a table cell.
func (s state) label() string {
	label := "unread"
	switch {
	case s.Shelf.Value.String() == shelfDone:
		label = "done"
	case s.Shelf.Value.String() == shelfLater:
		label = "later"
	case s.Seen.Value.String() == valueSeen:
		label = "read"
	}
	if s.Kept.Value.Bool() {
		label += "+kept"
	}
	return label
}

type entry struct {
	ID             int64      `json:"id"`
	SubscriptionID int64      `json:"subscription_id"`
	FeedID         int64      `json:"feed_id"`
	Seq            uint64     `json:"seq"`
	FirstSeenAt    time.Time  `json:"first_seen_at"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
	Title          string     `json:"title"`
	URL            string     `json:"url"`
	Author         string     `json:"author"`
	Rev            uint64     `json:"rev"`
	Deleted        bool       `json:"deleted"`
	State          state      `json:"state"`
}

// stateResult is one item of a POST /v1/state:batch response.
type stateResult struct {
	OpID   string `json:"op_id"`
	Status string `json:"status"`
	State  state  `json:"state"`
	Replay bool   `json:"replay"`
}

type entriesPage struct {
	Entries    []entry `json:"entries"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

// keptImportReport is the response from POST /v1/kept:import.
type keptImportReport struct {
	Results    []keptImportResult `json:"results"`
	Subscribed []subscription     `json:"subscribed"`
}

// keptImportResult is one article's outcome from a Kept import.
type keptImportResult struct {
	Index          int    `json:"index"`
	Status         string `json:"status"`
	EntryID        int64  `json:"entry_id,omitempty"`
	SubscriptionID int64  `json:"subscription_id,omitempty"`
	FeedURL        string `json:"feed_url,omitempty"`
	Title          string `json:"title,omitempty"`
	Error          string `json:"error,omitempty"`
}

type position struct {
	Principal int64  `json:"principal"`
	View      string `json:"view"`
	Seq       uint64 `json:"seq"`
	Rev       uint64 `json:"rev"`
}

type content struct {
	EntryID          int64  `json:"entry_id"`
	HTML             string `json:"html"`
	Text             string `json:"text"`
	SanitizerVersion int    `json:"sanitizer_version"`
	Source           string `json:"source"`
}

type feedHealth struct {
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	LastAttemptAt       *time.Time `json:"last_attempt_at,omitempty"`
	NextFetchAt         time.Time  `json:"next_fetch_at"`
	ErrorClass          string     `json:"error_class,omitempty"`
	ErrorMessage        string     `json:"error_message,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	HTTPStatus          int        `json:"http_status"`
	PostsPerDay         float64    `json:"posts_per_day"`
	Firehose            bool       `json:"firehose"`
}

type subscription struct {
	InitialSince        *time.Time `json:"initial_since,omitempty"`
	InitialThroughSeq   uint64     `json:"initial_through_seq"`
	InitialFetchPending bool       `json:"initial_fetch_pending"`
	ID                  int64      `json:"id"`
	FeedID              int64      `json:"feed_id"`
	Title               string     `json:"title"`
	URL                 string     `json:"url"`
	SiteURL             string     `json:"site_url"`
	FolderID            int64      `json:"folder_id"`
	Cadence             string     `json:"cadence"`
	Extract             string     `json:"extract"`
	Rev                 uint64     `json:"rev"`
	Deleted             bool       `json:"deleted"`
	Private             bool       `json:"private"`
	Health              feedHealth `json:"health"`
}

type folder struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Rev     uint64 `json:"rev"`
	Deleted bool   `json:"deleted"`
}

type opmlResult struct {
	Path         []int         `json:"path"`
	Subscription *subscription `json:"subscription,omitempty"`
	Folder       *folder       `json:"folder,omitempty"`
	Deferred     bool          `json:"deferred"`
	Error        string        `json:"error,omitempty"`
}

type annotation struct {
	ID          int64   `json:"id"`
	PrincipalID int64   `json:"principal_id"`
	Kind        string  `json:"kind"`
	EntryIDs    []int64 `json:"entry_ids"`
	Value       string  `json:"value"`
	Rev         int64   `json:"rev"`
	Untrusted   bool    `json:"untrusted"`
	Deleted     bool    `json:"deleted"`
}

type notificationBudget struct {
	UsedToday  int `json:"used_today"`
	LimitToday int `json:"limit_today"`
}

type notificationWhy struct {
	Sender         string             `json:"sender"`
	Budget         notificationBudget `json:"budget"`
	MatchingRuleID int64              `json:"matching_rule_id,omitempty"`
}

type notification struct {
	ID          int64              `json:"id"`
	PrincipalID int64              `json:"principal_id"`
	Note        string             `json:"note"`
	EntryIDs    []int64            `json:"entry_ids"`
	Category    string             `json:"category"`
	Status      string             `json:"status"`
	HeldUntil   *time.Time         `json:"held_until,omitempty"`
	Budget      notificationBudget `json:"budget"`
	Why         notificationWhy    `json:"why"`
	Generation  string             `json:"generation"`
	CreatedAt   time.Time          `json:"created_at"`
	Replay      bool               `json:"replay"`
}

type triageResult struct {
	Results      []stateResult `json:"results"`
	RevertHandle string        `json:"revert_handle"`
	ExpiresAt    time.Time     `json:"expires_at"`
}

type activity struct {
	ID            int64           `json:"id"`
	PrincipalID   int64           `json:"principal_id"`
	PrincipalName string          `json:"principal_name"`
	Action        string          `json:"action"`
	Target        json.RawMessage `json:"target"`
	Before        json.RawMessage `json:"before"`
	After         json.RawMessage `json:"after"`
	Generation    string          `json:"generation"`
	CreatedAt     time.Time       `json:"created_at"`
	NextAction    string          `json:"next_action"`
	Reason        string          `json:"reason,omitempty"`
}

type activityGroup struct {
	PrincipalID   int64      `json:"principal_id"`
	PrincipalName string     `json:"principal_name"`
	Activities    []activity `json:"activities"`
}

type activityPage struct {
	Activities []activity      `json:"activities,omitempty"`
	Groups     []activityGroup `json:"groups,omitempty"`
}

type changeRecord struct {
	Rev     uint64          `json:"rev"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type changesPage struct {
	Changes    []changeRecord `json:"changes"`
	Next       revision       `json:"next"`
	NextCursor string         `json:"next_cursor"`
}

// searchMatch is one entry or agent-note hit from the search endpoint.
type searchMatch struct {
	Kind           string  `json:"kind"`
	EntryID        int64   `json:"entry_id"`
	SubscriptionID int64   `json:"subscription_id"`
	FeedID         int64   `json:"feed_id"`
	Seq            int64   `json:"seq"`
	Title          string  `json:"title"`
	URL            string  `json:"url"`
	Snippet        string  `json:"snippet"`
	Rank           float64 `json:"rank"`
	AnnotationID   int64   `json:"annotation_id,omitempty"`
	AgentName      string  `json:"agent_name,omitempty"`
	Untrusted      bool    `json:"untrusted"`
}

type searchPage struct {
	Matches    []searchMatch `json:"matches"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// searchFilter narrows a saved search to a subscription or folder.
type searchFilter struct {
	SubscriptionID int64 `json:"subscription_id,omitempty"`
	FolderID       int64 `json:"folder_id,omitempty"`
	Limit          int   `json:"limit,omitempty"`
}

type savedSearch struct {
	ID      int64        `json:"id"`
	Name    string       `json:"name"`
	Query   string       `json:"query"`
	Filters searchFilter `json:"filters"`
}

// problem is the RFC 9457 body the API returns on failure.
type problem struct {
	Type      string          `json:"type"`
	Title     string          `json:"title"`
	Status    int             `json:"status"`
	Detail    string          `json:"detail"`
	Budget    json.RawMessage `json:"budget"`
	ErrorCode string          `json:"error"`
}

// apiError carries a server problem with the transport status and headers.
type apiError struct {
	problem    problem
	status     int
	raw        string
	retryAfter string
}

func (e *apiError) Error() string {
	message := fmt.Sprintf("%d %s", e.status, e.problem.Title)
	if e.problem.Detail != "" {
		message += ": " + e.problem.Detail
	}
	if e.status == http.StatusTooManyRequests {
		if budget := budgetText(e.problem.Budget); budget != "" {
			message += " (budget " + budget + ")"
		}
		if e.retryAfter != "" {
			message += "; retry after " + e.retryAfter
		}
	}
	return message
}

// budgetText extracts the budget fields the API documents; unknown shapes fall
// back to their compact JSON.
func budgetText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var budget struct {
		UsedToday  *int `json:"used_today"`
		LimitToday *int `json:"limit_today"`
	}
	if err := json.Unmarshal(raw, &budget); err == nil && (budget.UsedToday != nil || budget.LimitToday != nil) {
		return fmt.Sprintf("used %d/%d", intOr(budget.UsedToday, 0), intOr(budget.LimitToday, 0))
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return string(raw)
	}
	return compact.String()
}

func intOr(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}
