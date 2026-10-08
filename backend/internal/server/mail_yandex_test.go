package server

import (
	"strings"
	"testing"
)

func TestParseVKAdsMail(t *testing.T) {
	subject := "Новая заявка от формы: Лиды 22.08 серия 36.3"
	body := `Новая заявка по форме: Лиды 22.08 серия 36.3
Дата отправки: 2026-09-16 21:29:15 (МСК)
Имя: Иван Петров
Телефон: +7 (900) 111-22-33
Email: ivan@example.com
`
	got := parseVKAdsMail(subject, body)
	phone := digitsOnly(got["phone"].(string))
	if phone != "79001112233" {
		t.Fatalf("phone %q", got["phone"])
	}
	if got["name"] != "Иван Петров" {
		t.Fatalf("name %v", got["name"])
	}
	if got["email"] != "ivan@example.com" {
		t.Fatalf("email %v", got["email"])
	}
	form, _ := got["form"].(string)
	if !strings.Contains(form, "36.3") {
		t.Fatalf("form %q", form)
	}
}

func TestIsVKAdsMail(t *testing.T) {
	if !isVKAdsMail("ads.notifications@vk.company", "Новая заявка от формы: X") {
		t.Fatal("expected match")
	}
	if isVKAdsMail("spam@mail.ru", "Привет") {
		t.Fatal("expected skip")
	}
}
