package main

import (
	"testing"
	"time"
)

func TestNextIdle(t *testing.T) {
	// A pass that did work polls again eagerly.
	if got := nextIdle(16*time.Second, true, 30*time.Second); got != deliveryInterval {
		t.Fatalf("work resets pacing: %v", got)
	}
	// Idle passes back off exponentially to the cap.
	for _, c := range []struct{ current, max, want time.Duration }{
		{time.Second, 30 * time.Second, 2 * time.Second},
		{8 * time.Second, 30 * time.Second, 16 * time.Second},
		{16 * time.Second, 30 * time.Second, 30 * time.Second},
		{30 * time.Second, 30 * time.Second, 30 * time.Second},
	} {
		if got := nextIdle(c.current, false, c.max); got != c.want {
			t.Fatalf("nextIdle(%v, idle, %v) = %v, want %v", c.current, c.max, got, c.want)
		}
	}
	// A worker without an idle window keeps its eager pacing.
	if got := nextIdle(time.Second, false, deliveryInterval); got != deliveryInterval {
		t.Fatalf("eager workers stay at the interval: %v", got)
	}
}
