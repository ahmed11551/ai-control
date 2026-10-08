package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// wappiPollLoop тянет чаты MAX через API каждые 5с — пока нет публичного webhook.
func (a *App) wappiPollLoop() {
	if !a.cfg.WappiReady() {
		return
	}
	log.Println("Wappi MAX: poll входящих каждые 5с (без webhook)")
	seen := map[string]string{} // profileID|chatID → last_message_id
	var mu sync.Mutex
	bootstrap := true
	for {
		n, err := a.wappiPollOnce(&mu, seen, bootstrap)
		if err != nil {
			log.Println("Wappi poll:", err)
		} else if n > 0 {
			log.Printf("Wappi poll: новых сообщений %d", n)
		}
		bootstrap = false
		time.Sleep(5 * time.Second)
	}
}

type wappiDialog struct {
	ID            string
	Name          string
	Phone         string
	LastMessageID string
	LastData      string
	Unread        int
}

func (a *App) wappiPollOnce(mu *sync.Mutex, seen map[string]string, bootstrap bool) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	profileID, err := a.wappiProfileID(ctx)
	if err != nil || profileID == "" {
		return 0, err
	}
	dialogs, err := a.wappiListChats(ctx, profileID, 40)
	if err != nil {
		return 0, err
	}
	added := 0
	changedChats := map[string]bool{}
	for _, d := range dialogs {
		if d.ID == "" {
			continue
		}
		key := profileID + "|" + d.ID
		mu.Lock()
		prev := seen[key]
		mu.Unlock()

		if bootstrap {
			mu.Lock()
			seen[key] = d.LastMessageID
			mu.Unlock()
			continue
		}
		if d.LastMessageID == "" || d.LastMessageID == prev {
			continue
		}

		msgs, err := a.wappiChatMessages(ctx, profileID, d.ID, 25)
		if err != nil {
			log.Println("Wappi messages:", d.ID, err)
			continue
		}
		// Только сообщения новее last seen — не выгружаем всю старую переписку чата.
		var fresh []map[string]any
		for _, m := range msgs { // API отдаёт desc (новые сверху)
			mid := firstScalar(m["id"])
			if mid == "" {
				mid = firstScalar(m["message_id"])
			}
			if mid == "" {
				mid = firstScalar(m["messageId"])
			}
			if prev != "" && mid == prev {
				break
			}
			fresh = append(fresh, m)
		}
		for i := len(fresh) - 1; i >= 0; i-- {
			m := fresh[i]
			ev, ok := a.wappiPollMsgToEv(profileID, d, m)
			if !ok {
				continue
			}
			if err := a.upsertInbox(ctx, ev); err != nil {
				continue
			}
			added++
			changedChats[d.ID] = true
		}
		mu.Lock()
		seen[key] = d.LastMessageID
		mu.Unlock()
	}
	if added > 0 && a.hub != nil {
		for chatID := range changedChats {
			a.hub.Broadcast("inbox.updated", map[string]any{
				"channel": "max",
				"chat_id": chatID,
				"via":     "wappi_poll",
			})
		}
	}
	return added, nil
}

func (a *App) wappiListChats(ctx context.Context, profileID string, limit int) ([]wappiDialog, error) {
	if limit <= 0 {
		limit = 40
	}
	u := "https://wappi.pro/maxapi/sync/chats/get?profile_id=" + url.QueryEscape(profileID) +
		"&limit=" + strconv.Itoa(limit) + "&order=desc&show_all=false"
	body, err := a.wappiJSON(ctx, http.MethodPost, u, map[string]any{})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Dialogs []map[string]any `json:"dialogs"`
		Chats   []map[string]any `json:"chats"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("Wappi chats: bad json")
	}
	raw := parsed.Dialogs
	if len(raw) == 0 {
		raw = parsed.Chats
	}
	out := make([]wappiDialog, 0, len(raw))
	for _, m := range raw {
		id := firstScalar(m["id"])
		if id == "" {
			id = firstScalar(m["chat_id"])
		}
		out = append(out, wappiDialog{
			ID:            id,
			Name:          firstScalar(m["name"]),
			Phone:         firstScalar(m["phone"]),
			LastMessageID: firstScalar(m["last_message_id"]),
			LastData:      firstScalar(m["last_message_data"]),
			Unread:        asInt(m["unread_count"]) + asInt(m["wapi_unread_count"]),
		})
	}
	return out, nil
}

func (a *App) wappiChatMessages(ctx context.Context, profileID, chatID string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 20
	}
	u := "https://wappi.pro/maxapi/sync/messages/get?profile_id=" + url.QueryEscape(profileID) +
		"&chat_id=" + url.QueryEscape(chatID) +
		"&limit=" + strconv.Itoa(limit) + "&order=desc&mark_all=false"
	body, err := a.wappiJSON(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("Wappi messages: bad json")
	}
	return parsed.Messages, nil
}

func (a *App) wappiPollMsgToEv(profileID string, d wappiDialog, m map[string]any) (inboxEv, bool) {
	flat := flattenInbox(m)
	flat["profile_id"] = profileID
	flat["channel"] = "max"
	if firstScalar(flat["chatId"]) == "" && firstScalar(flat["chat_id"]) == "" {
		flat["chatId"] = d.ID
	}
	if firstScalar(flat["phone"]) == "" && d.Phone != "" {
		flat["phone"] = d.Phone
	}
	if firstScalar(flat["contact_name"]) == "" && d.Name != "" {
		flat["contact_name"] = d.Name
	}
	// текстовое тело: body иногда не строка
	if firstScalar(flat["body"]) == "" {
		if cap := firstScalar(flat["caption"]); cap != "" {
			flat["body"] = cap
		} else if typ := strings.ToLower(firstScalar(flat["type"])); typ != "" && typ != "text" {
			flat["body"] = "[" + typ + "]"
		}
	}
	ev, ok := inboxEvFrom("wappi", "max", flat)
	if !ok {
		return inboxEv{}, false
	}
	ev.Channel = "max"
	if ev.Title == "" || looksLikeClientPhone(ev.Title) {
		if d.Name != "" && !looksLikeClientPhone(d.Name) {
			ev.Title = d.Name
		} else {
			ev.Title = "Клиент"
		}
	}
	return ev, true
}
