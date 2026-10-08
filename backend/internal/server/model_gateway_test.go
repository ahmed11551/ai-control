package server

import (
	"context"
	"strings"
	"testing"
)

func TestMaskForModel(t *testing.T) {
	got := maskForModel("паспорт 4510 123456, звоните +7 999 123-45-67")
	if strings.Contains(got, "4510") || strings.Contains(got, "999") {
		t.Fatalf("mask left digits: %q", got)
	}
	if !strings.Contains(got, "[паспорт]") || !strings.Contains(got, "[телефон]") {
		t.Fatalf("mask markers: %q", got)
	}
}

func TestModelGatewayStaysLocal(t *testing.T) {
	t.Setenv("MODEL_GATEWAY_URL", "")
	if _, err := modelGatewayClassify(context.Background(), "паспорт 4510 123456"); err == nil {
		t.Fatal("empty gateway must not call out")
	}
	if hit, ok := classifyOutbound(context.Background(), "Добрый день"); ok {
		t.Fatalf("ordinary text blocked: %+v", hit)
	}
	hit, ok := classifyOutbound(context.Background(), "Пришлите серию и номер паспорта")
	if !ok || hit.Rule != rulePassportRequest {
		t.Fatalf("passport request must require approval: %+v %v", hit, ok)
	}
}
