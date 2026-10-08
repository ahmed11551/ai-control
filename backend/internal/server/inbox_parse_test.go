package server

import "testing"

func TestInboxEventsWappiIncoming(t *testing.T) {
	raw := map[string]any{
		"messages": []any{
			map[string]any{
				"wh_type":      "incoming_message",
				"profile_id":   "prof-1",
				"id":           "m1",
				"body":         "привет",
				"chatId":       "79001112233@c.us",
				"from":         "79001112233@c.us",
				"contact_name": "Клиент",
			},
		},
	}
	ev := inboxEvents("wappi", raw)
	if len(ev) != 1 {
		t.Fatalf("got %d events", len(ev))
	}
	if ev[0].Channel != "whatsapp" {
		t.Fatalf("channel %q", ev[0].Channel)
	}
	if ev[0].ChatID != "79001112233@c.us" {
		t.Fatalf("chat %q", ev[0].ChatID)
	}
	if ev[0].Body != "привет" || ev[0].Profile != "prof-1" || ev[0].Direction != "inbound" {
		t.Fatalf("%+v", ev[0])
	}
	if ev[0].Phone != "79001112233" {
		t.Fatalf("phone %q", ev[0].Phone)
	}
}

func TestInboxEventsSkipDelivery(t *testing.T) {
	raw := map[string]any{
		"messages": []any{
			map[string]any{"wh_type": "delivery_status", "id": "x", "chatId": "1"},
		},
	}
	if ev := inboxEvents("wappi", raw); len(ev) != 0 {
		t.Fatalf("expected skip, got %+v", ev)
	}
}

func TestInboxEventsMaxBotMessage(t *testing.T) {
	raw := map[string]any{
		"update_type": "message_created",
		"timestamp":   float64(1739184000000),
		"message": map[string]any{
			"recipient": map[string]any{
				"chat_id":   float64(-100000000),
				"chat_type": "dialog",
				"user_id":   float64(12345),
			},
			"body": map[string]any{
				"mid":  "mid.abc",
				"text": "Привет",
			},
			"sender": map[string]any{
				"user_id":    float64(54321),
				"first_name": "Иван",
				"is_bot":     false,
				"name":       "Иван",
			},
		},
	}
	ev := inboxEvents("max", raw)
	if len(ev) != 1 {
		t.Fatalf("got %d", len(ev))
	}
	if ev[0].Channel != "max" || ev[0].ChatID != "-100000000" || ev[0].Body != "Привет" {
		t.Fatalf("%+v", ev[0])
	}
	if ev[0].ExtID != "mid.abc" || ev[0].Title != "Иван" {
		t.Fatalf("%+v", ev[0])
	}
}

func TestInboxEventsMaxBotSkipBotEcho(t *testing.T) {
	raw := map[string]any{
		"update_type": "message_created",
		"message": map[string]any{
			"recipient": map[string]any{"chat_id": float64(1)},
			"body":      map[string]any{"mid": "m", "text": "echo"},
			"sender":    map[string]any{"user_id": float64(9), "is_bot": true, "name": "Bot"},
		},
	}
	if ev := inboxEvents("max", raw); len(ev) != 0 {
		t.Fatalf("expected skip, got %+v", ev)
	}
}

func TestInboxEventsMaxBotStarted(t *testing.T) {
	raw := map[string]any{
		"update_type": "bot_started",
		"timestamp":   float64(1),
		"chat_id":     float64(777),
		"user": map[string]any{
			"user_id": float64(777),
			"name":    "Клиент",
		},
	}
	ev := inboxEvents("max", raw)
	if len(ev) != 1 || ev[0].ChatID != "777" || ev[0].Title != "Клиент" {
		t.Fatalf("%+v", ev)
	}
}

func TestInboxEventsWappiMax(t *testing.T) {
	raw := map[string]any{
		"messages": []any{
			map[string]any{
				"wh_type":      "incoming_message",
				"profile_id":   "prof-max",
				"id":           "m-max",
				"body":         "макс",
				"chatId":       "-12345",
				"from":         "-12345",
				"phone":        "79005551122",
				"contact_name": "Кандидат",
			},
		},
	}
	ev := inboxEvents("wappi", raw)
	if len(ev) != 1 {
		t.Fatalf("got %d", len(ev))
	}
	if ev[0].Channel != "max" || ev[0].Profile != "prof-max" || ev[0].Phone != "79005551122" {
		t.Fatalf("%+v", ev[0])
	}
}

func TestInboxEventsWappiMessagesObject(t *testing.T) {
	raw := map[string]any{
		"messages": map[string]any{
			"wh_type": "delivery_status",
			"id":      "x",
			"chatId":  "1",
		},
	}
	if ev := inboxEvents("wappi", raw); len(ev) != 0 {
		t.Fatalf("expected skip, got %+v", ev)
	}
}

func TestInboxEventsWappiMaxObjectMessage(t *testing.T) {
	raw := map[string]any{
		"messages": map[string]any{
			"wh_type":       "incoming_message",
			"profile_id":    "prof-max",
			"id":            "m-obj",
			"body":          "объект",
			"chatId":        "-999",
			"contact_phone": "79001234567",
			"contact_name":  "Тест",
		},
	}
	ev := inboxEvents("wappi", raw)
	if len(ev) != 1 {
		t.Fatalf("got %d", len(ev))
	}
	if ev[0].Channel != "max" || ev[0].ChatID != "-999" || ev[0].Phone != "79001234567" || ev[0].Body != "объект" {
		t.Fatalf("%+v", ev[0])
	}
}

func TestInboxEventsSkipMaxSystemNotice(t *testing.T) {
	raw := map[string]any{
		"messages": []any{
			map[string]any{
				"wh_type":    "incoming_message",
				"profile_id": "prof-max",
				"id":         "sys1",
				"body":       "<b>Новый вход в MAX</b>\n\nОбнаружили вход в профиль с вашим номером телефона",
				"chatId":     "-12345",
				"from":       "-12345",
				"phone":      "79003148350",
			},
		},
	}
	if ev := inboxEvents("wappi", raw); len(ev) != 0 {
		t.Fatalf("expected system notice skip, got %+v", ev)
	}
}

func TestIsMaxSystemNotice(t *testing.T) {
	if !isMaxSystemNotice(`<b>Новый вход в MAX</b>`) {
		t.Fatal("expected notice")
	}
	if !isMaxSystemNotice("Завершить все сессии, кроме текущей") {
		t.Fatal("expected sessions notice")
	}
	if isMaxSystemNotice("привет, хочу работать") {
		t.Fatal("client message must pass")
	}
}

func TestMaxInboundMayBindContact(t *testing.T) {
	if !maxInboundMayBindContact("79001112233", "8 (900) 111-22-33") {
		t.Fatal("same phone must bind")
	}
	if maxInboundMayBindContact("", "79001112233") {
		t.Fatal("empty sender must not bind")
	}
	if maxInboundMayBindContact("79001112233", "79009998877") {
		t.Fatal("foreign phone must not bind")
	}
}
