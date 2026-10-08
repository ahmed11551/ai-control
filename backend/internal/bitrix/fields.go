package bitrix

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func bitrixUserPhones(row map[string]any) (inner, work string) {
	seen := map[string]bool{}
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || seen[raw] {
			return
		}
		seen[raw] = true
		d := digitsOnly(raw)
		if inner == "" && len(d) >= 2 && len(d) <= 4 {
			inner = d
			return
		}
		if work == "" && len(d) >= 10 {
			work = raw
		}
	}
	for _, key := range []string{
		"UF_PHONE_INNER", "UF_INNER_PHONE", "UF_USR_PHONE_INNER", "INNER_PHONE",
		"PERSONAL_PAGER", "WORK_PHONE", "PERSONAL_PHONE", "PERSONAL_MOBILE",
	} {
		add(rowString(row, key))
	}
	for k, v := range row {
		ku := strings.ToUpper(k)
		if !strings.HasPrefix(ku, "UF_") {
			continue
		}
		if strings.Contains(ku, "PHONE") || strings.Contains(ku, "INNER") || strings.Contains(ku, "TEL") || strings.Contains(ku, "SIP") {
			add(firstScalar(v))
		}
	}
	return inner, work
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "Y"
		}
		return "N"
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func rowString(row map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := firstScalar(row[k]); s != "" {
			return s
		}
	}
	return ""
}

func firstScalar(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case []any:
		for _, item := range t {
			if s := firstScalar(item); s != "" {
				return s
			}
		}
		return ""
	case map[string]any:
		for _, k := range []string{"VALUE", "value", "EMAIL", "PHONE", "NAME"} {
			if s := asString(t[k]); s != "" {
				return s
			}
		}
		return ""
	default:
		return asString(t)
	}
}

func fullName(row map[string]any) string {
	parts := []string{
		rowString(row, "LAST_NAME", "lastName"),
		rowString(row, "NAME", "name"),
		rowString(row, "SECOND_NAME", "secondName"),
	}
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) > 0 {
		return strings.Join(out, " ")
	}
	if t := rowString(row, "TITLE", "title"); t != "" {
		return t
	}
	return ""
}

func leadStatus(code string) string {
	c := strings.ToUpper(strings.TrimSpace(code))
	switch c {
	case "NEW", "NEW_LEAD", "UNPROCESSED":
		return "new"
	case "IN_PROCESS", "IN_WORK", "PROCESSING", "IN_PROGRESS":
		return "in_progress"
	case "CONVERTED", "QUALIFIED", "PROCESSED":
		return "qualified"
	case "JUNK", "LOST", "REJECTED":
		return "rejected"
	default:
		if c == "" {
			return "new"
		}
		return strings.ToLower(c)
	}
}

func sourceID(code string) string {
	c := strings.ToUpper(strings.TrimSpace(code))
	switch c {
	case "CALL", "CALLBACK", "RC_GENERATOR":
		return "phone"
	case "WEB", "WEBFORM", "STORE", "LANDING":
		return "site"
	case "VK", "VKADS", "VKONTAKTE":
		return "vk_ads"
	case "PARTNER", "RECOMMENDATION":
		return "referral"
	case "SELF", "MANUAL", "":
		return "manual"
	default:
		if c == "" {
			return "manual"
		}
		return strings.ToLower(c)
	}
}

func taskStatus(v any) string {
	s := asString(v)
	switch s {
	case "5", "4", "completed", "COMPLETED":
		return "completed"
	default:
		return "in_progress"
	}
}

func bitrixIDList(v any) []string {
	var out []string
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		s := strings.TrimSpace(t)
		if s == "" || s == "[]" {
			return nil
		}
		if strings.Contains(s, ",") {
			for _, p := range strings.Split(s, ",") {
				if id := strings.TrimSpace(p); id != "" {
					out = append(out, id)
				}
			}
			return out
		}
		return []string{s}
	case []any:
		for _, x := range t {
			if s := asString(x); s != "" {
				out = append(out, s)
			}
		}
	case map[string]any:
		for _, x := range t {
			if s := asString(x); s != "" {
				out = append(out, s)
			}
		}
	default:
		if s := asString(t); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func taskTags(v any) []string {
	var out []string
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			if m, ok := x.(map[string]any); ok {
				if s := nz(rowString(m, "TITLE", "title", "NAME", "name"), ""); s != "" {
					out = append(out, s)
					continue
				}
			}
			if s := asString(x); s != "" {
				out = append(out, s)
			}
		}
	case string:
		for _, p := range strings.Split(t, ",") {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func crmBindings(v any) (entityType string, entityBitrixID string) {
	var items []string
	switch t := v.(type) {
	case string:
		if t != "" {
			items = append(items, t)
		}
	case []any:
		for _, x := range t {
			if s := asString(x); s != "" {
				items = append(items, s)
			}
		}
	}
	for _, item := range items {
		item = strings.TrimSpace(item)
		switch {
		case strings.HasPrefix(item, "D_"):
			return "deal", strings.TrimPrefix(item, "D_")
		case strings.HasPrefix(item, "L_"):
			return "lead", strings.TrimPrefix(item, "L_")
		case strings.HasPrefix(item, "CO_"):
			return "company", strings.TrimPrefix(item, "CO_")
		case strings.HasPrefix(item, "C_"):
			return "contact", strings.TrimPrefix(item, "C_")
		}
	}
	return "", ""
}

func firstDept(v any) string {
	switch t := v.(type) {
	case []any:
		if len(t) > 0 {
			return asString(t[0])
		}
	default:
		return asString(v)
	}
	return ""
}

func isActive(v any) bool {
	s := strings.ToUpper(asString(v))
	return s != "N" && s != "0" && s != "FALSE"
}

func semanticsWonLost(sem string) (won, lost bool) {
	switch strings.ToUpper(sem) {
	case "S":
		return true, false
	case "F":
		return false, true
	default:
		return false, false
	}
}
