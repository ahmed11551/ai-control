package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// errModelUnavailable — AI выключен или не ответил. Решение остаётся за правилами.
var errModelUnavailable = errors.New("model gateway unavailable")

var phoneLikeRe = regexp.MustCompile(`(?:\+?\d[\d\s\-()]{8,}\d|\d{10,15})`)

// maskForModel убирает телефоны и серию/номер паспорта до любой внешней отправки.
func maskForModel(raw string) string {
	s := passportSeriesNumRe.ReplaceAllString(raw, "[паспорт]")
	return phoneLikeRe.ReplaceAllString(s, "[телефон]")
}

// classifyOutbound — сначала правила. Внешний AI только если задан MODEL_GATEWAY_URL,
// и только по уже замаскированному тексту. Без шлюза текст никуда не уходит.
func classifyOutbound(ctx context.Context, raw string) (PolicyHit, bool) {
	if hit, ok := checkOutboundPolicy(raw); ok {
		return hit, true
	}
	label, err := modelGatewayClassify(ctx, maskForModel(raw))
	if err != nil || !strings.EqualFold(strings.TrimSpace(label), "block") {
		return PolicyHit{}, false
	}
	return PolicyHit{
		Rule:     "ai_signal",
		Severity: "medium",
		Message:  "Сообщение не отправлено: проверка безопасности не пропустила текст.",
	}, true
}

func modelGatewayClassify(ctx context.Context, masked string) (string, error) {
	endpoint := strings.TrimSpace(os.Getenv("MODEL_GATEWAY_URL"))
	if endpoint == "" {
		return "", errModelUnavailable
	}
	body, _ := json.Marshal(map[string]string{"text": masked})
	reqCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", errModelUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errModelUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", errModelUnavailable
	}
	var out struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", errModelUnavailable
	}
	return out.Action, nil
}
