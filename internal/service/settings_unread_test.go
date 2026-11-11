package service

import "testing"

func TestDecodeUnreadPreference(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want bool
	}{
		{"legacy", `{}`, false},
		{"enabled", `{"show_only_unread":true}`, true},
		{"disabled", `{"show_only_unread":false}`, false},
		{"invalid", `{"show_only_unread":"true"}`, false},
		{"null", `{"show_only_unread":null}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeReaderSettings([]byte(tc.raw))
			if err != nil || got.ShowOnlyUnread != tc.want {
				t.Fatalf("show_only_unread = %v, want %v; error: %v", got.ShowOnlyUnread, tc.want, err)
			}
		})
	}
}
