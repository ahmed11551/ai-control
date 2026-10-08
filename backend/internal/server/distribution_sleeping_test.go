package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSleepingDistHiddenFromOtherRoles(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/distribution/team?pool=sleeping", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxRole, "manager"))
	rec := httptest.NewRecorder()
	(&App{}).listDistributionTeam(rec, req)
	if rec.Code != 403 {
		t.Fatalf("manager code=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/distribution/team?pool=sleeping", nil)
	ctx := context.WithValue(req.Context(), ctxRole, "rop")
	ctx = context.WithValue(ctx, ctxUserID, "rop-other")
	rec = httptest.NewRecorder()
	(&App{}).listDistributionTeam(rec, req.WithContext(ctx))
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), "администратора") {
		t.Fatalf("other rop code=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/distribution/team?pool=sleeping", strings.NewReader(`{"auto":false}`))
	req = req.WithContext(context.WithValue(req.Context(), ctxRole, "rop"))
	rec = httptest.NewRecorder()
	(&App{}).saveDistributionTeam(rec, req)
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), "администратора") {
		t.Fatalf("save code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSleepingDrainNoopWithoutDB(t *testing.T) {
	n, err := (&App{}).drainSleepingLeads(context.Background(), 5)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if (&App{}).departmentIsTema(context.Background(), "dept") {
		t.Fatal("nil db must not look like Тема")
	}
	if (&App{}).temaDepartmentID(context.Background()) != "" {
		t.Fatal("nil db")
	}
	if (&App{}).distSleepingAutoOn(context.Background(), "dept") {
		t.Fatal("nil db must not turn sleeping auto on")
	}
}
