package server

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	passportSeriesNumRe = regexp.MustCompile(`(?i)(\d{2}\s*\d{2}|\d{4})\s*[№#]?\s*\d{6}`)
	passportDeptCodeRe  = regexp.MustCompile(`\d{3}-\d{3}`)
)

// looksLikePassportText — текст с паспортными данными (не просто слово «паспорт»).
func looksLikePassportText(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" || len([]rune(s)) < 8 {
		return false
	}
	lower := strings.ToLower(s)
	hasPassportWord := strings.Contains(lower, "паспорт") ||
		strings.Contains(lower, "серия") ||
		strings.Contains(lower, "выдан") ||
		strings.Contains(lower, "код подраздел") ||
		strings.Contains(lower, "кем выдан") ||
		strings.Contains(lower, "дата рожден") ||
		strings.Contains(lower, "место рожден")

	hasSeriesNum := passportSeriesNumRe.MatchString(s)
	hasDept := passportDeptCodeRe.MatchString(s) && (strings.Contains(lower, "код") || strings.Contains(lower, "подраздел") || strings.Contains(lower, "выдан"))

	digitCount := 0
	for _, r := range s {
		if unicode.IsDigit(r) {
			digitCount++
		}
	}

	// Явный формат серии+номера + ключевое слово или код подразделения.
	if hasSeriesNum && (hasPassportWord || hasDept || digitCount >= 10) {
		return true
	}
	// Много цифр + явные паспортные слова (серия/выдан/паспорт).
	if hasPassportWord && digitCount >= 10 && (strings.Contains(lower, "серия") || strings.Contains(lower, "выдан") || strings.Contains(lower, "паспорт")) {
		return true
	}
	if hasDept && hasPassportWord {
		return true
	}
	return false
}
