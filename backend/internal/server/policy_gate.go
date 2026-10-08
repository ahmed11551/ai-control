package server

import (
	"regexp"
	"strings"
	"unicode"
)

const (
	rulePassportShare     = "passport_share"
	rulePassportRequest   = "passport_request"
	ruleCandidatePhoneReq = "candidate_phone_request"
	rulePersonalMessenger = "personal_messenger"
	rulePersonalRedirect  = "personal_redirect"
	msgPassportShare      = "Отправлять паспортные данные в этом канале запрещено"
	msgPassportRequest    = "Текстовый запрос паспортных данных требует согласования администратора"
	msgCandidatePhoneReq  = "Запрашивать номер телефона кандидата в этом канале запрещено"
	msgPersonalMessenger  = "Переводить переписку в личный мессенджер запрещено"
	msgPersonalRedirect   = "Переводить кандидата на личный номер или внешнюю ссылку запрещено. Используйте рабочий диалог CRM."
	msgSessionRestricted  = "Сессия ограничена: доступ только на чтение. Напишите РОПу или администратору."
)

var externalContactLinkRe = regexp.MustCompile(`(?i)(https?://|www\.|t\.me/|telegram\.me/|wa\.me/|whatsapp\.com/|vk\.com/)`)

// PolicyHit — срабатывание детерминированного правила. AI сюда не входит.
type PolicyHit struct {
	Rule     string
	Severity string
	Message  string
}

func normalizePolicyText(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	prevSpace := false
	for _, r := range raw {
		if r == 'ё' {
			r = 'е'
		}
		if r == '\u200b' || r == '\ufeff' || unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
			continue
		}
		if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func containsAny(s string, parts ...string) bool {
	for _, p := range parts {
		if p != "" && strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func candidatePhoneRequested(norm string) bool {
	if norm == "" || !containsAny(norm, "номер телефона", "номер мобильного", "мобильный номер", "ваш номер", "ваш телефон") {
		return false
	}
	return containsAny(norm, "пришл", "скин", "отправ", "дайте", "укаж", "напиш", "остав")
}

func passportRequested(norm string) bool {
	if norm == "" {
		return false
	}
	if containsAny(norm, "не отправляй", "не отправляйте", "не присылай", "не присылайте", "не отправлять", "не присылать") {
		return false
	}
	if strings.Contains(norm, "серия и номер") || strings.Contains(norm, "серию и номер") {
		return true
	}
	if strings.Contains(norm, "паспортн") && strings.Contains(norm, "данн") {
		return true
	}
	if strings.Contains(norm, "код подразделения") || strings.Contains(norm, "код подразделен") {
		return true
	}
	ask := containsAny(norm, "пришл", "скин", "отправ", "фото", "копи", "нужен", "нужны", "нужна", "дайте", "укаж")
	return (strings.Contains(norm, "паспорт") && ask) || (strings.Contains(norm, "скан") && (ask || strings.Contains(norm, "паспорт")))
}

func personalMessengerRequested(norm string) bool {
	if norm == "" {
		return false
	}
	// Запрет переходить в личный канал — предупреждение, а не попытка увода.
	if containsAny(norm, "не переходи", "не переходите", "не пишите", "не пиши", "не отправляй", "не отправляйте") {
		return false
	}
	if strings.Contains(norm, "в личку") || strings.Contains(norm, "личный мессенджер") || strings.Contains(norm, "личном мессенджере") {
		return true
	}
	has := containsAny(norm, "телеграм", "telegram", "ватсап", "whatsapp", "вотсап", "вайбер", "viber")
	if strings.Contains(norm, " в тг") || strings.HasPrefix(norm, "тг ") || strings.Contains(norm, "мой тг") {
		has = true
	}
	if !has {
		return false
	}
	return containsAny(norm, "напиш", "перейд", "перепиш", "пиши", "личн", "мой ", "давай", "свяж", "продолж")
}

func personalRedirectRequested(raw, norm string) bool {
	if raw == "" || norm == "" {
		return false
	}
	contactCue := containsAny(norm,
		"мой номер", "мой телефон", "личный номер", "наберите", "позвон", "звони", "напиш",
		"пиши", "свяж", "перейд", "перепиш", "в личку", "по номеру", "на номер", "по ссылке",
		"откройте ссылку", "перейдите по ссылке",
	)
	if !contactCue {
		return false
	}
	return externalContactLinkRe.MatchString(raw) || phoneLikeRe.MatchString(raw)
}

// checkOutboundPolicy — жёсткие правила до любого AI.
// Пустой текст и обычная переписка (включая MAX) не блокируются.
func checkOutboundPolicy(raw string) (PolicyHit, bool) {
	norm := normalizePolicyText(raw)
	if personalRedirectRequested(raw, norm) {
		return PolicyHit{Rule: rulePersonalRedirect, Severity: "high", Message: msgPersonalRedirect}, true
	}
	if looksLikePassportText(raw) {
		return PolicyHit{Rule: rulePassportShare, Severity: "high", Message: msgPassportShare}, true
	}
	if candidatePhoneRequested(norm) {
		return PolicyHit{Rule: ruleCandidatePhoneReq, Severity: "high", Message: msgCandidatePhoneReq}, true
	}
	if passportRequested(norm) {
		return PolicyHit{Rule: rulePassportRequest, Severity: "high", Message: msgPassportRequest}, true
	}
	if personalMessengerRequested(norm) {
		return PolicyHit{Rule: rulePersonalMessenger, Severity: "medium", Message: msgPersonalMessenger}, true
	}
	return PolicyHit{}, false
}
