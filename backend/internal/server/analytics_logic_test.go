package server

import (
	"strings"
	"testing"
	"time"
)

func TestParseAnalyticsRange(t *testing.T) {
	loc := time.FixedZone("MSK", 3*3600)
	now := time.Date(2026, 10, 4, 22, 15, 0, 0, loc)

	from, to, err := parseAnalyticsRange("day", "", "", now, loc)
	if err != nil || !from.Equal(time.Date(2026, 10, 4, 0, 0, 0, 0, loc)) || !to.Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, loc)) {
		t.Fatalf("day %v %v %v", from, to, err)
	}
	from, to, err = parseAnalyticsRange("week", "", "", now, loc)
	if err != nil || !from.Equal(time.Date(2026, 9, 28, 0, 0, 0, 0, loc)) || !to.Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, loc)) {
		t.Fatalf("week %v %v %v", from, to, err)
	}
	from, to, err = parseAnalyticsRange("month", "", "", now, loc)
	if err != nil || !from.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, loc)) || !to.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, loc)) {
		t.Fatalf("month %v %v %v", from, to, err)
	}
	from, to, err = parseAnalyticsRange("custom", "2026-10-01", "2026-10-04", now, loc)
	if err != nil || !from.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, loc)) || !to.Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, loc)) {
		t.Fatalf("custom %v %v %v", from, to, err)
	}
	if _, _, err = parseAnalyticsRange("custom", "2026-10-04", "2026-10-01", now, loc); err == nil {
		t.Fatal("inverted range")
	}
}

func TestClassifyLeadBucket(t *testing.T) {
	if classifyLeadBucket(true, false, true, hirePipelineKey+":bilet") != "success" {
		t.Fatal("won wins")
	}
	if classifyLeadBucket(false, true, false, "") != "rejected" {
		t.Fatal("lost")
	}
	if classifyLeadBucket(false, false, true, hirePipelineKey+":2") != "to_logist" {
		t.Fatal("flag")
	}
	if classifyLeadBucket(false, false, false, hirePipelineKey+":bilet") != "to_logist" {
		t.Fatal("stage")
	}
	if classifyLeadBucket(false, false, false, hirePipelineKey+":2") != "in_work" {
		t.Fatal("work")
	}
	sql := sqlLeadBucketExpr()
	for _, k := range logistHandoffStageKeys() {
		if !strings.Contains(sql, k) {
			t.Fatalf("sql missing %s", k)
		}
	}
}

func TestCallIsConnectedAndRestrict(t *testing.T) {
	if !callIsConnected("ANSWERED", 12) {
		t.Fatal("answered")
	}
	if callIsConnected("no_answer", 0) || callIsConnected("ANSWERED", 0) || callIsConnected("missed", 8) {
		t.Fatal("not connected")
	}
	if shareOf(2, 8) != 0.25 || shareOf(0, 0) != 0 {
		t.Fatal("share")
	}
	if shouldRestrictSession(2) || !shouldRestrictSession(3) {
		t.Fatal("threshold")
	}
}
