package server

import (
	"net/http"
	"strconv"
	"strings"
)

type autodialScheduleRow struct {
	ID              string `json:"id"`
	SortOrder       int    `json:"sort_order"`
	DayFrom         int    `json:"day_from"`
	DayTo           *int   `json:"day_to"`
	CallsMin        int    `json:"calls_min"`
	CallsMax        int    `json:"calls_max"`
	IntervalMinutes int    `json:"interval_minutes"`
	Note            string `json:"note"`
	PeriodLabel     string `json:"period_label"`
}

func autodialPeriodLabel(dayFrom int, dayTo *int) string {
	if dayTo == nil {
		return "30+ дней"
	}
	if dayFrom == *dayTo {
		switch dayFrom {
		case 1:
			return "1 день"
		case 2:
			return "2 день"
		case 3:
			return "3 день"
		default:
			return strconv.Itoa(dayFrom) + " день"
		}
	}
	if dayFrom == 4 && *dayTo == 7 {
		return "4–7 день"
	}
	if dayFrom <= 8 && *dayTo == 30 {
		return "7–30 день"
	}
	return strconv.Itoa(dayFrom) + "–" + strconv.Itoa(*dayTo) + " день"
}

func (a *App) ensureAutodialScheduleDefaults(r *http.Request) {
	var n int
	_ = a.db.QueryRow(r.Context(), `SELECT count(*) FROM autodial_schedule`).Scan(&n)
	if n > 0 {
		return
	}
	_, _ = a.db.Exec(r.Context(), `
		INSERT INTO autodial_schedule (sort_order, day_from, day_to, calls_min, calls_max, interval_minutes, note)
		VALUES
			(1, 1, 1, 5, 15, 60, 'Максимальная активность в первый день'),
			(2, 2, 2, 1, 7, 120, 'Снижаем частоту, но сохраняем активность'),
			(3, 3, 3, 1, 5, 180, 'Продолжаем работу с теплыми лидами'),
			(4, 4, 7, 3, 4, 240, 'Умеренная активность'),
			(5, 8, 30, 1, 1, 1440, 'Разное время суток'),
			(6, 31, NULL, 0, 0, 1440, 'Можно настроить индивидуально')`)
}

func (a *App) autodialScheduleGet(w http.ResponseWriter, r *http.Request) {
	if !autodialFeatureEnabled() {
		writeAutodialPaused(w)
		return
	}
	if !isAdmin(r) && !isROP(r) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	a.ensureAutodialScheduleDefaults(r)
	rows, err := a.db.Query(r.Context(), `
		SELECT id::text, sort_order, day_from, day_to, calls_min, calls_max, interval_minutes, note
		FROM autodial_schedule
		ORDER BY sort_order, day_from`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось загрузить расписание"})
		return
	}
	defer rows.Close()
	items := make([]autodialScheduleRow, 0, 8)
	for rows.Next() {
		var it autodialScheduleRow
		var dayTo *int
		if rows.Scan(&it.ID, &it.SortOrder, &it.DayFrom, &dayTo, &it.CallsMin, &it.CallsMax, &it.IntervalMinutes, &it.Note) != nil {
			continue
		}
		it.DayTo = dayTo
		it.PeriodLabel = autodialPeriodLabel(it.DayFrom, dayTo)
		items = append(items, it)
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (a *App) autodialSchedulePut(w http.ResponseWriter, r *http.Request) {
	if !autodialFeatureEnabled() {
		writeAutodialPaused(w)
		return
	}
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только администратор"})
		return
	}
	var in struct {
		Items []struct {
			ID              string `json:"id"`
			CallsMin        int    `json:"calls_min"`
			CallsMax        int    `json:"calls_max"`
			IntervalMinutes int    `json:"interval_minutes"`
			Note            string `json:"note"`
		} `json:"items"`
	}
	if err := readJSON(r, &in); err != nil || len(in.Items) == 0 {
		writeJSON(w, 400, map[string]string{"error": "нужен список периодов"})
		return
	}
	ctx := r.Context()
	for _, it := range in.Items {
		id := strings.TrimSpace(it.ID)
		if id == "" {
			continue
		}
		callsMin := it.CallsMin
		callsMax := it.CallsMax
		if callsMin < 0 {
			callsMin = 0
		}
		if callsMax < 0 {
			callsMax = 0
		}
		if callsMax < callsMin {
			callsMax = callsMin
		}
		if callsMax > 50 {
			callsMax = 50
		}
		interval := it.IntervalMinutes
		if interval < 0 {
			interval = 0
		}
		if interval > 7*24*60 {
			interval = 7 * 24 * 60
		}
		note := strings.TrimSpace(it.Note)
		if len(note) > 240 {
			note = note[:240]
		}
		_, _ = a.db.Exec(ctx, `
			UPDATE autodial_schedule
			SET calls_min=$2, calls_max=$3, interval_minutes=$4, note=$5, updated_at=now()
			WHERE id=$1::uuid`, id, callsMin, callsMax, interval, note)
	}
	a.autodialScheduleGet(w, r)
}
