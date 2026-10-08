package server

import (
	"encoding/json"
	"testing"
)

func TestExolveRecordingKeyHelpers(t *testing.T) {
	if got := exolveRecordingKey("7135909209505402880"); got != "exolve:7135909209505402880" {
		t.Fatalf("key=%q", got)
	}
	if got := parseExolveRecordingKey("exolve:7135909209505402880"); got != "7135909209505402880" {
		t.Fatalf("parse=%q", got)
	}
	if parseExolveRecordingKey("asterisk/foo.wav") != "" {
		t.Fatal("asterisk key must not parse as exolve")
	}
	url := exolveDownloadURL("7135909209505402880", "")
	if url != "https://api.exolve.ru/statistics/download/7135909209505402880" {
		t.Fatalf("url=%q", url)
	}
	custom := "https://api.exolve.ru/statistics/download/999"
	if got := exolveDownloadURL("1", custom); got != custom {
		t.Fatalf("custom path=%q", got)
	}
}

func TestParseExolveCallEventsCRR(t *testing.T) {
	raw := []byte(`{
		"uid": 7135909209505402880,
		"type": "crr",
		"path": "https://api.exolve.ru/statistics/download/7135909209505402880",
		"number_a": "79991112233",
		"called_number": "74991112233",
		"record_duration": 21
	}`)
	evs := parseExolveCallEvents(raw)
	if len(evs) != 1 {
		t.Fatalf("len=%d", len(evs))
	}
	ev := evs[0]
	if ev.Type != "crr" {
		t.Fatalf("type=%q", ev.Type)
	}
	if ev.CallID != "7135909209505402880" {
		t.Fatalf("call_id=%q", ev.CallID)
	}
	if ev.Path == "" || ev.RecordDuration != 21 {
		t.Fatalf("path=%q dur=%d", ev.Path, ev.RecordDuration)
	}
	if ev.Status != "call_record_ready" {
		t.Fatalf("status=%q", ev.Status)
	}
	peers := exolveEventPeerPhones(ev)
	if len(peers) == 0 {
		t.Fatal("expected peer phones")
	}
}

func TestParseExolveCallEventsCRRArray(t *testing.T) {
	raw, _ := json.Marshal([]map[string]any{
		{"type": "crr", "uid": "111", "path": "https://api.exolve.ru/statistics/download/111"},
		{"type": "h", "call_id": "222", "event_side": "A", "hangup_cause": "NORMAL_CLEARING"},
	})
	evs := parseExolveCallEvents(raw)
	if len(evs) != 2 {
		t.Fatalf("len=%d", len(evs))
	}
	if evs[0].Type != "crr" || evs[0].CallID != "111" {
		t.Fatalf("first=%+v", evs[0])
	}
	if evs[1].Type != "h" {
		t.Fatalf("second type=%q", evs[1].Type)
	}
}

func TestPeerTail(t *testing.T) {
	if got := peerTail("8 (999) 111-22-33"); got != "9991112233" {
		t.Fatalf("tail=%q", got)
	}
}
