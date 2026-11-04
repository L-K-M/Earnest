package service

import (
	"encoding/json"
	"testing"
)

func TestLabelJSONKeys(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{
		{Label{ID: 1, Name: "work", Rev: 2}, `{"id":1,"name":"work","rev":2,"deleted":false}`},
		{LabelMembership{ID: 1, LabelID: 2, EntryID: 3, Rev: 4}, `{"id":1,"label_id":2,"entry_id":3,"rev":4,"deleted":false}`},
	} {
		got, err := json.Marshal(tc.value)
		if err != nil || string(got) != tc.want {
			t.Errorf("JSON = %s, %v; want %s", got, err, tc.want)
		}
	}
}
