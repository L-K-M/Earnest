package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

// Retrieval renders short snippets; the queries must not drag whole article
// texts out of the database to build them. The bound is the contract between
// RetrievalTextLimit and the left() projection in the retrieval SQL.
func TestRetrievalBoundsSourceText(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	ctx := context.Background()
	var user, feed, entry int64
	if err := db.Pool().QueryRow(ctx, "INSERT INTO users(username,password_hash) VALUES('human','$argon2id$test') RETURNING id").Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, "INSERT INTO feeds(url) VALUES('https://example.com/rss') RETURNING id").Scan(&feed); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, "INSERT INTO subscriptions(user_id,feed_id) VALUES($1,$2)", user, feed); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, `INSERT INTO entries(feed_id,seq,identity_hash,title,language,content_hash)
 VALUES($1,1,decode(repeat('ab',32),'hex'),'Needle title','en-US',decode(repeat('cd',32),'hex')) RETURNING id`, feed).Scan(&entry); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("word ", 5000)
	if _, err := db.Pool().Exec(ctx, `INSERT INTO contents(entry_id,content_hash,sanitizer_version,source,html,text)
 VALUES($1,decode(repeat('cd',32),'hex'),1,'feed','<p>short</p>',$2)`, entry, long); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO embeddings(entry_id,model,dims,embedding,content_hash,input_title)
 VALUES($1,'model',1024,(ARRAY[1::real] || array_fill(0::real,ARRAY[1023]))::halfvec,decode(repeat('cd',32),'hex'),'Needle title')`, entry); err != nil {
		t.Fatal(err)
	}

	vector := make([]float32, llm.StorageDimensions)
	vector[0] = 1
	hits, err := db.SemanticSearch(ctx, user, 0, 0, 0, "model", vector, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("semantic hits %d", len(hits))
	}
	if len([]rune(hits[0].Text)) > store.RetrievalTextLimit {
		t.Fatalf("semantic text %d runes exceeds the bound", len([]rune(hits[0].Text)))
	}

	lexical, err := db.AskLexical(ctx, user, "needle", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lexical) != 1 {
		t.Fatalf("lexical hits %d", len(lexical))
	}
	if len([]rune(lexical[0].Text)) > store.RetrievalTextLimit {
		t.Fatalf("lexical text %d runes exceeds the bound", len([]rune(lexical[0].Text)))
	}
}
