package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallClientPhone(t *testing.T) {
	if got := callClientPhone("inbound", "+7 999 123-45-67", "79014175784"); got != "79991234567" {
		t.Fatalf("inbound phone %q", got)
	}
	if got := callClientPhone("outbound", "79014175784", "8 (999) 123-45-67"); got != "79991234567" {
		t.Fatalf("outbound phone %q", got)
	}
	if validCallClientPhone("101") {
		t.Fatal("short extension must not become a lead")
	}
	if !validCallClientPhone("79991234567") {
		t.Fatal("mobile must be valid")
	}
}

func TestSamePhoneTail(t *testing.T) {
	if !samePhoneTail("+7 999 123-45-67", "89991234567") {
		t.Fatal("same mobile")
	}
	if samePhoneTail("79991234567", "79990000000") {
		t.Fatal("different mobile")
	}
}

func TestCallLeadLookupSkipsSleeping(t *testing.T) {
	if !strings.Contains(sqlCallLeadByPhone, oldLeadsPipelineKey) || !strings.Contains(sqlCallLeadByPhone, "sleeping") {
		t.Fatal("open-card lookup must skip sleeping leads")
	}
	if !strings.Contains(sqlCallLeadByPhone, "is_lost") {
		t.Fatal("open-card lookup must skip lost leads")
	}
}

func TestOpenCallCardRequiresUser(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/calls/open-card", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	(&App{}).openCallCard(rec, req)
	if rec.Code != 401 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOpenCallCardNoPhone(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/calls/open-card", strings.NewReader(`{"peer":"101"}`))
	ctx := context.WithValue(req.Context(), ctxUserID, "11111111-1111-1111-1111-111111111111")
	rec := httptest.NewRecorder()
	(&App{}).openCallCard(rec, req.WithContext(ctx))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "нет номера") {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOpenCallCardNilDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/calls/open-card", strings.NewReader(`{"peer":"79001234567","uid":"abc_def12"}`))
	ctx := context.WithValue(req.Context(), ctxUserID, "11111111-1111-1111-1111-111111111111")
	rec := httptest.NewRecorder()
	(&App{}).openCallCard(rec, req.WithContext(ctx))
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), "не удалось создать") {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}
