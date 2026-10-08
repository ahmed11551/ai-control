package server

import (
	"strings"
	"testing"
	"time"
)

func TestNightRequestWhere(t *testing.T) {
	where, args := nightRequestWhere("", "Иван", "today", "", "", "wait", "")
	if !strings.Contains(where, "sent_at") || !strings.Contains(where, "done_at IS NULL") || !strings.Contains(where, "skip_at IS NULL") || !strings.Contains(where, "ILIKE") {
		t.Fatalf("where = %s", where)
	}
	where, _ = nightRequestWhere("", "", "", "", "", "skip", "")
	if !strings.Contains(where, "skip_at IS NOT NULL") {
		t.Fatalf("skip where = %s", where)
	}
	where, _ = nightRequestWhere("", "", "", "", "", "no", "")
	if !strings.Contains(where, "done_at IS NULL") || !strings.Contains(where, "skip_at IS NULL") {
		t.Fatalf("legacy no where = %s", where)
	}
	if len(args) != 1 {
		t.Fatalf("args %d", len(args))
	}
	where, args = nightRequestWhere("11111111-1111-1111-1111-111111111111", "7999", "week", "", "", "yes", "")
	if !strings.Contains(where, "department_id") || !strings.Contains(where, "date - 6") || !strings.Contains(where, "done_at IS NOT NULL") {
		t.Fatalf("where = %s", where)
	}
	if len(args) != 4 {
		t.Fatalf("phone args %d: %s", len(args), where)
	}
	where, _ = nightRequestWhere("nope", "", "past", "", "", "", "")
	if strings.Contains(where, "department_id") || !strings.Contains(where, "date <") {
		t.Fatalf("bad dept leaked: %s", where)
	}
	where, args = nightRequestWhere("", "", "range", "2026-10-01", "2026-10-04", "", "")
	if !strings.Contains(where, ">=") || !strings.Contains(where, "<=") || len(args) != 2 {
		t.Fatalf("range %s %v", where, args)
	}
}

func TestNightRequestCallFilter(t *testing.T) {
	where, args := nightRequestWhere("", "", "", "", "", "", "soon")
	if !strings.Contains(where, "first_call_at IS NOT NULL") || !strings.Contains(where, "done_at IS NULL") || !strings.Contains(where, "skip_at IS NULL") {
		t.Fatalf("soon where = %s", where)
	}
	if len(args) != 0 {
		t.Fatalf("soon args %d", len(args))
	}
	where, _ = nightRequestWhere("", "", "today", "", "", "wait", "overdue")
	if !strings.Contains(where, "first_call_at < now()") || !strings.Contains(where, "sent_at") {
		t.Fatalf("overdue where = %s", where)
	}
	where, _ = nightRequestWhere("", "", "", "", "", "", "later")
	if strings.Contains(where, "first_call_at") {
		t.Fatalf("unknown call leaked: %s", where)
	}
}

func TestParseNightFirstCall(t *testing.T) {
	at, ok := parseNightFirstCall("2026-10-06T21:15")
	if !ok {
		t.Fatal("expected parsed time")
	}
	got := at.In(moscowLocation())
	if got.Year() != 2026 || got.Month() != time.October || got.Day() != 6 || got.Hour() != 21 || got.Minute() != 15 {
		t.Fatalf("parsed %s", got)
	}
	at, ok = parseNightFirstCall("2026-10-06T21:15:00")
	if !ok || at.In(moscowLocation()).Minute() != 15 {
		t.Fatal("seconds")
	}
	for _, raw := range []string{"", "2026-10-06", "вчера", "1999-01-01T10:00", "2099-01-01T10:00"} {
		if _, ok := parseNightFirstCall(raw); ok {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestNightSelfNoteSearch(t *testing.T) {
	where, args := nightRequestWhere("", "не берёт", "", "", "", "", "")
	if !strings.Contains(where, "self_note") || !strings.Contains(where, "ILIKE") {
		t.Fatalf("note search missing: %s", where)
	}
	if len(args) != 1 {
		t.Fatalf("note search should reuse one pattern, args %d", len(args))
	}
	note := clipNightSelfNote("  " + strings.Repeat("а", 1005) + " \x00 ")
	if strings.Contains(note, "\x00") || len([]rune(note)) != 1000 {
		t.Fatalf("clip %d", len([]rune(note)))
	}
	if clipNightSelfNote("  \n позвонить утром \n") != "позвонить утром" {
		t.Fatal("trim")
	}
}

func TestCanSendToNightLogist(t *testing.T) {
	for _, role := range []string{"manager", "sales", "rop", "admin", " Manager "} {
		if !canSendToNightLogist(role) {
			t.Fatalf("expected send for %q", role)
		}
	}
	for _, role := range []string{"night_logist", "logist", "customer", ""} {
		if canSendToNightLogist(role) {
			t.Fatalf("did not expect send for %q", role)
		}
	}
}

