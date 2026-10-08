package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDealListWhereSourceFilter(t *testing.T) {
	a := &App{}
	withRole := func(url string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		ctx := context.WithValue(req.Context(), ctxRole, "admin")
		ctx = context.WithValue(ctx, ctxUserID, "admin-1")
		return req.WithContext(ctx)
	}
	req := withRole("/deals?source=manual")
	clause, _ := a.dealListWhere(req)
	if !strings.Contains(clause, "manual") {
		t.Fatalf("manual filter missing: %s", clause)
	}
	req = withRole("/deals?source=vk_ads")
	clause, _ = a.dealListWhere(req)
	if !strings.Contains(clause, "vk_ads") {
		t.Fatalf("vk filter missing: %s", clause)
	}
	req = withRole("/deals?source=vk")
	clause, _ = a.dealListWhere(req)
	if !strings.Contains(clause, "vk_ads") {
		t.Fatalf("vk alias must map to vk_ads: %s", clause)
	}
	req = withRole("/deals")
	clause, _ = a.dealListWhere(req)
	// без source не должно быть фильтра по лиду-source (кроме прочих условий)
	if strings.Contains(clause, "ld.source") {
		t.Fatalf("no source param must not filter ld.source: %s", clause)
	}
}
