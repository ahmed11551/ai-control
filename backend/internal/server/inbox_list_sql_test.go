package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInboxLeadJoinUsesPhoneTail(t *testing.T) {
	if strings.Contains(inboxLeadJoin, "regexp_replace") || strings.Contains(inboxLeadJoin, "LIKE") {
		t.Fatal("lead lookup must not scan every contact phone")
	}
	for _, need := range []string{"phone_tail", "le.contact_id=t.contact_id", "ORDER BY le.created_at DESC"} {
		if !strings.Contains(inboxLeadJoin, need) {
			t.Fatalf("missing %s", need)
		}
	}
}

func TestInboxListAccessSkipsPhoneScan(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/inbox", nil)
	ctx := context.WithValue(req.Context(), ctxUserID, "11111111-1111-1111-1111-111111111111")
	req = req.WithContext(context.WithValue(ctx, ctxRole, "manager"))
	sql, args := inboxThreadListAccessSQL(req, "t", "l", nil)
	if strings.Contains(sql, "phone_tail") || strings.Contains(sql, "regexp_replace") || strings.Contains(sql, "ld.") {
		t.Fatalf("list filter still scans phones: %s", sql)
	}
	for _, need := range []string{"owner_user_id", "author_id", "le.contact_id=t.contact_id", "d2.owner_id"} {
		if !strings.Contains(sql, need) {
			t.Fatalf("missing %s in %s", need, sql)
		}
	}
	if len(args) != 1 {
		t.Fatalf("args %d", len(args))
	}
}
