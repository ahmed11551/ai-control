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
	"strconv"
	"strings"
	"time"
)

const maxBotAPI = "https://platform-api2.max.ru"

func (a *App) maxBotSend(ctx context.Context, chatID, body string) error {
	chat := strings.TrimSpace(chatID)
	if chat == "" {
		return fmt.Errorf("нет chat_id")
	}
	q := url.Values{}
	q.Set("chat_id", chat)
	err := a.maxBotPOST(ctx, maxBotAPI+"/messages?"+q.Encode(), map[string]any{"text": body})
	if err == nil {
		return nil
	}
	if _, perr := strconv.ParseInt(chat, 10, 64); perr == nil && !strings.HasPrefix(chat, "-") {
		q2 := url.Values{}
		q2.Set("user_id", chat)
		return a.maxBotPOST(ctx, maxBotAPI+"/messages?"+q2.Encode(), map[string]any{"text": body})
	}
	return err
}

func (a *App) maxBotPOST(ctx context.Context, rawURL string, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", a.cfg.MaxBotToken)
	req.Header.Set("Content-Type", "application/json")
	cli := maxBotHTTPClient(20 * time.Second)
	res, err := cli.Do(req)
	if err != nil {
		return fmt.Errorf("MAX недоступен")
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	if res.StatusCode >= 300 {
		return fmt.Errorf("MAX отклонил сообщение (%d)", res.StatusCode)
	}
	return nil
}

func maxBotEvents(raw map[string]any) []inboxEv {
	if list, ok := raw["updates"].([]any); ok {
		var out []inboxEv
		for _, item := range list {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			if ev, ok := maxBotUpdateEv(m); ok {
				out = append(out, ev)
			}
		}
		return out
	}
	if ev, ok := maxBotUpdateEv(raw); ok {
		return []inboxEv{ev}
	}
	return nil
}

func maxBotUpdateEv(raw map[string]any) (inboxEv, bool) {
	typ := strings.ToLower(firstScalar(raw["update_type"]))
	switch typ {
	case "message_created", "message_edited":
		msg, _ := raw["message"].(map[string]any)
		if msg == nil {
			return inboxEv{}, false
		}
		sender, _ := msg["sender"].(map[string]any)
		if sender != nil && truthyAny(sender["is_bot"]) {
			return inboxEv{}, false
		}
		recipient, _ := msg["recipient"].(map[string]any)
		bodyMap, _ := msg["body"].(map[string]any)
		chatID := firstScalar(recipient["chat_id"])
		if chatID == "" {
			chatID = firstScalar(raw["chat_id"])
		}
		if chatID == "" && sender != nil {
			chatID = firstScalar(sender["user_id"])
		}
		text := ""
		extID := ""
		if bodyMap != nil {
			text = strings.TrimSpace(firstScalar(bodyMap["text"]))
			extID = firstScalar(bodyMap["mid"])
			if text == "" {
				if atts, ok := bodyMap["attachments"].([]any); ok && len(atts) > 0 {
					text = "[вложение]"
				}
			}
		}
		title := ""
		if sender != nil {
			title = strings.TrimSpace(strings.Join([]string{
				firstScalar(sender["first_name"]),
				firstScalar(sender["last_name"]),
			}, " "))
			if title == "" {
				title = firstScalar(sender["name"])
			}
			if title == "" {
				title = firstScalar(sender["username"])
			}
		}
		if text == "" && chatID == "" {
			return inboxEv{}, false
		}
		if text == "" {
			text = "[сообщение]"
		}
		return inboxEv{
			Channel:   "max",
			ChatID:    chatID,
			Title:     title,
			Body:      text,
			ExtID:     extID,
			Direction: "inbound",
			Profile:   "bot",
		}, true
	case "bot_started":
		chatID := firstScalar(raw["chat_id"])
		user, _ := raw["user"].(map[string]any)
		title := ""
		leadID, contactID, dealID := "", "", ""
		payload := strings.TrimSpace(firstScalar(raw["payload"]))
		switch {
		case strings.HasPrefix(payload, "lead_"):
			leadID = strings.TrimPrefix(payload, "lead_")
		case strings.HasPrefix(payload, "deal_"):
			dealID = strings.TrimPrefix(payload, "deal_")
		case strings.HasPrefix(payload, "contact_"):
			contactID = strings.TrimPrefix(payload, "contact_")
		}
		if user != nil {
			title = firstScalar(user["name"])
			if title == "" {
				title = strings.TrimSpace(strings.Join([]string{
					firstScalar(user["first_name"]),
					firstScalar(user["last_name"]),
				}, " "))
			}
			if title == "" {
				title = firstScalar(user["username"])
			}
			if chatID == "" {
				chatID = firstScalar(user["user_id"])
			}
		}
		if chatID == "" {
			return inboxEv{}, false
		}
		if title == "" {
			title = chatID
		}
		return inboxEv{
			Channel:   "max",
			ChatID:    chatID,
			Title:     title,
			Body:      "[старт бота]",
			ExtID:     "start:" + chatID + ":" + firstScalar(raw["timestamp"]),
			Direction: "inbound",
			Profile:   "bot",
			LeadID:    leadID,
			ContactID: contactID,
			DealID:    dealID,
		}, true
	default:
		return inboxEv{}, false
	}
}

func truthyAny(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true") || t == "1"
	case float64:
		return t != 0
	case json.Number:
		n, _ := t.Float64()
		return n != 0
	default:
		return false
	}
}

func (a *App) ensureMaxBotLine(ctx context.Context) {
	if !a.cfg.MaxBotReady() {
		return
	}
	name := "MAX Bot"
	ref := "bot"
	if me, err := a.maxBotMe(ctx); err == nil {
		if n := strings.TrimSpace(firstScalar(me["name"])); n != "" {
			name = n
			a.maxBotName = n
		}
		if u := strings.TrimSpace(firstScalar(me["username"])); u != "" {
			a.maxBotUsername = strings.TrimPrefix(u, "@")
			if a.maxBotName == "" {
				name = "@" + a.maxBotUsername
			}
		}
		if uid := firstScalar(me["user_id"]); uid != "" {
			ref = uid
		}
	} else {
		log.Println("MAX Bot /me:", err)
	}
	var id string
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM inbox_lines
		WHERE provider='max' AND channel='max' AND is_active
		ORDER BY name LIMIT 1`).Scan(&id)
	if id != "" {
		log.Println("MAX Bot: линия inbox готова")
		return
	}
	_, _ = a.db.Exec(ctx, `
		INSERT INTO inbox_lines (provider, channel, name, kind, operator_label, external_ref, is_active)
		SELECT 'max','max',$1,'pool','',$2,true
		WHERE NOT EXISTS (
			SELECT 1 FROM inbox_lines WHERE provider='max' AND channel='max' AND is_active
		)`, name, ref)
	log.Println("MAX Bot: линия inbox готова")
}

func (a *App) maxBotMe(ctx context.Context) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, maxBotAPI+"/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", a.cfg.MaxBotToken)
	cli := maxBotHTTPClient(15 * time.Second)
	res, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("MAX /me %d", res.StatusCode)
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *App) maxBotPollLoop() {
	if !a.cfg.MaxBotReady() || !a.cfg.MaxBotPoll {
		return
	}
	log.Println("MAX Bot: long polling /updates")
	var marker int64
	for {
		updates, next, err := a.maxBotFetchUpdates(marker)
		if err != nil {
			log.Println("MAX Bot poll:", err)
			time.Sleep(3 * time.Second)
			continue
		}
		if next > 0 {
			marker = next
		}
		if len(updates) == 0 {
			continue
		}
		log.Printf("MAX Bot: получено обновлений %d (marker=%d)", len(updates), marker)
		for _, raw := range updates {
			evs := maxBotEvents(raw)
			for _, ev := range evs {
				if ev.Body == "" && ev.ChatID == "" {
					continue
				}
				if err := a.upsertInbox(context.Background(), ev); err != nil {
					log.Println("MAX Bot upsert:", err)
					continue
				}
				log.Printf("MAX Bot ← %s chat=%s %q", ev.Direction, ev.ChatID, trimLog(ev.Body, 60))
				if a.hub != nil {
					a.hub.Broadcast("inbox.updated", map[string]any{
						"channel": ev.Channel,
						"chat_id": ev.ChatID,
					})
				}
			}
		}
	}
}

func (a *App) maxBotFetchUpdates(marker int64) ([]map[string]any, int64, error) {
	const serverTimeout = 25
	q := url.Values{}
	q.Set("limit", "100")
	q.Set("timeout", strconv.Itoa(serverTimeout))
	q.Set("types", "message_created,message_edited,bot_started")
	if marker > 0 {
		q.Set("marker", strconv.FormatInt(marker, 10))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(serverTimeout+10)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, maxBotAPI+"/updates?"+q.Encode(), nil)
	if err != nil {
		return nil, marker, err
	}
	req.Header.Set("Authorization", a.cfg.MaxBotToken)
	cli := maxBotHTTPClient(time.Duration(serverTimeout+10) * time.Second)
	res, err := cli.Do(req)
	if err != nil {
		return nil, marker, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		return nil, marker, fmt.Errorf("MAX /updates %d: %s", res.StatusCode, trimLog(string(raw), 120))
	}
	var parsed struct {
		Updates []map[string]any `json:"updates"`
		Marker  *int64           `json:"marker"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, marker, err
	}
	next := marker
	if parsed.Marker != nil && *parsed.Marker > 0 {
		next = *parsed.Marker
	}
	return parsed.Updates, next, nil
}

func trimLog(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
