package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// aiControlProxy exposes only read-only AI Control endpoints to CRM admins.
// The AI token never leaves the backend and is never sent to the browser.
func (a *App) aiControlProxy(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "только для администратора"})
		return
	}
	if strings.TrimSpace(a.cfg.AIControlURL) == "" || strings.TrimSpace(a.cfg.AIControlToken) == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "AI Control не настроен"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/ai-control")
	if !allowedAIControlPath(path) || (r.Method != http.MethodGet && !(r.Method == http.MethodPost && isFindingReviewPath(path))) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "AI Control endpoint not found"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var body io.Reader
	if r.Method == http.MethodPost {
		body = io.LimitReader(r.Body, 1<<20)
	}
	target := a.cfg.AIControlURL + path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target, body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "AI Control недоступен"})
		return
	}
	req.Header.Set("X-AI-Token", a.cfg.AIControlToken)
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := (&http.Client{Timeout: 6 * time.Second}).Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "AI Control недоступен"})
		return
	}
	defer res.Body.Close()
	w.Header().Set("Content-Type", res.Header.Get("Content-Type"))
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(res.Body, 2<<20))
}

func isFindingReviewPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 4 && parts[0] == "v1" && parts[1] == "findings" && parts[2] != "" && parts[3] == "review"
}

func allowedAIControlPath(path string) bool {
	if path == "/healthz" || path == "/readyz" || path == "/v1/jobs" || path == "/v1/events" || path == "/v1/findings" {
		return true
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 3 && parts[0] == "v1" && (parts[1] == "jobs" || parts[1] == "analyses") && parts[2] != "" {
		return true
	}
	return len(parts) == 4 && parts[0] == "v1" && parts[1] == "findings" && parts[2] != "" && parts[3] == "reviews"
}
