package stats

import (
	"testing"
	"time"
)

func TestRateLimiterWindow(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	l := NewRateLimiter(3)
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if !l.Allow("a") {
			t.Fatalf("отказ на %d-м", i+1)
		}
	}
	if l.Allow("a") {
		t.Fatal("четвёртый прошёл")
	}
	if !l.Allow("b") {
		t.Fatal("чужой ключ заперт")
	}
	now = now.Add(time.Minute)
	if !l.Allow("a") {
		t.Fatal("новое окно не открылось")
	}
}
