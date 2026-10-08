package server

import (
	"context"
	"net/http"
	"testing"
)

func reqRole(role string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	return r.WithContext(context.WithValue(r.Context(), ctxRole, role))
}

func TestNightLogistRoleMatrix(t *testing.T) {
	if !isLogistRole("logist") || !isLogistRole("night_logist") || !isLogistRole(" Night_Logist ") {
		t.Fatal("both logist roles must share the cabinet")
	}
	if isLogistRole("manager") || isLogistRole("rop") || isLogistRole("") {
		t.Fatal("other roles are not logists")
	}
	if !isLogist(reqRole("night_logist")) || !isLogist(reqRole("logist")) {
		t.Fatal("isLogist must accept night logist")
	}
	if isLogist(reqRole("manager")) {
		t.Fatal("manager is not a logist")
	}

	nightReq := reqRole("night_logist")
	nightReq = nightReq.WithContext(context.WithValue(nightReq.Context(), ctxUserID, "u-night"))
	scope := (&App{}).crmScope(nightReq)
	if !scope.IsLogist || scope.All || scope.IsManager {
		t.Fatalf("night logist scope %+v", scope)
	}
	plainReq := reqRole("logist")
	plainReq = plainReq.WithContext(context.WithValue(plainReq.Context(), ctxUserID, "u-logist"))
	plain := (&App{}).crmScope(plainReq)
	if !plain.IsLogist {
		t.Fatal("plain logist scope")
	}

	if !seeClientPhonesAllowed("night_logist", false) {
		t.Fatal("night logist sees phones without a department flag")
	}
	if seeClientPhonesAllowed("logist", true) || seeClientPhonesAllowed("logist", false) {
		t.Fatal("plain logist must not see phones even if the department flag is on")
	}
	if seeClientPhonesAllowed("manager", false) || !seeClientPhonesAllowed("manager", true) {
		t.Fatal("manager phones still follow the department flag")
	}

	if !seeInboxFilesAllowed("night_logist", false) || !seeInboxFilesAllowed("logist", false) {
		t.Fatal("both logist roles see chat files")
	}
	if seeInboxFilesAllowed("manager", false) {
		t.Fatal("manager chat files stay closed without the department flag")
	}

	for _, role := range []string{"logist", "night_logist"} {
		if !canSeeTicketSensitive(role) || !canDownloadProtectedDocs(role) || !canSeeLogistTicketFiles(role) {
			t.Fatalf("%s must keep ticket and document access", role)
		}
		if !biletDocRoleOK(role) || !canBrowseDealDocs(role) || !staffDialogRoleOK(role) {
			t.Fatalf("%s must keep logistics document and dialog access", role)
		}
		if !inboxSeesAllMessages(reqRole(role)) || !staffDialogSeesAll(reqRole(role)) {
			t.Fatalf("%s must see logistics chats", role)
		}
		if shouldRecordOwnerOpened(role, "u1", "u1", false) {
			t.Fatalf("%s must not mark a lead as opened by a manager", role)
		}
	}

	foundNight := false
	for _, role := range creatableRolesFor("admin") {
		if role == "night_logist" {
			foundNight = true
		}
	}
	if !foundNight {
		t.Fatalf("admin creatable roles: %#v", creatableRolesFor("admin"))
	}
	for _, role := range creatableRolesFor("rop") {
		if role == "night_logist" || role == "logist" {
			t.Fatal("rop must not create logists")
		}
	}
	if normStaffRole("ночной логист") != "night_logist" || normStaffRole("логист") != "logist" {
		t.Fatal("role name normalization")
	}
	if fmtRoleLabel("night_logist") != "Ночной логист" || fmtRoleLabel("logist") != "Логист" {
		t.Fatal("role labels")
	}
	if !nightLogistCanDialSeen("night_logist", true) || nightLogistCanDialSeen("night_logist", false) {
		t.Fatal("night logist dials only a card he can already see")
	}
	if nightLogistCanDialSeen("logist", true) || nightLogistCanDialSeen("manager", true) {
		t.Fatal("day logist and manager do not use the night dial path")
	}
}
