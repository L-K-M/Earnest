package api

import (
	"encoding/json"
	"testing"
)

func TestSchemaBuilds(t *testing.T) {
	data, err := OpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]any)
	for _, path := range []string{"/v1/state:batch", "/v1/state:through", "/v1/events", "/v1/opml:import"} {
		if _, ok := paths[path]; !ok {
			t.Errorf("missing path %s", path)
		}
	}
}
