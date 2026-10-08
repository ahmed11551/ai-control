package server

import (
	"strings"
	"testing"
	"time"
)

func TestClassifyWappiHealthSpamAndFlood(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	h, label, _ := classifyWappiHealth(true, "open", "", "ACCOUNT_SPAM_RESTRICTED", now)
	if h != "spam" || !strings.Contains(label, "бан") {
		t.Fatalf("spam: %s %s", h, label)
	}
	h, label, _ = classifyWappiHealth(true, "open", "", "CHECK_PHONE_FLOODWAIT", now)
	if h != "floodwait" || !strings.Contains(strings.ToLower(label), "лимит") {
		t.Fatalf("flood: %s %s", h, label)
	}
}

func TestClassifyWappiHealthPaymentAndOffline(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	h, _, _ := classifyWappiHealth(true, "open", "2026-01-01T00:00:00Z", "", now)
	if h != "payment" {
		t.Fatalf("payment got %s", h)
	}
	h, _, _ = classifyWappiHealth(false, "open", "", "", now)
	if h != "unauthorized" {
		t.Fatalf("unauth got %s", h)
	}
	h, _, _ = classifyWappiHealth(true, "close", "", "", now)
	if h != "offline" {
		t.Fatalf("offline got %s", h)
	}
	h, _, _ = classifyWappiHealth(true, "open", "2099-01-01T00:00:00Z", "online", now)
	if h != "ok" {
		t.Fatalf("ok got %s", h)
	}
}
