package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const telegramAPI = "https://api.telegram.org"

func (a *App) telegramReady() bool {
	return strings.TrimSpace(a.cfg.TelegramBotToken) != ""
}

func (a *App) ensureTelegramSchema(ctx context.Context) {
	_, _ = a.db.Exec(ctx, `
CREATE TABLE IF NOT EXISTS telegram_notify_chats (
  chat_id BIGINT PRIMARY KEY,
  label TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`)
}

func (a *App) startTelegramBot() {
	if !a.telegramReady() {
		log.Println("Telegram bot: TELEGRAM_BOT_TOKEN не задан — уведомления логистам выкл")
		return
	}
	a.ensureTelegramSchema(context.Background())
	log.Println("Telegram bot: long poll запущен")
	go a.telegramPollLoop()
}

type tgUser struct {
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

func (a *App) telegramPollLoop() {
	offset := 0
	client := &http.Client{Timeout: 60 * time.Second}
	for {
		u := fmt.Sprintf("%s/bot%s/getUpdates?timeout=50&offset=%d", telegramAPI, a.cfg.TelegramBotToken, offset)
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		res, err := client.Do(req)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
		_ = res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			time.Sleep(5 * time.Second)
			continue
		}
		var parsed struct {
			OK     bool `json:"ok"`
			Result []struct {
				UpdateID int64 `json:"update_id"`
				Message  *struct {
					Text string `json:"text"`
					Chat struct {
						ID int64 `json:"id"`
					} `json:"chat"`
					From *tgUser `json:"from"`
				} `json:"message"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil || !parsed.OK {
			time.Sleep(3 * time.Second)
			continue
		}
		for _, upd := range parsed.Result {
			if int(upd.UpdateID)+1 > offset {
				offset = int(upd.UpdateID) + 1
			}
			if upd.Message == nil {
				continue
			}
			a.handleTelegramMessage(upd.Message.Chat.ID, upd.Message.Text, upd.Message.From)
		}
	}
}

func (a *App) handleTelegramMessage(chatID int64, text string, from *tgUser) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	lower := strings.ToLower(text)
	label := ""
	if from != nil {
		label = strings.TrimSpace(from.FirstName)
		if from.Username != "" {
			if label != "" {
				label += " @" + from.Username
			} else {
				label = "@" + from.Username
			}
		}
	}
	if strings.HasPrefix(lower, "/start") {
		a.ensureTelegramSchema(context.Background())
		_, _ = a.db.Exec(context.Background(), `
			INSERT INTO telegram_notify_chats (chat_id, label)
			VALUES ($1, $2)
			ON CONFLICT (chat_id) DO UPDATE SET label=EXCLUDED.label`, chatID, label)
		_ = a.telegramSend(chatID,
			"Подписка на заявки «Билет» включена.\n"+
				"Когда карточку отправят логисту, сюда придёт уведомление.\n"+
				"chat_id: "+strconv.FormatInt(chatID, 10))
		return
	}
	if strings.HasPrefix(lower, "/stop") {
		_, _ = a.db.Exec(context.Background(), `DELETE FROM telegram_notify_chats WHERE chat_id=$1`, chatID)
		_ = a.telegramSend(chatID, "Подписка отключена.")
		return
	}
	if strings.HasPrefix(lower, "/help") {
		_ = a.telegramSend(chatID, "/start — получать заявки «Билет»\n/stop — отписаться")
	}
}

func (a *App) telegramSend(chatID int64, text string) error {
	if !a.telegramReady() || chatID == 0 || strings.TrimSpace(text) == "" {
		return nil
	}
	return a.telegramSendToken(a.cfg.TelegramBotToken, chatID, text)
}

func (a *App) telegramSendToken(token string, chatID int64, text string) error {
	token = strings.TrimSpace(token)
	if token == "" || chatID == 0 || strings.TrimSpace(text) == "" {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})
	url := fmt.Sprintf("%s/bot%s/sendMessage", telegramAPI, token)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("telegram sendMessage %d", res.StatusCode)
	}
	return nil
}

func (a *App) telegramChatIDs(ctx context.Context) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, 8)
	add := func(id int64) {
		if id == 0 || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, part := range strings.Split(a.cfg.TelegramLogistChatIDs, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.ParseInt(part, 10, 64)
		if err == nil {
			add(n)
		}
	}
	a.ensureTelegramSchema(ctx)
	rows := a.queryMaps(ctx, `SELECT chat_id::bigint AS chat_id FROM telegram_notify_chats`)
	for _, row := range rows {
		switch v := row["chat_id"].(type) {
		case int64:
			add(v)
		case int32:
			add(int64(v))
		case float64:
			add(int64(v))
		case string:
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				add(n)
			}
		default:
			s := strings.TrimSpace(fmt.Sprint(v))
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				add(n)
			}
		}
	}
	return out
}

// notifyLogistsDealToBilet — Telegram + in-app, когда сделка ушла логисту («Билет»).
func (a *App) notifyLogistsDealToBilet(ctx context.Context, dealID string) {
	dealID = strings.TrimSpace(dealID)
	if dealID == "" {
		return
	}
	var title, ownerName, phone string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(d.title,''),
		       coalesce(u.full_name,''),
		       coalesce(nullif(c.phone,''), nullif(l.phone,''), '')
		FROM deals d
		LEFT JOIN users u ON u.id=d.owner_id
		LEFT JOIN contacts c ON c.id=d.contact_id
		LEFT JOIN leads l ON l.id=d.lead_id
		WHERE d.id=$1::uuid`, dealID).Scan(&title, &ownerName, &phone)
	if title == "" {
		title = "Сделка"
	}
	body := title
	if ownerName != "" {
		body += " · менеджер: " + ownerName
	}
	if phone != "" {
		body += " · " + phone
	}

	// In-app всем логистам и админам.
	users := a.queryMaps(ctx, `
		SELECT u.id::text
		FROM users u
		JOIN roles r ON r.id=u.role_id
		WHERE u.is_active AND lower(coalesce(r.name,'')) IN ('logist','admin')`)
	for _, u := range users {
		uid := strings.TrimSpace(fmt.Sprint(u["id"]))
		if uid == "" {
			continue
		}
		a.pushUserNotification(ctx, uid, "logistics_bilet", "Новая заявка: Билет", body, map[string]any{
			"kind":    "logistics_bilet",
			"deal_id": dealID,
		}, "deal_id", dealID)
	}

	if !a.telegramReady() {
		return
	}
	link := ""
	if base := strings.TrimSpace(a.cfg.PublicBaseURL); base != "" {
		link = strings.TrimRight(base, "/") + "/crm?deal=" + dealID
	}
	msg := "<b>Новая заявка логисту</b>\n" +
		"Стадия: <b>Билет</b>\n" +
		escapeTelegramHTML(title)
	if ownerName != "" {
		msg += "\nМенеджер: " + escapeTelegramHTML(ownerName)
	}
	if phone != "" {
		msg += "\nТел: " + escapeTelegramHTML(phone)
	}
	if link != "" {
		msg += "\n<a href=\"" + link + "\">Открыть в CRM</a>"
	}
	chats := a.telegramChatIDs(ctx)
	for _, chatID := range chats {
		if err := a.telegramSend(chatID, msg); err != nil {
			log.Printf("telegram notify chat=%d: %v", chatID, err)
		}
	}
}

func escapeTelegramHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func (a *App) isHireBiletStageID(ctx context.Context, stageID string) bool {
	stageID = strings.TrimSpace(stageID)
	if stageID == "" {
		return false
	}
	var key string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(bitrix_id,'') FROM pipeline_stages WHERE id=$1::uuid`, stageID).Scan(&key)
	return key == hirePipelineKey+":bilet"
}
