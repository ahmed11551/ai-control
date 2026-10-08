package server

import "testing"

func TestParseCallHookVoximplant(t *testing.T) {
	ev := parseCallHook("voximplant", map[string]any{
		"event":     "disconnected",
		"sessionId": "sess-1",
		"call": map[string]any{
			"callerid":    "+79001112233",
			"destination": "+78005553535",
			"duration":    42,
		},
		"direction": "inbound",
	})
	if ev.UID != "voximplant:sess-1" {
		t.Fatalf("uid %s", ev.UID)
	}
	if ev.From != "+79001112233" || ev.To != "+78005553535" {
		t.Fatalf("phones %s %s", ev.From, ev.To)
	}
	if ev.Duration != 42 || ev.Direction != "inbound" {
		t.Fatalf("meta %+v", ev)
	}
}

func TestParseCallHookIncomingWord(t *testing.T) {
	ev := parseCallHook("voximplant", map[string]any{
		"session_id":  "2",
		"callerid":    "79001112233",
		"destination": "8800",
		"callType":    "incoming",
	})
	if ev.Direction != "inbound" {
		t.Fatalf("dir %s", ev.Direction)
	}
}

func TestBuildSoftphoneEventInbound(t *testing.T) {
	ev, err := buildSoftphoneEvent("abc", "vox", "incoming", "7 925 123-45-67", "start", "", 0, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if ev.UID != "softphone:voximplant:abc" {
		t.Fatalf("uid %s", ev.UID)
	}
	if ev.Provider != "voximplant" || ev.Direction != "inbound" || ev.From != "7 925 123-45-67" || ev.To != "" {
		t.Fatalf("event %+v", ev)
	}
	if ev.Disp != "RINGING" || ev.UserID != "user-1" {
		t.Fatalf("meta %+v", ev)
	}
}

func TestBuildSoftphoneEventOutboundEnd(t *testing.T) {
	ev, err := buildSoftphoneEvent("x", "exolve", "outgoing", "79251234567", "end", "answered", 42, "u2")
	if err != nil {
		t.Fatal(err)
	}
	if ev.Direction != "outbound" || ev.To != "79251234567" || ev.From != "" {
		t.Fatalf("event %+v", ev)
	}
	if ev.Duration != 42 || ev.Disp != "ANSWERED" || ev.Provider != "exolve" {
		t.Fatalf("meta %+v", ev)
	}
}
