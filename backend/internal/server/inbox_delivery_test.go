package server

import (
	"strings"
	"testing"
)

func TestNormalizeWappiDeliveryStatus(t *testing.T) {
	cases := map[string]string{
		"pending":       "pending",
		"delivered":     "delivered",
		"read":          "read",
		"undelivered":   "failed",
		"error":         "failed",
		"temporary ban": "failed",
		"sent":          "sent",
		"":              "",
		"foo":           "",
	}
	for in, want := range cases {
		if got := normalizeWappiDeliveryStatus(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestCanUpgradeDeliveryStatus(t *testing.T) {
	if !canUpgradeDeliveryStatus("", "sent") {
		t.Fatal("empty → sent")
	}
	if !canUpgradeDeliveryStatus("sent", "delivered") {
		t.Fatal("sent → delivered")
	}
	if !canUpgradeDeliveryStatus("delivered", "read") {
		t.Fatal("delivered → read")
	}
	if canUpgradeDeliveryStatus("read", "delivered") {
		t.Fatal("read must not downgrade to delivered")
	}
	if canUpgradeDeliveryStatus("sent", "pending") {
		t.Fatal("sent must not downgrade to pending")
	}
	if !canUpgradeDeliveryStatus("sent", "failed") {
		t.Fatal("sent → failed")
	}
	if canUpgradeDeliveryStatus("read", "failed") {
		t.Fatal("read must not become failed")
	}
}

func TestApplyWappiDeliveryStatusesParsesObject(t *testing.T) {
	raw := map[string]any{
		"messages": map[string]any{
			"id":         "114986604267657812",
			"wh_type":    "delivery_status",
			"status":     "delivered",
			"profile_id": "d6f3286b-e584",
		},
	}
	flat := flattenInbox(raw["messages"].(map[string]any))
	wh := scalarKeys(flat, "wh_type", "type")
	st := normalizeWappiDeliveryStatus(scalarKeys(flat, "status", "delivery_status"))
	id := scalarKeys(flat, "id", "message_id")
	if !strings.Contains(wh, "delivery") || st != "delivered" || id != "114986604267657812" {
		t.Fatalf("wh=%q st=%q id=%q", wh, st, id)
	}
}

func TestInboxEventsStillSkipsCreatingMessageFromDelivery(t *testing.T) {
	raw := map[string]any{
		"messages": []any{
			map[string]any{"wh_type": "delivery_status", "id": "x", "chatId": "1", "status": "delivered"},
		},
	}
	if ev := inboxEvents("wappi", raw); len(ev) != 0 {
		t.Fatalf("delivery must not create chat message, got %+v", ev)
	}
}

func TestExtractWappiMessageID(t *testing.T) {
	body := []byte(`{"status":"done","message_id":"116181216826565519","detail":"Message sent successfully"}`)
	if got := extractWappiMessageID(body); got != "116181216826565519" {
		t.Fatalf("got %q", got)
	}
}
