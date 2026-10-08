package server

import "testing"

func TestCheckOutboundPolicyBlocksPassportRequestWithoutApproval(t *testing.T) {
	for _, s := range []string{
		"Пришлите серию и номер паспорта через защищённую форму",
		"Нужны паспортные данные для оформления",
		"Фото паспорта можно отправить в защищённом канале",
	} {
		hit, ok := checkOutboundPolicy(s)
		if !ok || hit.Rule != rulePassportRequest {
			t.Fatalf("%q: got %+v ok=%v", s, hit, ok)
		}
	}
}

func TestCheckOutboundPolicyAllowsOrdinaryChat(t *testing.T) {
	for _, s := range []string{
		"",
		"Добрый день, когда вам удобно созвониться?",
		"Напишите в MAX, я на связи",
		"паспорт готов?",
		"Отправил билет в карточку",
		"мы используем телеграм компании для рассылок",
		"Не отправляйте паспорт в этот чат, используйте защищённую форму",
		"Не присылайте паспорт текстом, отправьте фото или PDF через защищённый канал",
		"Не переходите в личный Telegram, продолжайте диалог здесь",
		"Не отправляйте паспорт в этот чат, используйте защищённую форму",
	} {
		if hit, ok := checkOutboundPolicy(s); ok {
			t.Fatalf("%q must pass, got %+v", s, hit)
		}
	}
}

func TestCheckOutboundPolicyMessengerAndShare(t *testing.T) {
	hit, ok := checkOutboundPolicy("Перейди в телеграм, там продолжим")
	if !ok || hit.Rule != rulePersonalMessenger {
		t.Fatalf("messenger: %+v %v", hit, ok)
	}
	hit, ok = checkOutboundPolicy("Паспорт 4510 123456 выдан ОВД")
	if !ok || hit.Rule != rulePassportShare {
		t.Fatalf("share: %+v %v", hit, ok)
	}
}

func TestCheckOutboundPolicyCandidatePhoneRequest(t *testing.T) {
	for _, s := range []string{
		"Пришлите ваш номер телефона",
		"Скиньте мобильный номер для связи",
		"Укажите ваш телефон в сообщении",
	} {
		hit, ok := checkOutboundPolicy(s)
		if !ok || hit.Rule != ruleCandidatePhoneReq {
			t.Fatalf("%q: got %+v ok=%v", s, hit, ok)
		}
	}
}

func TestCheckOutboundPolicyPersonalRedirect(t *testing.T) {
	for _, s := range []string{
		"Позвоните мне на номер +7 999 123-45-67",
		"Перейдите по ссылке https://example.com и напишите мне",
		"Мой номер 89991234567, свяжитесь со мной напрямую",
	} {
		hit, ok := checkOutboundPolicy(s)
		if !ok || hit.Rule != rulePersonalRedirect {
			t.Fatalf("%q: got %+v ok=%v", s, hit, ok)
		}
	}
}

func TestNormalizePolicyText(t *testing.T) {
	got := normalizePolicyText("  Пришлите\u200b серию, и номер! ")
	if got != "пришлите серию и номер" {
		t.Fatalf("norm %q", got)
	}
}
