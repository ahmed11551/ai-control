package server

import (
	"context"
	"net/http"
	"testing"
)

func TestStaffDialogRoleOK(t *testing.T) {
	for _, role := range []string{"admin", "logist", "night_logist", "manager", "sales", "rop", "Admin"} {
		if !staffDialogRoleOK(role) {
			t.Fatalf("role %q must be allowed", role)
		}
	}
	if staffDialogRoleOK("guest") || staffDialogRoleOK("") {
		t.Fatal("guest must be denied")
	}
}

func TestStaffDialogSeesAll(t *testing.T) {
	mk := func(role string) *http.Request {
		r, _ := http.NewRequest(http.MethodGet, "/staff-dialogs", nil)
		ctx := context.WithValue(r.Context(), ctxRole, role)
		return r.WithContext(ctx)
	}
	if !staffDialogSeesAll(mk("admin")) {
		t.Fatal("admin must see all")
	}
	if !staffDialogSeesAll(mk("logist")) {
		t.Fatal("logist must see all")
	}
	if !staffDialogSeesAll(mk("night_logist")) {
		t.Fatal("night logist must see all")
	}
	if staffDialogSeesAll(mk("manager")) {
		t.Fatal("manager must not see all")
	}
	if staffDialogSeesAll(mk("rop")) {
		t.Fatal("rop must not see all")
	}
	if staffDialogSeesAll(mk("sales")) {
		t.Fatal("sales must not see all")
	}
}

func TestStaffDialogAllowsMessage(t *testing.T) {
	if !staffDialogAllowsMessage("open") || !staffDialogAllowsMessage("Open") {
		t.Fatal("open must allow messages")
	}
	if staffDialogAllowsMessage("closed") || staffDialogAllowsMessage("") {
		t.Fatal("closed/empty must deny messages")
	}
}

func TestStaffDialogOpenClosedStatus(t *testing.T) {
	allowed := map[string]bool{"open": true, "closed": true}
	for _, s := range []string{"open", "closed"} {
		if !allowed[s] {
			t.Fatalf("missing %s", s)
		}
	}
	if allowed["pending"] {
		t.Fatal("pending must not be a dialog status")
	}
}
