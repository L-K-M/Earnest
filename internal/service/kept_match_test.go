package service

import (
	"testing"
)

func TestMatchKept(t *testing.T) {
	primary, link := []byte("primary-identity"), []byte("link-identity")
	work := func(guid string) keptWork {
		w := keptWork{identities: [][]byte{primary}}
		if guid != "" {
			w.identities = append(w.identities, link)
		}
		w.change.item = IngestItem{GUID: guid, URL: "https://example.test/a", Title: "A"}
		return w
	}
	byPrimary := keptCandidate{id: 4, identity: primary}
	byGUID := keptCandidate{id: 3, identity: []byte("other"), guid: "g"}
	byLink := keptCandidate{id: 2, identity: link, guid: ""}
	byTitle := keptCandidate{id: 1, identity: []byte("guid-identity"), guid: "", url: "https://example.test/a", title: "A"}

	for _, tc := range []struct {
		name       string
		work       keptWork
		candidates []keptCandidate
		want       int64 // 0: no match
	}{
		{"the identity the import would create wins", work("g"), []keptCandidate{byTitle, byLink, byGUID, byPrimary}, 4},
		{"then the publisher GUID", work("g"), []keptCandidate{byTitle, byLink, byGUID}, 3},
		{"then the link identity", work("g"), []keptCandidate{byTitle, byLink}, 2},
		{"a link identity under another GUID is another article", work("g"), []keptCandidate{{id: 2, identity: link, guid: "other"}}, 0},
		{"link and title decide when the export has no GUID", work(""), []keptCandidate{{id: 1, identity: []byte("x"), guid: "fetched", url: "https://example.test/a", title: "A"}}, 1},
		{"link and title decide when the entry has no GUID", work("g"), []keptCandidate{byTitle}, 1},
		{"link and title never override two GUIDs", work("g"), []keptCandidate{{id: 1, identity: []byte("x"), guid: "fetched", url: "https://example.test/a", title: "A"}}, 0},
		{"the title must agree", work(""), []keptCandidate{{id: 1, identity: []byte("x"), url: "https://example.test/a", title: "B"}}, 0},
		{"ties go to the first candidate", work("g"), []keptCandidate{{id: 5, identity: []byte("x"), guid: "g"}, {id: 6, identity: []byte("y"), guid: "g"}}, 5},
		{"nothing stored", work("g"), nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := matchKept(tc.work, tc.candidates)
			if tc.want == 0 {
				if ok {
					t.Fatalf("matched %+v, want no match", got)
				}
				return
			}
			if !ok || got.id != tc.want {
				t.Fatalf("matched %+v (%v), want entry %d", got, ok, tc.want)
			}
		})
	}
}
