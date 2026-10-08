package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

func parseList(r *http.Request, defLimit, maxLimit int) (q string, limit, offset int) {
	q = strings.TrimSpace(r.URL.Query().Get("q"))
	rawLimit := strings.TrimSpace(r.URL.Query().Get("limit"))
	if rawLimit == "0" || strings.EqualFold(rawLimit, "all") {
		limit = maxLimit
	} else {
		limit = atoiDefault(rawLimit, defLimit)
		if limit < 1 {
			limit = defLimit
		}
		if limit > maxLimit {
			limit = maxLimit
		}
	}
	offset = atoiDefault(r.URL.Query().Get("offset"), 0)
	if offset < 0 {
		offset = 0
	}
	return q, limit, offset
}

func periodDays(r *http.Request) int {
	d := atoiDefault(r.URL.Query().Get("days"), 0)
	if d == 1 || d == 7 || d == 30 {
		return d
	}
	return 0
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func likePattern(q string) string {
	return "%" + q + "%"
}

func wantMine(r *http.Request) bool {
	v := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mine")))
	return v == "1" || v == "true" || v == "yes"
}

func parseSort(r *http.Request, allowed map[string]string, fallback string) string {
	col := allowed[strings.TrimSpace(r.URL.Query().Get("sort"))]
	if col == "" {
		col = fallback
	}
	dir := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("dir")))
	if dir != "ASC" {
		dir = "DESC"
	}
	return col + " " + dir
}

func (a *App) queryInt(ctx context.Context, sql string, args ...any) int {
	var n int
	if err := a.db.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		return 0
	}
	return n
}

func (a *App) writePage(w http.ResponseWriter, r *http.Request, items []map[string]any, total, limit, offset int) {
	if items == nil {
		items = []map[string]any{}
	}
	a.writePhones(w, r, 200, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}
