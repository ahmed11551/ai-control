package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/forgecrm/server/internal/bitrix"
)

type importHub struct {
	mu     sync.Mutex
	busy   bool
	cancel context.CancelFunc
	job    bitrix.Progress
}

func (a *App) importStatus(w http.ResponseWriter, r *http.Request) {
	local := bitrix.LocalImported(r.Context(), a.db)
	a.imp.mu.Lock()
	job := a.imp.job
	busy := a.imp.busy
	a.imp.mu.Unlock()
	if job.Counts == nil {
		job.Counts = map[string]bitrix.Count{}
	}
	for k, v := range local {
		cur := job.Counts[k]
		if cur.Imported == 0 {
			cur.Imported = v.Imported
		}
		job.Counts[k] = cur
	}
	if job.Status == "" {
		job.Status = "idle"
	}
	if busy && job.Status != "running" {
		job.Status = "running"
	}
	writeJSON(w, 200, job)
}

func (a *App) importInventory(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только администратор"})
		return
	}
	hook, err := a.readWebhook(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	prog, err := bitrix.Inventory(r.Context(), hook)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	local := bitrix.LocalImported(r.Context(), a.db)
	for k, v := range local {
		c := prog.Counts[k]
		c.Imported = v.Imported
		prog.Counts[k] = c
	}
	writeJSON(w, 200, prog)
}

func (a *App) importRun(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только администратор"})
		return
	}
	var in struct {
		Webhook  string   `json:"webhook"`
		Entities []string `json:"entities"`
	}
	_ = readJSON(r, &in)
	hook := strings.TrimSpace(in.Webhook)
	if hook == "" {
		hook = a.cfg.BitrixWebhook
	}
	if hook == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите входящий вебхук Bitrix24"})
		return
	}

	a.imp.mu.Lock()
	if a.imp.busy {
		a.imp.mu.Unlock()
		writeJSON(w, 409, map[string]string{"error": "импорт уже идёт"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.imp.busy = true
	a.imp.cancel = cancel
	a.imp.job = bitrix.Progress{Status: "running", Phase: "start", Counts: map[string]bitrix.Count{}}
	a.imp.mu.Unlock()

	go func() {
		defer func() {
			a.imp.mu.Lock()
			a.imp.busy = false
			a.imp.cancel = nil
			a.imp.mu.Unlock()
		}()
		im, err := bitrix.NewImporter(a.db, hook, in.Entities, func(p bitrix.Progress) {
			a.imp.mu.Lock()
			a.imp.job = p
			a.imp.mu.Unlock()
			if p.Status == "done" || p.Status == "error" {
				a.persistJob(p)
			}
		})
		if err != nil {
			a.imp.mu.Lock()
			a.imp.job = bitrix.Progress{Status: "error", Error: err.Error(), Counts: map[string]bitrix.Count{}}
			a.imp.mu.Unlock()
			return
		}
		if err := im.Run(ctx); err != nil && a.imp.job.Status != "error" {
			p := im.Progress()
			p.Status = "error"
			p.Error = err.Error()
			a.imp.mu.Lock()
			a.imp.job = p
			a.imp.mu.Unlock()
			a.persistJob(p)
		}
	}()

	writeJSON(w, 202, map[string]any{"ok": true, "status": "running"})
}

func (a *App) persistJob(p bitrix.Progress) {
	b, _ := json.Marshal(p.Counts)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = a.db.Exec(ctx, `
		INSERT INTO bitrix_import_jobs (portal, status, phase, counts, error)
		VALUES ($1,$2,$3,$4::jsonb,$5)`,
		p.Portal, p.Status, p.Phase, string(b), nullIfEmpty(p.Error))
}

func (a *App) readWebhook(r *http.Request) (string, error) {
	var in struct {
		Webhook string `json:"webhook"`
	}
	_ = readJSON(r, &in)
	hook := strings.TrimSpace(in.Webhook)
	if hook == "" {
		hook = a.cfg.BitrixWebhook
	}
	if hook == "" {
		return "", errWebhook
	}
	return hook, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

type webhookErr string

func (e webhookErr) Error() string { return string(e) }

const errWebhook webhookErr = "укажите входящий вебхук Bitrix24"
