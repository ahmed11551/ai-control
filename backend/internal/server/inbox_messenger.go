package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (a *App) messengerStatus() map[string]any {
	out := map[string]any{
		"max":        a.cfg.MaxBotReady(),
		"wappi":      a.cfg.WappiReady(),
		"wappi_poll": false,
		"hook":       a.cfg.InboxWebhookSecret != "",
		"missing":    a.messengerMissing(),
	}
	if a.cfg.MaxBotReady() {
		u := strings.TrimPrefix(strings.TrimSpace(a.maxBotUsername), "@")
		out["max_username"] = u
		out["max_name"] = a.maxBotName
		if u != "" {
			out["max_link"] = "https://max.ru/" + u
		}
	}
	return out
}

func (a *App) messengerMissing() string {
	if a.cfg.MaxBotReady() || a.cfg.WappiReady() {
		return ""
	}
	return "В .env задайте WAPPI_TOKEN (+ опционально WAPPI_PROFILE_ID) и/или MAX_BOT_TOKEN. Ключи в чат не кладите."
}

func (a *App) wappiSend(ctx context.Context, channel, profileID, chatID, phone, body string) (messageID string, err error) {
	chat := strings.TrimSpace(chatID)
	ph := maxNormPhone(phone)
	if ph == "" {
		ph = maxNormPhone(chat)
	}
	payload := map[string]any{"body": body}
	ch := normChannel(channel)
	switch ch {
	case "max":
		// Docs: recipient (телефон) или chat_id / user_id. Холодная — phone.
		// https://wappi.pro/max-api-documentation
		if chat != "" && chatPhone(chat) == "" {
			payload["chat_id"] = chat
		} else if ph != "" {
			payload["recipient"] = ph
		} else if chat != "" {
			payload["recipient"] = chat
		} else {
			return "", fmt.Errorf("нет получателя")
		}
	default:
		recipient, c := inboxRecipient(chatID, phone)
		if c != "" && (strings.Contains(c, "@") || chatPhone(c) == "") {
			payload["chat_id"] = c
		} else if recipient != "" {
			payload["recipient"] = recipient
		} else {
			return "", fmt.Errorf("нет получателя")
		}
	}
	u := "https://wappi.pro/" + wappiSendPath(channel) + "?profile_id=" + url.QueryEscape(profileID)
	bodyResp, err := a.postMessengerBody(ctx, u, map[string]string{
		"Authorization": a.cfg.WappiToken,
		"Content-Type":  "application/json",
	}, payload, "Wappi")
	if err != nil {
		return "", err
	}
	return extractWappiMessageID(bodyResp), nil
}

func (a *App) sendInboxOut(ctx context.Context, provider, channel, extRef, chatID, phone, body string) (messageID string, err error) {
	via := messengerVia(provider)
	if via == "" && a.cfg.MaxBotReady() && normChannel(channel) == "max" {
		via = "max"
	}
	if via == "" && a.cfg.WappiReady() && normChannel(channel) == "max" {
		via = "wappi"
	}
	if via == "" {
		if strings.TrimSpace(provider) == "" {
			return "", fmt.Errorf("диалог без линии — ответ только у нас")
		}
		return "", fmt.Errorf("неизвестный провайдер линии")
	}
	switch via {
	case "max":
		if !a.cfg.MaxBotReady() {
			return "", fmt.Errorf("нет MAX_BOT_TOKEN")
		}
		return "", a.maxBotSend(ctx, chatID, body)
	case "wappi":
		if !a.cfg.WappiReady() {
			return "", fmt.Errorf("нет WAPPI_TOKEN")
		}
		profileID := strings.TrimSpace(extRef)
		if profileID == "" || looksLikeLegacyWazzupRef(profileID) {
			return "", fmt.Errorf("нет привязанного аккаунта MAX у линии")
		}
		return a.wappiSend(ctx, channel, profileID, chatID, phone, body)
	default:
		return "", fmt.Errorf("неизвестный провайдер линии")
	}
}

func (a *App) postMessenger(ctx context.Context, rawURL string, headers map[string]string, payload map[string]any, label string) error {
	_, err := a.postMessengerBody(ctx, rawURL, headers, payload, label)
	return err
}

func (a *App) postMessengerBody(ctx context.Context, rawURL string, headers map[string]string, payload map[string]any, label string) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	cli := &http.Client{Timeout: 20 * time.Second}
	res, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s недоступен (%v)", label, err)
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if res.StatusCode >= 300 {
		log.Printf("%s reject %d (payload keys ok, body redacted len=%d)", label, res.StatusCode, len(respBody))
		return respBody, messengerRejectErr(label, res.StatusCode, respBody)
	}
	return respBody, nil
}

func looksLikeLegacyWazzupRef(s string) bool {
	// Старые channelId Wazzup — UUID; profile_id Wappi короче / другого формата.
	s = strings.TrimSpace(s)
	return len(s) == 36 && strings.Count(s, "-") == 4
}

func messengerVia(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "max", "maxbot":
		return "max"
	case "wappi", "wazzup": // wazzup — legacy линии, шлём через Wappi
		return "wappi"
	default:
		return ""
	}
}

func wappiSendPath(channel string) string {
	switch normChannel(channel) {
	case "telegram":
		return "tapi/sync/message/send"
	case "max":
		return "maxapi/sync/message/send"
	default:
		return "api/sync/message/send"
	}
}

func inboxRecipient(chatID, phone string) (recipient, chat string) {
	chat = strings.TrimSpace(chatID)
	recipient = strings.TrimSpace(phone)
	if recipient == "" {
		recipient = chatPhone(chat)
	}
	if recipient == "" {
		recipient = chat
	}
	return recipient, chat
}

func chatPhone(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "@"); i > 0 {
		s = s[:i]
	}
	return digitsOnly(s)
}

func messengerRejectErr(label string, status int, body []byte) error {
	detail := ""
	var parsed map[string]any
	if json.Unmarshal(body, &parsed) == nil {
		if d, ok := parsed["detail"].(string); ok {
			detail = strings.TrimSpace(d)
		}
	}
	if msg := friendlyMessengerDetail(detail); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	if detail != "" {
		// Без сырого JSON — только короткий detail, без номеров.
		d := redactPhonesInText(detail)
		if len(d) > 120 {
			d = d[:120]
		}
		return fmt.Errorf("%s: %s", label, d)
	}
	return fmt.Errorf("%s отклонил сообщение (%d)", label, status)
}

func friendlyMessengerDetail(detail string) string {
	d := strings.ToLower(strings.TrimSpace(detail))
	switch {
	case strings.Contains(d, "failed to find user by phone"),
		strings.Contains(d, "user not found"),
		strings.Contains(d, "chat not found"):
		return "Контакт не зарегистрирован в MAX."
	case strings.Contains(d, "not authorized"), strings.Contains(d, "unauthorized"), strings.Contains(d, "forbidden"):
		return "Нет доступа к профилю мессенджера — проверьте токен линии"
	case strings.Contains(d, "profile"), strings.Contains(d, "not found") && strings.Contains(d, "profile"):
		return "Профиль мессенджера не найден — проверьте линию"
	case strings.Contains(d, "rate limit"), strings.Contains(d, "too many"):
		return "Слишком много запросов к мессенджеру — подождите и повторите"
	default:
		return ""
	}
}
