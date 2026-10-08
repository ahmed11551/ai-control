package server

import (
	"context"
	"net/http"
	"testing"
)

func TestInboxSeesAllMessagesRoles(t *testing.T) {
	mk := func(role string) *http.Request {
		r, _ := http.NewRequest(http.MethodGet, "/inbox", nil)
		ctx := context.WithValue(r.Context(), ctxRole, role)
		return r.WithContext(ctx)
	}
	if !inboxSeesAllMessages(mk("admin")) {
		t.Fatal("admin must see all")
	}
	if !inboxSeesAllMessages(mk("logist")) {
		t.Fatal("logist must see all")
	}
	if inboxSeesAllMessages(mk("rop")) {
		t.Fatal("rop must not see all chats")
	}
	if inboxSeesAllMessages(mk("manager")) {
		t.Fatal("manager must not see all")
	}
}
