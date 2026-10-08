package server

import (
	"strings"
	"testing"
)

func TestAccountLinePhoneStaysVisible(t *testing.T) {
	payload := map[string]any{
		"phone":          "79014171910",
		"from_phone":     "79014172349",
		"operator_label": "79014172349",
	}
	out, ok := redactPhonesValue(payload).(map[string]any)
	if !ok {
		t.Fatal("type")
	}
	if out["phone"] == "79014171910" {
		t.Fatal("client phone must stay masked")
	}
	if out["from_phone"] != "79014172349" || out["operator_label"] != "79014172349" {
		t.Fatalf("account number was masked: %#v", out)
	}
}

func TestMaskClientPhone(t *testing.T) {
	got := maskClientPhone("+7 (999) 123-45-67")
	if got == "" || got == "+7 (999) 123-45-67" {
		t.Fatalf("expected masked phone, got %q", got)
	}
	if !looksLikeClientPhone("79991234567") {
		t.Fatal("expected looksLikeClientPhone")
	}
}

func TestRedactAMIEventForWS(t *testing.T) {
	ev := map[string]string{
		"From":     "79991234567",
		"To":       "74951234567",
		"Uniqueid": "abc",
		"UserId":   "u1",
	}
	out := redactAMIEventForWS(ev)
	if out["From"] == "79991234567" || out["To"] == "74951234567" {
		t.Fatalf("phones not redacted: %#v", out)
	}
	if out["Uniqueid"] != "abc" || out["UserId"] != "u1" {
		t.Fatalf("non-phone fields changed: %#v", out)
	}
	// original must stay intact for DB match
	if ev["From"] != "79991234567" {
		t.Fatal("original event mutated")
	}
}

func TestRedactPhonesSliceMaps(t *testing.T) {
	payload := map[string]any{
		"channels": []map[string]string{
			{"From": "79991234567", "Uniqueid": "1"},
		},
		"contacts": []map[string]any{
			{"title": "Иван", "phone": "79991234567"},
		},
	}
	out, ok := redactPhonesValue(payload).(map[string]any)
	if !ok {
		t.Fatal("type")
	}
	ch := out["channels"].([]map[string]string)
	if ch[0]["From"] == "79991234567" {
		t.Fatalf("channel From not redacted: %#v", ch[0])
	}
	ct := out["contacts"].([]map[string]any)
	if ct[0]["phone"] == "79991234567" {
		t.Fatalf("contact phone not redacted: %#v", ct[0])
	}
}

func TestDemoSeedDisabledByDefault(t *testing.T) {
	t.Setenv("DEMO_SEED", "")
	if demoSeedEnabled() {
		t.Fatal("demo seed must stay off unless DEMO_SEED=1")
	}
	t.Setenv("DEMO_SEED", "1")
	if !demoSeedEnabled() {
		t.Fatal("DEMO_SEED=1 should enable seed")
	}
	t.Setenv("DEMO_SEED", "no")
	if demoSeedEnabled() {
		t.Fatal("unexpected enable")
	}
}

func TestSeeInboxFilesAllowed(t *testing.T) {
	cases := []struct {
		role string
		flag bool
		want bool
	}{
		{"admin", false, true},
		{"logist", false, true},
		{"night_logist", false, true},
		{"manager", true, true},
		{"sales", true, true},
		{"rop", true, true},
		{"manager", false, false},
		{"rop", false, false},
		{"support", true, false},
		{"", true, false},
	}
	for _, tc := range cases {
		if got := seeInboxFilesAllowed(tc.role, tc.flag); got != tc.want {
			t.Fatalf("seeInboxFilesAllowed(%q, %v)=%v want %v", tc.role, tc.flag, got, tc.want)
		}
	}
	// Билеты логиста не открываются этим флагом.
	if canSeeLogistTicketFiles("manager") || canSeeLogistTicketFiles("rop") {
		t.Fatal("logist ticket files must stay closed for manager/rop")
	}
}

func TestCanSeeClientPhonesRole(t *testing.T) {
	if !canSeeClientPhonesRole("admin") {
		t.Fatal("admin should see phones")
	}
	if canSeeClientPhonesRole("manager") || canSeeClientPhonesRole("rop") {
		t.Fatal("role-only check: non-admin must not see full phones")
	}
}

func TestSeeClientPhonesAllowed(t *testing.T) {
	cases := []struct {
		role string
		flag bool
		want bool
	}{
		{"admin", false, true},
		{"admin", true, true},
		{"manager", true, true},
		{"sales", true, true},
		{"rop", true, true},
		{"manager", false, false},
		{"rop", false, false},
		{"logist", true, false},
		{"logist", false, false},
		{"night_logist", false, true},
		{"night_logist", true, true},
		{"support", true, false},
		{"marketing", true, false},
		{"", true, false},
	}
	for _, tc := range cases {
		got := seeClientPhonesAllowed(tc.role, tc.flag)
		if got != tc.want {
			t.Fatalf("seeClientPhonesAllowed(%q, %v)=%v want %v", tc.role, tc.flag, got, tc.want)
		}
	}
}

func TestRedactDoesNotCorruptDealUUID(t *testing.T) {
	// Хвост UUID — 11 цифр после 'f' (как у реального бага на kanban stage).
	id := "c97d6150-37d1-43c6-ad6e-f71234566318"
	if !looksLikeUUID(id) {
		t.Fatal("expected UUID")
	}
	row := map[string]any{
		"id":            id,
		"deal_id":       id,
		"lead_id":       id,
		"stage_id":      "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		"phone":         "79280203801",
		"contact_phone": "79280203801",
		"title":         "Заявка 79280203801",
	}
	out, ok := redactPhonesValue(row).(map[string]any)
	if !ok {
		t.Fatal("type")
	}
	if out["id"] != id || out["deal_id"] != id || out["lead_id"] != id {
		t.Fatalf("UUID corrupted: %#v", out)
	}
	if stringsContainBullet(fmtString(out["id"])) {
		t.Fatalf("id has bullet: %v", out["id"])
	}
	if out["phone"] == "79280203801" || out["contact_phone"] == "79280203801" {
		t.Fatalf("phones not masked: %#v", out)
	}
	title := fmtString(out["title"])
	if strings.Contains(title, "79280203801") {
		t.Fatalf("title phone not masked: %q", title)
	}
}

func TestRedactKeepsCommentBodyWithDigits(t *testing.T) {
	body := "Перезвонить 79001234567 завтра"
	payloadJSON := `{"body":"` + body + `"}`
	row := map[string]any{
		"action":    "comment",
		"payload":   payloadJSON,
		"card_note": "Заметка 79007654321",
		"body":      body,
	}
	out, ok := redactPhonesValue(row).(map[string]any)
	if !ok {
		t.Fatal("type")
	}
	if fmtString(out["body"]) != body {
		t.Fatalf("body redacted: %q", out["body"])
	}
	if fmtString(out["card_note"]) != "Заметка 79007654321" {
		t.Fatalf("card_note redacted: %q", out["card_note"])
	}
	gotPayload := fmtString(out["payload"])
	if !strings.Contains(gotPayload, "79001234567") {
		t.Fatalf("payload body digits lost: %q", gotPayload)
	}
	if gotPayload == "7••••••4567" || !strings.HasPrefix(strings.TrimSpace(gotPayload), "{") {
		t.Fatalf("payload destroyed as phone: %q", gotPayload)
	}
	// phone field still masked
	row2 := map[string]any{"phone": "79001234567", "peer": "79001234567"}
	out2 := redactPhonesValue(row2).(map[string]any)
	if out2["phone"] == "79001234567" || out2["peer"] == "79001234567" {
		t.Fatalf("phone fields not masked: %#v", out2)
	}
}

func TestLooksLikeStandalonePhone(t *testing.T) {
	if !looksLikeStandalonePhone("+7 (999) 123-45-67") {
		t.Fatal("expected standalone phone")
	}
	if looksLikeStandalonePhone(`{"body":"Клиент 79001234567"}`) {
		t.Fatal("JSON must not look like standalone phone")
	}
	if looksLikeStandalonePhone("Клиент 79001234567") {
		t.Fatal("text with phone must not be standalone")
	}
}

func fmtString(v any) string {
	s, _ := v.(string)
	return s
}

func stringsContainBullet(s string) bool {
	return strings.Contains(s, "•")
}
