package feed

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// updateGolden rewrites every .golden.json from the current parser. Run:
//
//	go test ./internal/feed -run TestGoldenCorpus -update
var updateGolden = flag.Bool("update", false, "rewrite golden corpus files from the current parser")

const defaultFixtureBaseURL = "https://example.test/feed.xml"

// fixtureOptions is the optional <fixture>.options.json sidecar.
type fixtureOptions struct {
	BaseURL   *string `json:"base_url"`
	Charset   string  `json:"charset"`
	MaxBytes  int64   `json:"max_bytes"`
	MaxItems  int     `json:"max_items"`
	TimeoutMS int     `json:"timeout_ms"`
}

// goldenFile is the snapshot shape. Error records the sentinel message for
// documents the parser rejects, which keeps rejected fixtures stable even when
// the underlying detail changes.
type goldenFile struct {
	Error string       `json:"error,omitempty"`
	Feed  goldenFeed   `json:"feed"`
	Items []goldenItem `json:"items"`
}

type goldenFeed struct {
	Title    string `json:"title"`
	SiteURL  string `json:"site_url"`
	FeedURL  string `json:"feed_url"`
	Language string `json:"language"`
	IconURL  string `json:"icon_url"`
}

type goldenEnclosure struct {
	URL    string `json:"url"`
	Type   string `json:"type,omitempty"`
	Length int64  `json:"length,omitempty"`
}

type goldenItem struct {
	GUID        string            `json:"guid"`
	URL         string            `json:"url"`
	Title       string            `json:"title"`
	Author      string            `json:"author"`
	Published   *string           `json:"published,omitempty"`
	Updated     *string           `json:"updated,omitempty"`
	SummaryHTML string            `json:"summary_html"`
	ContentHTML string            `json:"content_html"`
	Enclosures  []goldenEnclosure `json:"enclosures,omitempty"`
	Image       string            `json:"image,omitempty"`
	Categories  []string          `json:"categories,omitempty"`
	Language    string            `json:"language"`
	Identity    string            `json:"identity"`
	ContentHash string            `json:"content_hash"`
}

func TestGoldenCorpus(t *testing.T) {
	// Tests run in the package directory; the corpus lives at the repository
	// root next to the other shared test data.
	dir := filepath.Join("..", "..", "testdata", "feeds")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read fixture directory: %v", err)
	}

	fixtures := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasSuffix(name, ".golden.json") || strings.HasSuffix(name, ".options.json") {
			continue
		}
		fixtures++
		t.Run(strings.TrimSuffix(name, filepath.Ext(name)), func(t *testing.T) {
			t.Parallel()
			checkGolden(t, dir, name)
		})
	}
	if fixtures == 0 {
		t.Fatal("no fixtures found")
	}
}

func checkGolden(t *testing.T, dir, name string) {
	t.Helper()

	stem := strings.TrimSuffix(name, filepath.Ext(name))
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	opts, err := loadFixtureOptions(filepath.Join(dir, stem+".options.json"))
	if err != nil {
		t.Fatalf("read options: %v", err)
	}
	baseURL := defaultFixtureBaseURL
	if opts.BaseURL != nil {
		baseURL = *opts.BaseURL
	}

	limits := Limits{
		MaxBytes: opts.MaxBytes,
		MaxItems: opts.MaxItems,
		Timeout:  time.Duration(opts.TimeoutMS) * time.Millisecond,
	}
	feed, parseErr := Parse(data, ParseOptions{
		Charset: opts.Charset,
		BaseURL: baseURL,
		Limits:  limits,
	})

	got := buildGolden(feed, parseErr)
	goldenPath := filepath.Join(dir, stem+".golden.json")
	if *updateGolden {
		writeGolden(t, goldenPath, got)
		return
	}

	wantBytes, err := os.ReadFile(goldenPath)
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing golden file %s; run go test ./internal/feed -run TestGoldenCorpus -update", goldenPath)
	}
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	gotValue, err := jsonValue(got)
	if err != nil {
		t.Fatalf("encode got: %v", err)
	}
	wantValue, err := jsonValueBytes(wantBytes)
	if err != nil {
		t.Fatalf("decode golden %s: %v", goldenPath, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		pretty, _ := json.MarshalIndent(got, "", "  ")
		t.Fatalf("golden mismatch for %s:\n got: %s\nwant: %s", name, pretty, wantBytes)
	}
}

func loadFixtureOptions(path string) (fixtureOptions, error) {
	defaults := fixtureOptions{BaseURL: stringPtr(defaultFixtureBaseURL)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaults, nil
	}
	if err != nil {
		return fixtureOptions{}, err
	}
	if err := json.Unmarshal(data, &defaults); err != nil {
		return fixtureOptions{}, err
	}
	return defaults, nil
}

func buildGolden(f *Feed, parseErr error) goldenFile {
	if parseErr != nil {
		return goldenFile{Error: classifyError(parseErr)}
	}
	out := goldenFile{Feed: goldenFeed{
		Title:    f.Title,
		SiteURL:  f.SiteURL,
		FeedURL:  f.FeedURL,
		Language: f.Language,
		IconURL:  f.IconURL,
	}}
	for _, it := range f.Items {
		item := goldenItem{
			GUID:        it.GUID,
			URL:         it.URL,
			Title:       it.Title,
			Author:      it.Author,
			Published:   goldenTime(it.Published),
			Updated:     goldenTime(it.Updated),
			SummaryHTML: it.SummaryHTML,
			ContentHTML: it.ContentHTML,
			Image:       it.Image,
			Language:    it.Language,
			Identity:    IdentityHash(it),
			ContentHash: ContentHash(it),
		}
		for _, enc := range it.Enclosures {
			item.Enclosures = append(item.Enclosures, goldenEnclosure{
				URL: enc.URL, Type: enc.Type, Length: enc.Length,
			})
		}
		item.Categories = append(item.Categories, it.Categories...)
		out.Items = append(out.Items, item)
	}
	return out
}

// classifyError maps wrapped errors back to their sentinel text so goldens do
// not depend on decoder detail messages.
func classifyError(err error) string {
	for _, sentinel := range []error{
		ErrTooLarge, ErrTooManyItems, ErrParseTimeout,
		ErrUnsupportedFormat, ErrMalformed, ErrDTD,
	} {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return err.Error()
}

func goldenTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

func writeGolden(t *testing.T, path string, doc goldenFile) {
	t.Helper()
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("encode golden: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
	t.Logf("wrote %s", path)
}

func jsonValue(doc goldenFile) (any, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return jsonValueBytes(raw)
}

func jsonValueBytes(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func stringPtr(s string) *string { return &s }
