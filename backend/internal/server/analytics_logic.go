package server

import (
	"fmt"
	"strings"
	"time"
)

const (
	analyticsPresetDay    = "day"
	analyticsPresetWeek   = "week"
	analyticsPresetMonth  = "month"
	analyticsPresetCustom = "custom"
	massViewThreshold     = 30
	policyRestrictAfter   = 3
)

// Стадии адаптации, на которых карточка уже у логиста (если сделка ещё не выиграна и не проиграна).
func logistHandoffStageKeys() []string {
	return []string{
		hirePipelineKey + ":bilet",
		hirePipelineKey + ":departure",
		hirePipelineKey + ":selection",
		hirePipelineKey + ":prescription",
	}
}

func sqlLeadBucketExpr() string {
	quoted := make([]string, 0, len(logistHandoffStageKeys()))
	for _, k := range logistHandoffStageKeys() {
		quoted = append(quoted, "'"+k+"'")
	}
	return `CASE
		WHEN s.is_won THEN 'success'
		WHEN s.is_lost THEN 'rejected'
		WHEN coalesce(d.logistics_needed,false) OR coalesce(s.bitrix_id,'') IN (` + strings.Join(quoted, ",") + `) THEN 'to_logist'
		ELSE 'in_work'
	END`
}

func classifyLeadBucket(won, lost, logistics bool, stageKey string) string {
	if won {
		return "success"
	}
	if lost {
		return "rejected"
	}
	if logistics {
		return "to_logist"
	}
	for _, k := range logistHandoffStageKeys() {
		if stageKey == k {
			return "to_logist"
		}
	}
	return "in_work"
}

func callIsConnected(disposition string, durationSec int) bool {
	if durationSec <= 0 {
		return false
	}
	return !callDispositionMissed(disposition)
}

func shareOf(count, total int) float64 {
	if total <= 0 || count <= 0 {
		return 0
	}
	return float64(count) / float64(total)
}

func shouldRestrictSession(openViolations int) bool {
	return openViolations >= policyRestrictAfter
}

func parseAnalyticsRange(preset, fromRaw, toRaw string, now time.Time, loc *time.Location) (time.Time, time.Time, error) {
	if loc == nil {
		loc = time.FixedZone("MSK", 3*3600)
	}
	now = now.In(loc)
	preset = strings.ToLower(strings.TrimSpace(preset))
	if preset == "" {
		preset = analyticsPresetMonth
	}
	switch preset {
	case analyticsPresetDay:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		return start, start.AddDate(0, 0, 1), nil
	case analyticsPresetWeek:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		offset := (int(start.Weekday()) + 6) % 7
		start = start.AddDate(0, 0, -offset)
		return start, start.AddDate(0, 0, 7), nil
	case analyticsPresetMonth:
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(0, 1, 0), nil
	case analyticsPresetCustom:
		from, err := parseAnalyticsDate(fromRaw, loc, false)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("укажите начало периода")
		}
		to, err := parseAnalyticsDate(toRaw, loc, true)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("укажите конец периода")
		}
		if !to.After(from) {
			return time.Time{}, time.Time{}, fmt.Errorf("конец периода раньше начала")
		}
		if to.Sub(from) > 366*24*time.Hour {
			return time.Time{}, time.Time{}, fmt.Errorf("период не длиннее года")
		}
		return from, to, nil
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("неизвестный период")
	}
}

func parseAnalyticsDate(raw string, loc *time.Location, endExclusive bool) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty")
	}
	if len(raw) >= 10 && raw[4] == '-' && raw[7] == '-' {
		t, err := time.ParseInLocation("2006-01-02", raw[:10], loc)
		if err != nil {
			return time.Time{}, err
		}
		if endExclusive {
			return t.AddDate(0, 0, 1), nil
		}
		return t, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, err
	}
	return t.In(loc), nil
}

func analyticsLocation() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.FixedZone("MSK", 3*3600)
	}
	return loc
}
