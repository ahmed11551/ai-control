package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestDeptAccessDeniedMsg(t *testing.T) {
	if deptAccessDeniedMsg != "Отдел временно отключён администратором" {
		t.Fatalf("msg=%q", deptAccessDeniedMsg)
	}
}

func TestDeptLoginBlocked(t *testing.T) {
	cases := []struct {
		name          string
		role, userID  string
		hasDept       bool
		accessEnabled bool
		wantBlocked   bool
	}{
		{"admin always ok", "admin", "u1", true, false, false},
		{"Admin case", "Admin", "u1", true, false, false},
		{"empty user", "manager", "", true, false, false},
		{"no department", "manager", "u1", false, false, false},
		{"dept on", "manager", "u1", true, true, false},
		{"dept off manager", "manager", "u1", true, false, true},
		{"dept off rop", "rop", "u1", true, false, true},
		{"dept off sales", "sales", "u1", true, false, true},
		{"dept off logist", "logist", "u1", true, false, true},
		{"other dept not in call", "manager", "u2", true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deptLoginBlocked(tc.role, tc.userID, tc.hasDept, tc.accessEnabled)
			if got != tc.wantBlocked {
				t.Fatalf("got %v want %v", got, tc.wantBlocked)
			}
		})
	}
}

func TestUserDeptAccessDeniedEarlyExit(t *testing.T) {
	a := &App{} // без БД: ранние выходы не ходят в QueryRow
	if a.userDeptAccessDenied(context.Background(), "", "manager") {
		t.Fatal("empty user must not block")
	}
	if a.userDeptAccessDenied(context.Background(), "u1", "admin") {
		t.Fatal("admin must not block")
	}
	if a.userDeptAccessDenied(context.Background(), "u1", "ADMIN") {
		t.Fatal("ADMIN must not block")
	}
}

func TestDeptAccessEnabledEmpty(t *testing.T) {
	a := &App{}
	if !a.deptAccessEnabled(context.Background(), "") {
		t.Fatal("empty dept id must be treated as enabled")
	}
	if !a.deptAccessEnabled(context.Background(), "   ") {
		t.Fatal("blank dept id must be treated as enabled")
	}
}

func TestPostDepartmentAccessForbidden(t *testing.T) {
	a := &App{}
	req := httptest.NewRequest(http.MethodPost, "/departments/x/access", strings.NewReader(`{"enabled":false}`))
	req = req.WithContext(context.WithValue(req.Context(), ctxRole, "manager"))
	rr := httptest.NewRecorder()
	a.postDepartmentAccess(rr, req)
	if rr.Code != 403 {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var out map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if out["error"] == "" {
		t.Fatal("expected error")
	}
}

func TestPostDepartmentAccessBadBody(t *testing.T) {
	a := &App{}
	req := httptest.NewRequest(http.MethodPost, "/departments/x/access", strings.NewReader(`{}`))
	req = req.WithContext(context.WithValue(req.Context(), ctxRole, "admin"))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "11111111-1111-1111-1111-111111111111")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	a.postDepartmentAccess(rr, req)
	if rr.Code != 400 {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPostDepartmentAccessMissingID(t *testing.T) {
	a := &App{}
	req := httptest.NewRequest(http.MethodPost, "/departments//access", strings.NewReader(`{"enabled":true}`))
	req = req.WithContext(context.WithValue(req.Context(), ctxRole, "admin"))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	a.postDepartmentAccess(rr, req)
	if rr.Code != 400 {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestOnlyDisabledDeptBlocks(t *testing.T) {
	// имитация двух отделов: выключенный блокирует только своих
	disabled := deptLoginBlocked("manager", "user-in-off", true, false)
	enabled := deptLoginBlocked("manager", "user-in-on", true, true)
	if !disabled {
		t.Fatal("user in disabled dept must be blocked")
	}
	if enabled {
		t.Fatal("user in enabled dept must not be blocked")
	}
}
