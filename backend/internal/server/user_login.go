package server

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

var cyrillicLoginMap = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "h", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch",
	'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

func translitLogin(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if v, ok := cyrillicLoginMap[r]; ok {
			b.WriteString(v)
			continue
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte('.')
		}
	}
	out := strings.Trim(b.String(), "._-")
	for strings.Contains(out, "..") {
		out = strings.ReplaceAll(out, "..", ".")
	}
	return out
}

func sanitizeLogin(raw string) string {
	s := translitLogin(raw)
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "._-")
	for strings.Contains(out, "..") {
		out = strings.ReplaceAll(out, "..", ".")
	}
	if len(out) > 48 {
		out = out[:48]
		out = strings.Trim(out, "._-")
	}
	return out
}

func suggestLoginFromName(fullName string) string {
	parts := strings.Fields(strings.TrimSpace(fullName))
	if len(parts) == 0 {
		return "user"
	}
	base := sanitizeLogin(parts[0])
	if base == "" {
		base = "user"
	}
	if len(parts) > 1 {
		first := sanitizeLogin(parts[1])
		if first != "" {
			base = base + "." + string([]rune(first)[0])
		}
	}
	return base
}

func (a *App) loginTaken(ctx context.Context, login, excludeID string) bool {
	login = strings.ToLower(strings.TrimSpace(login))
	if login == "" {
		return true
	}
	var n int
	q := `SELECT count(*) FROM users WHERE lower(login)=lower($1)`
	args := []any{login}
	if excludeID != "" {
		q += ` AND id<>$2::uuid`
		args = append(args, excludeID)
	}
	_ = a.db.QueryRow(ctx, q, args...).Scan(&n)
	return n > 0
}

func (a *App) ensureUniqueLogin(ctx context.Context, base, excludeID string) string {
	base = sanitizeLogin(base)
	if base == "" {
		base = "user"
	}
	cand := base
	for i := 2; i < 1000; i++ {
		if !a.loginTaken(ctx, cand, excludeID) {
			return cand
		}
		cand = fmt.Sprintf("%s%d", base, i)
	}
	return fmt.Sprintf("%s%d", base, len(base)+1)
}

func (a *App) resolveStaffLogin(ctx context.Context, requested, fullName, email string) string {
	requested = sanitizeLogin(requested)
	if requested != "" {
		return a.ensureUniqueLogin(ctx, requested, "")
	}
	base := suggestLoginFromName(fullName)
	if base == "" || base == "user" {
		if local := sanitizeLogin(strings.Split(email, "@")[0]); local != "" {
			base = local
		}
	}
	return a.ensureUniqueLogin(ctx, base, "")
}
