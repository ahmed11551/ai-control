package server

import "testing"

func TestManagerCallKind(t *testing.T) {
	cases := []struct {
		dir, disp, want string
	}{
		{"outbound", "NO ANSWER", "outgoing"},
		{"out", "missed", "outgoing"},
		{"inbound", "no_answer", "missed"},
		{"incoming", "BUSY", "missed"},
		{"inbound", "ANSWERED", "incoming"},
		{"inbound", "RINGING", "incoming"},
		{"", "missed", "other"},
	}
	for _, tc := range cases {
		if got := managerCallKind(tc.dir, tc.disp); got != tc.want {
			t.Fatalf("%s/%s: got %s want %s", tc.dir, tc.disp, got, tc.want)
		}
	}
}

func TestCallPeerTail(t *testing.T) {
	if got := callPeerTail("inbound", "+7 999 123-45-67", "101"); got != "4567" {
		t.Fatalf("inbound tail %q", got)
	}
	if got := callPeerTail("outbound", "101", "79991234567"); got != "4567" {
		t.Fatalf("outbound tail %q", got)
	}
	if got := callPeerTail("inbound", "101", "79991234567"); got != "" {
		t.Fatalf("short inbound must not use the other number, got %q", got)
	}
	if got := callPeerTail("outbound", "79991234567", ""); got != "" {
		t.Fatalf("empty outbound tail %q", got)
	}
}
