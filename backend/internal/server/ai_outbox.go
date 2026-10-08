package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type aiOutboxEvent struct {
	EventID           string          `json:"event_id"`
	EventType         string          `json:"event_type"`
	SchemaVersion     string          `json:"schema_version"`
	OccurredAt        string          `json:"occurred_at"`
	Producer          string          `json:"producer"`
	CorrelationID     string          `json:"correlation_id"`
	AggregateType     string          `json:"aggregate_type"`
	AggregateID       string          `json:"aggregate_id"`
	AggregateVersion  int             `json:"aggregate_version"`
	DepartmentIDEvent string          `json:"department_id_at_event,omitempty"`
	ActorUserID       string          `json:"actor_user_id,omitempty"`
	DataClass         string          `json:"data_class"`
	TraceID           string          `json:"trace_id"`
	Payload           json.RawMessage `json:"payload"`
}

// ensureAIOutboxSchema creates only additive tables for the isolated delivery spike.
func (a *App) ensureAIOutboxSchema(ctx context.Context) {
	_, _ = a.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS ai_outbox (
			id BIGSERIAL PRIMARY KEY,
			event_id TEXT NOT NULL UNIQUE,
			event_json JSONB NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sent','failed')),
			attempts INTEGER NOT NULL DEFAULT 0,
			next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			last_error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE INDEX IF NOT EXISTS ai_outbox_delivery_idx ON ai_outbox(status,next_attempt_at,id)`)
}

func (a *App) enqueueAITestEvent(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "только для администратора"})
		return
	}
	var event aiOutboxEvent
	if err := readJSON(r, &event); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json"})
		return
	}
	if event.EventID == "" {
		event.EventID = fmt.Sprintf("crm-test-%d", time.Now().UnixNano())
	}
	if err := a.enqueueAIEvent(r.Context(), event); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "outbox insert failed"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"event_id": event.EventID, "status": "pending"})
}

func (a *App) enqueueAIEvent(ctx context.Context, event aiOutboxEvent) error {
	if event.EventID == "" {
		event.EventID = fmt.Sprintf("crm-test-%d", time.Now().UnixNano())
	}
	if event.SchemaVersion == "" {
		event.SchemaVersion = "crm-event-v1"
	}
	if event.OccurredAt == "" {
		event.OccurredAt = time.Now().UTC().Format(time.RFC3339)
	}
	if event.Producer == "" {
		event.Producer = "crm-test-outbox"
	}
	if event.CorrelationID == "" {
		event.CorrelationID = event.EventID
	}
	if event.TraceID == "" {
		event.TraceID = event.EventID
	}
	if event.DataClass == "" {
		event.DataClass = "C1"
	}
	if event.AggregateVersion < 1 {
		event.AggregateVersion = 1
	}
	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage(`{}`)
	}
	if event.EventType == "" || event.AggregateType == "" || event.AggregateID == "" {
		return fmt.Errorf("event_type, aggregate_type and aggregate_id are required")
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(ctx, `INSERT INTO ai_outbox(event_id,event_json) VALUES($1,$2) ON CONFLICT(event_id) DO NOTHING`, event.EventID, raw)
	return err
}

func (a *App) enqueueTaskAIEvent(ctx context.Context, eventType, taskID, actorID string, version int, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		logAIOutboxError(err)
		return
	}
	err = a.enqueueAIEvent(ctx, aiOutboxEvent{
		EventID:          fmt.Sprintf("crm-task-%s-%d", taskID, time.Now().UnixNano()),
		EventType:        eventType,
		OccurredAt:       time.Now().UTC().Format(time.RFC3339),
		AggregateType:    "task",
		AggregateID:      taskID,
		AggregateVersion: version,
		ActorUserID:      actorID,
		DataClass:        "C1",
		Payload:          raw,
	})
	if err != nil {
		logAIOutboxError(err)
	}
}

func (a *App) enqueueLeadAIEvent(ctx context.Context, eventType, leadID, actorID string, version int, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		logAIOutboxError(err)
		return
	}
	err = a.enqueueAIEvent(ctx, aiOutboxEvent{
		EventID:          fmt.Sprintf("crm-lead-%s-%d", leadID, time.Now().UnixNano()),
		EventType:        eventType,
		OccurredAt:       time.Now().UTC().Format(time.RFC3339),
		AggregateType:    "lead",
		AggregateID:      leadID,
		AggregateVersion: version,
		ActorUserID:      actorID,
		DataClass:        "C1",
		Payload:          raw,
	})
	if err != nil {
		logAIOutboxError(err)
	}
}

func (a *App) enqueueDealAIEvent(ctx context.Context, eventType, dealID, actorID string, version int, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		logAIOutboxError(err)
		return
	}
	err = a.enqueueAIEvent(ctx, aiOutboxEvent{
		EventID:          fmt.Sprintf("crm-deal-%s-%d", dealID, time.Now().UnixNano()),
		EventType:        eventType,
		OccurredAt:       time.Now().UTC().Format(time.RFC3339),
		AggregateType:    "deal",
		AggregateID:      dealID,
		AggregateVersion: version,
		ActorUserID:      actorID,
		DataClass:        "C1",
		Payload:          raw,
	})
	if err != nil {
		logAIOutboxError(err)
	}
}

func (a *App) enqueueDealAIEventFromDB(ctx context.Context, eventType, dealID, actorID string, version int) {
	var title, amount, currency, pipelineID, stageID, stageName, ownerID, leadID string
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(d.title,''), coalesce(d.amount::text,''), coalesce(d.currency,''),
		       coalesce(d.pipeline_id::text,''), coalesce(d.stage_id::text,''),
		       coalesce(ps.name,''), coalesce(d.owner_id::text,''), coalesce(d.lead_id::text,'')
		FROM deals d
		LEFT JOIN pipeline_stages ps ON ps.id=d.stage_id
		WHERE d.id=$1::uuid`, dealID).
		Scan(&title, &amount, &currency, &pipelineID, &stageID, &stageName, &ownerID, &leadID)
	if err != nil {
		logAIOutboxError(err)
		return
	}
	a.enqueueDealAIEvent(ctx, eventType, dealID, actorID, version, map[string]any{
		"title":       title,
		"amount":      amount,
		"currency":    currency,
		"pipeline_id": pipelineID,
		"stage_id":    stageID,
		"stage":       stageName,
		"owner_id":    ownerID,
		"lead_id":     leadID,
	})
}

func (a *App) enqueueCallAIEventFromDB(ctx context.Context, eventType, callID, actorID string, version int) {
	var direction, provider, disposition, recordingKey, entityType, entityID string
	var duration int
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(direction,''), coalesce(provider,''), coalesce(disposition,''),
		       coalesce(recording_key,''), coalesce(entity_type,''), coalesce(entity_id::text,''),
		       coalesce(duration_sec,0)
		FROM calls WHERE id=$1::uuid`, callID).
		Scan(&direction, &provider, &disposition, &recordingKey, &entityType, &entityID, &duration)
	if err != nil {
		logAIOutboxError(err)
		return
	}
	recordingStatus := "missing"
	if strings.TrimSpace(recordingKey) != "" {
		recordingStatus = "available"
	}
	err = a.enqueueAIEvent(ctx, aiOutboxEvent{
		EventID:          fmt.Sprintf("crm-call-%s-%d", callID, time.Now().UnixNano()),
		EventType:        eventType,
		OccurredAt:       time.Now().UTC().Format(time.RFC3339),
		AggregateType:    "call",
		AggregateID:      callID,
		AggregateVersion: version,
		ActorUserID:      actorID,
		DataClass:        "C1",
		Payload: aiOutboxJSON(map[string]any{
			"direction":        direction,
			"provider":         provider,
			"disposition":      disposition,
			"duration_sec":     duration,
			"recording_status": recordingStatus,
			"entity_type":      entityType,
			"entity_id":        entityID,
		}),
	})
	if err != nil {
		logAIOutboxError(err)
	}
}

func (a *App) enqueueEmployeeAIEvent(ctx context.Context, employeeID, actorID string, version int, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		logAIOutboxError(err)
		return
	}
	if err := a.enqueueAIEvent(ctx, aiOutboxEvent{
		EventID:          fmt.Sprintf("crm-employee-%s-%d", employeeID, time.Now().UnixNano()),
		EventType:        "employee.activity",
		OccurredAt:       time.Now().UTC().Format(time.RFC3339),
		AggregateType:    "employee",
		AggregateID:      employeeID,
		AggregateVersion: version,
		ActorUserID:      actorID,
		DataClass:        "C1",
		Payload:          raw,
	}); err != nil {
		logAIOutboxError(err)
	}
}

func aiOutboxJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func (a *App) aiOutboxLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := a.deliverOneAIOutbox(context.Background()); err != nil {
			logAIOutboxError(err)
		}
	}
}

func (a *App) deliverOneAIOutbox(ctx context.Context) error {
	if strings.TrimSpace(a.cfg.AIControlURL) == "" || strings.TrimSpace(a.cfg.AIControlToken) == "" {
		return nil
	}
	var id int64
	var eventID string
	var raw []byte
	var attempts int
	err := a.db.QueryRow(ctx, `UPDATE ai_outbox SET attempts=attempts+1,updated_at=now() WHERE id=(SELECT id FROM ai_outbox WHERE status='pending' AND next_attempt_at<=now() ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING id,event_id,event_json,attempts`).Scan(&id, &eventID, &raw, &attempts)
	if err != nil {
		return nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, a.cfg.AIControlURL+"/v1/events", bytes.NewReader(raw))
	if err != nil {
		return a.failAIOutbox(ctx, id, attempts, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AI-Token", a.cfg.AIControlToken)
	res, err := (&http.Client{Timeout: 9 * time.Second}).Do(req)
	if err == nil {
		defer res.Body.Close()
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
			_, updateErr := a.db.Exec(ctx, `UPDATE ai_outbox SET status='sent',last_error='',updated_at=now() WHERE id=$1`, id)
			return updateErr
		}
		err = fmt.Errorf("AI Control returned HTTP %d", res.StatusCode)
	}
	return a.failAIOutbox(ctx, id, attempts, err)
}

func (a *App) failAIOutbox(ctx context.Context, id int64, attempts int, err error) error {
	status := "pending"
	if attempts >= 5 {
		status = "failed"
	}
	_, updateErr := a.db.Exec(ctx, `UPDATE ai_outbox SET status=$1,last_error=$2,next_attempt_at=now()+($3::int * interval '2 seconds'),updated_at=now() WHERE id=$4`, status, err.Error(), attempts, id)
	return updateErr
}

func logAIOutboxError(err error) {
	if err != nil {
		fmt.Printf("ai outbox delivery: %v\n", err)
	}
}
