package push

import (
	"errors"
	"slices"
	"testing"
)

func TestTextAllowed(t *testing.T) {
	tests := []struct {
		name string
		dev  Device
		want bool
	}{
		{"webpush opted in", Device{Kind: KindWebPush, ShowText: true}, true},
		{"webpush opted out", Device{Kind: KindWebPush}, false},
		{"unifiedpush opted in", Device{Kind: KindUnifiedPush, ShowText: true}, true},
		{"unifiedpush opted out", Device{Kind: KindUnifiedPush}, false},
		{"self-hosted ntfy opted in", Device{Kind: KindNtfy, ShowText: true, SelfHosted: true}, true},
		{"self-hosted ntfy opted out", Device{Kind: KindNtfy, SelfHosted: true}, false},
		{"third-party ntfy opted in", Device{Kind: KindNtfy, ShowText: true}, false},
		{"apns opted in", Device{Kind: KindAPNs, ShowText: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.dev.TextAllowed(); got != tt.want {
				t.Fatalf("TextAllowed = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegistry(t *testing.T) {
	registry := NewRegistry()
	if kinds := registry.Kinds(); len(kinds) != 0 {
		t.Fatalf("new registry has kinds %v", kinds)
	}
	ntfy, err := NewNtfy(Config{Client: testOutboundClient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(KindNtfy, ntfy); err != nil {
		t.Fatal(err)
	}
	got, err := registry.Get(KindNtfy)
	if err != nil {
		t.Fatal(err)
	}
	if got != Transport(ntfy) {
		t.Fatal("registry returned a different transport")
	}
	if _, err := registry.Get(KindWebPush); !errors.Is(err, ErrUnknownTransport) {
		t.Fatalf("unknown kind error = %v", err)
	}
	if err := registry.Register("", ntfy); err == nil {
		t.Fatal("empty kind accepted")
	}
	if err := registry.Register(KindWebPush, nil); err == nil {
		t.Fatal("nil transport accepted")
	}
	if kinds := registry.Kinds(); !slices.Equal(kinds, []Kind{KindNtfy}) {
		t.Fatalf("kinds = %v", kinds)
	}
}
