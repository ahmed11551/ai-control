package server

import (
	"testing"
	"time"
)

func TestParseInboxEventTimeUnix(t *testing.T) {
	got := parseInboxEventTime(map[string]any{"timestamp": "1710000000"})
	want := time.Unix(1710000000, 0).UTC()
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseInboxEventTimeRFC3339(t *testing.T) {
	got := parseInboxEventTime(map[string]any{"created_at": "2024-06-01T12:00:00Z"})
	if got.Year() != 2024 || got.Month() != 6 {
		t.Fatalf("got %v", got)
	}
}
