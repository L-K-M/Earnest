package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/api"
	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

type reviewFeedProber struct{}

func (reviewFeedProber) Probe(_ context.Context, raw string, _ *service.FeedCredentials) (string, service.FeedMeta, []service.IngestItem, error) {
	hash := sha256.Sum256([]byte("review entry"))
	identity := hex.EncodeToString(hash[:])
	return raw, service.FeedMeta{Title: "Review feed", Language: "english"}, []service.IngestItem{{IdentityHash: identity, ContentHash: identity, Title: "Review article", URL: "https://feed.test/article", ContentHTML: "<p>Searchable review content</p>", ContentText: "Searchable review content", SanitizerVersion: 1, ContentSource: service.ContentFromFeed}}, nil
}

func TestReviewedCLICommandsAgainstAPI(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	secret := bytes.Repeat([]byte{7}, 32)
	auth := service.NewAuth(db, nil, secret)
	owner, err := auth.CreateUser(t.Context(), "reviewer", "password")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(db, service.Deps{SecretKey: secret, FeedProber: reviewFeedProber{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Subscribe(t.Context(), owner, "https://feed.test/rss", service.SubscribeOptions{OpID: "setup"})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := svc.Entries(t.Context(), owner, service.EntryFilter{View: service.ViewAll})
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v error=%v", entries, err)
	}
	id := strconv.FormatInt(entries[0].ID, 10)
	token, err := auth.CreateToken(t.Context(), owner, service.TokenOptions{Name: "Review Agent", Kind: service.TokenAgent, Audience: service.AudienceAPI, Scopes: []service.Scope{service.ScopeEntriesRead, service.ScopeAnnotationsWrite, service.ScopeStateWriteHuman, service.ScopePositionOwn}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.New(svc, auth, slog.New(slog.NewTextHandler(io.Discard, nil)), "https://reader.test", nil))
	defer server.Close()
	t.Setenv("EARNEST_URL", server.URL)
	t.Setenv("EARNEST_TOKEN", token.Secret)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	run := func(t *testing.T, args ...string) string {
		t.Helper()
		var out, errOut bytes.Buffer
		if code := Run(args, &out, &errOut); code != exitOK {
			t.Fatalf("earnest %s: exit=%d stderr=%s", strings.Join(args, " "), code, errOut.String())
		}
		return out.String()
	}
	t.Run("activity empty wrapper", func(t *testing.T) { run(t, "activity") })
	t.Run("annotation and brief endpoints", func(t *testing.T) {
		run(t, "annotate", id, "--text", "distinctive_review_note")
		run(t, "brief", id, "--text", "review brief")
	})
	t.Run("search matches and agent attribution", func(t *testing.T) {
		out := run(t, "search", "distinctive_review_note")
		if !strings.Contains(out, "Review Agent") || !strings.Contains(out, id) {
			t.Fatalf("search output=%s", out)
		}
	})
	t.Run("triage and revert", func(t *testing.T) {
		out := run(t, "triage", "done", id, "--json")
		var result struct {
			RevertHandle string `json:"revert_handle"`
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil || result.RevertHandle == "" {
			t.Fatalf("triage=%s error=%v", out, err)
		}
		run(t, "revert", result.RevertHandle)
	})
	t.Run("activity populated wrapper", func(t *testing.T) {
		out := run(t, "activity")
		if !strings.Contains(out, "Review Agent") {
			t.Fatalf("activity output=%s", out)
		}
	})
}
