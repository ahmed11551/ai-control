package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const exolveRecordingKeyPrefix = "exolve:"

func exolveRecordingKey(callID string) string {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return ""
	}
	return exolveRecordingKeyPrefix + callID
}

func parseExolveRecordingKey(key string) string {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(key, exolveRecordingKeyPrefix) {
		return strings.TrimSpace(strings.TrimPrefix(key, exolveRecordingKeyPrefix))
	}
	return ""
}

func exolveDownloadURL(callID, path string) string {
	path = strings.TrimSpace(path)
	if path != "" && (strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "http://")) {
		return path
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return ""
	}
	return "https://api.exolve.ru/statistics/download/" + callID
}

// stampExolveCallIDOnCalls — привязать Exolve call_id к недавнему softphone-звонку (до crr).
func (a *App) stampExolveCallIDOnCalls(ctx context.Context, ev exolveCallEvent) {
	callID := strings.TrimSpace(ev.CallID)
	if callID == "" || ev.Type == "crr" {
		return
	}
	peers := exolveEventPeerPhones(ev)
	if len(peers) == 0 {
		return
	}
	for _, peer := range peers {
		tag, err := a.db.Exec(ctx, `
			UPDATE calls
			SET exolve_call_id=$1
			WHERE id = (
				SELECT id FROM calls
				WHERE coalesce(exolve_call_id,'') = ''
				  AND started_at > now() - interval '15 minutes'
				  AND (
				    coalesce(provider,'') = 'exolve'
				    OR asterisk_uniqueid LIKE 'softphone:exolve:%'
				  )
				  AND (
				    regexp_replace(coalesce(from_number,''),'[^0-9]','','g') LIKE '%'||$2
				    OR regexp_replace(coalesce(to_number,''),'[^0-9]','','g') LIKE '%'||$2
				  )
				ORDER BY started_at DESC
				LIMIT 1
			)`, callID, peerTail(peer))
		if err == nil && tag.RowsAffected() > 0 {
			return
		}
	}
}

func exolveEventPeerPhones(ev exolveCallEvent) []string {
	raw := []string{ev.NumberA, ev.Source, ev.Destination, ev.CalledNumber, ev.CallingNumber}
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		n := maxNormPhone(r)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

func peerTail(phone string) string {
	d := maxNormPhone(phone)
	if len(d) >= 10 {
		return d[len(d)-10:]
	}
	return d
}

// attachExolveRecordingRef сохраняет только ссылку на запись (без скачивания WAV).
func (a *App) attachExolveRecordingRef(ctx context.Context, exolveID, path string, peers []string, recordDuration int) bool {
	exolveID = strings.TrimSpace(exolveID)
	if exolveID == "" {
		return false
	}
	key := exolveRecordingKey(exolveID)
	callID := a.findCallForExolveRecording(ctx, exolveID, peers)
	if callID == "" {
		callID = a.ensureCallForExolveRecording(ctx, exolveID, peers, recordDuration)
	}
	if callID == "" {
		autodialLogf("recording ref miss call=%s peers=%v", exolveID, peers)
		return false
	}
	_, err := a.db.Exec(ctx, `
		UPDATE calls
		SET recording_key=$2,
		    exolve_call_id=$3,
		    duration_sec=CASE WHEN $4>0 AND duration_sec=0 THEN $4 ELSE duration_sec END
		WHERE id=$1::uuid`, callID, key, exolveID, recordDuration)
	if err != nil {
		autodialLogf("recording ref update fail call=%s crm=%s: %v", exolveID, callID, err)
		return false
	}
	a.patchCallActivityRecording(ctx, callID, exolveID, key, recordDuration, path)
	a.enqueueCallAIEventFromDB(ctx, "call.completed", callID, "", int(time.Now().Unix()))
	a.hub.Broadcast("call.updated", map[string]any{
		"id":              callID,
		"exolve_call_id":  exolveID,
		"recording_key":   key,
		"record_duration": recordDuration,
	})
	autodialLogf("recording ref OK exolve=%s crm=%s dur=%d", exolveID, callID, recordDuration)
	return true
}

func (a *App) findCallForExolveRecording(ctx context.Context, exolveID string, peers []string) string {
	var id string
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM calls
		WHERE exolve_call_id=$1 OR recording_key=$2
		ORDER BY started_at DESC LIMIT 1`,
		exolveID, exolveRecordingKey(exolveID)).Scan(&id)
	if id != "" {
		return id
	}
	var dealID string
	_ = a.db.QueryRow(ctx, `
		SELECT deal_id::text FROM autodial_attempts
		WHERE exolve_call_id=$1
		ORDER BY started_at DESC LIMIT 1`, exolveID).Scan(&dealID)
	if dealID != "" {
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM calls
			WHERE entity_type='deal' AND entity_id=$1::uuid
			  AND started_at > now() - interval '24 hours'
			ORDER BY started_at DESC LIMIT 1`, dealID).Scan(&id)
		if id != "" {
			return id
		}
		var leadID string
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(lead_id::text,'') FROM deals WHERE id=$1::uuid`, dealID).Scan(&leadID)
		if leadID != "" {
			_ = a.db.QueryRow(ctx, `
				SELECT id::text FROM calls
				WHERE entity_type='lead' AND entity_id=$1::uuid
				  AND started_at > now() - interval '24 hours'
				ORDER BY started_at DESC LIMIT 1`, leadID).Scan(&id)
			if id != "" {
				return id
			}
		}
	}
	for _, peer := range peers {
		tail := peerTail(peer)
		if tail == "" {
			continue
		}
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM calls
			WHERE started_at > now() - interval '15 minutes'
			  AND (
			    coalesce(provider,'') = 'exolve'
			    OR asterisk_uniqueid LIKE 'softphone:exolve:%'
			    OR asterisk_uniqueid LIKE 'exolve:%'
			  )
			  AND (
			    regexp_replace(coalesce(from_number,''),'[^0-9]','','g') LIKE '%'||$1
			    OR regexp_replace(coalesce(to_number,''),'[^0-9]','','g') LIKE '%'||$1
			  )
			ORDER BY started_at DESC LIMIT 1`, tail).Scan(&id)
		if id != "" {
			return id
		}
	}
	return ""
}

func (a *App) ensureCallForExolveRecording(ctx context.Context, exolveID string, peers []string, duration int) string {
	peer := ""
	if len(peers) > 0 {
		peer = peers[0]
	}
	uid := "exolve:" + exolveID
	ev := callEvent{
		UID:       uid,
		Provider:  "exolve",
		Direction: "outbound",
		To:        peer,
		From:      "",
		Duration:  duration,
		Disp:      "answered",
	}
	if len(peers) > 1 {
		ev.From = peers[1]
	}
	id, err := a.upsertCall(ctx, ev)
	if err != nil || id == "" {
		return ""
	}
	_ = a.attachCallCard(ctx, id, ev)
	var dealID string
	_ = a.db.QueryRow(ctx, `
		SELECT deal_id::text FROM autodial_attempts
		WHERE exolve_call_id=$1 ORDER BY started_at DESC LIMIT 1`, exolveID).Scan(&dealID)
	if dealID != "" {
		_, _ = a.db.Exec(ctx, `
			UPDATE calls SET entity_type='deal', entity_id=$2::uuid
			WHERE id=$1::uuid AND (entity_id IS NULL OR coalesce(entity_type,'')='')`,
			id, dealID)
	}
	return id
}

func (a *App) patchCallActivityRecording(ctx context.Context, callID, exolveID, key string, duration int, path string) {
	var entityType, entityID string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(entity_type,''), coalesce(entity_id::text,'')
		FROM calls WHERE id=$1::uuid`, callID).Scan(&entityType, &entityID)
	if entityType == "" || entityID == "" {
		return
	}
	var actID string
	var payloadRaw []byte
	_ = a.db.QueryRow(ctx, `
		SELECT id::text, payload FROM activity_log
		WHERE entity_type=$1 AND entity_id=$2::uuid AND action='call'
		  AND payload->>'call_id'=$3
		ORDER BY created_at DESC LIMIT 1`,
		entityType, entityID, callID).Scan(&actID, &payloadRaw)
	payload := map[string]any{}
	if len(payloadRaw) > 0 {
		_ = json.Unmarshal(payloadRaw, &payload)
	}
	if actID == "" {
		// autodial_finished — допишем recording к последнему событию с этим exolve call_id
		_ = a.db.QueryRow(ctx, `
			SELECT id::text, payload FROM activity_log
			WHERE entity_type=$1 AND entity_id=$2::uuid
			  AND action IN ('call','autodial_finished')
			  AND (payload->>'call_id'=$3 OR payload->>'exolve_call_id'=$3)
			ORDER BY created_at DESC LIMIT 1`,
			entityType, entityID, exolveID).Scan(&actID, &payloadRaw)
		if len(payloadRaw) > 0 {
			_ = json.Unmarshal(payloadRaw, &payload)
		}
	}
	payload["call_id"] = callID
	payload["exolve_call_id"] = exolveID
	payload["recording_key"] = key
	payload["recording_url"] = "/telephony/calls/" + callID + "/recording"
	if duration > 0 {
		payload["record_duration"] = duration
	}
	if strings.TrimSpace(path) != "" {
		payload["exolve_path"] = path
	}
	b, _ := json.Marshal(payload)
	if actID != "" {
		_, _ = a.db.Exec(ctx, `UPDATE activity_log SET payload=$2::jsonb WHERE id=$1::uuid`, actID, string(b))
		return
	}
	a.logAct(ctx, entityType, entityID, "", "call", payload)
}

// scheduleExolveRecordingPoll — fallback после autodial hangup, если crr ещё не пришёл.
func (a *App) scheduleExolveRecordingPoll(exolveCallID string) {
	exolveCallID = strings.TrimSpace(exolveCallID)
	if exolveCallID == "" || strings.TrimSpace(a.cfg.ExolveAPIKey) == "" {
		return
	}
	go func() {
		delays := []time.Duration{20 * time.Second, 40 * time.Second, 60 * time.Second, 90 * time.Second}
		for _, d := range delays {
			time.Sleep(d)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			ok := a.pollExolveRecordingReady(ctx, exolveCallID)
			cancel()
			if ok {
				return
			}
		}
	}()
}

func (a *App) pollExolveRecordingReady(ctx context.Context, exolveCallID string) bool {
	var n uint64
	if _, err := fmt.Sscanf(exolveCallID, "%d", &n); err != nil || n == 0 {
		return false
	}
	var out struct {
		Call []struct {
			VoiceRecordExist bool   `json:"voice_record_exist"`
			RecordCall       bool   `json:"record_call"`
			From             string `json:"from"`
			To               string `json:"to"`
			Duration         int    `json:"duration"`
		} `json:"call"`
	}
	if err := a.exolvePOST(ctx, "https://api.exolve.ru/statistics/call-history/v2/GetInfo", map[string]any{
		"call_id": []uint64{n},
	}, &out); err != nil || len(out.Call) == 0 {
		return false
	}
	c := out.Call[0]
	if !c.VoiceRecordExist {
		return false
	}
	peers := []string{}
	if p := maxNormPhone(c.From); p != "" {
		peers = append(peers, p)
	}
	if p := maxNormPhone(c.To); p != "" {
		peers = append(peers, p)
	}
	return a.attachExolveRecordingRef(ctx, exolveCallID, exolveDownloadURL(exolveCallID, ""), peers, c.Duration)
}

func (a *App) getCallRecording(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "нет id"})
		return
	}
	var recordingKey, exolveID, entityType, entityID string
	err := a.db.QueryRow(r.Context(), `
		SELECT coalesce(recording_key,''), coalesce(exolve_call_id,''),
		       coalesce(entity_type,''), coalesce(entity_id::text,'')
		FROM calls WHERE id=$1::uuid`, id).Scan(&recordingKey, &exolveID, &entityType, &entityID)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "звонок не найден"})
		return
	}
	if entityType == "lead" && entityID != "" {
		if hideIfMissing(w, a.canSeeLead(r.Context(), a.crmScope(r), entityID)) {
			return
		}
	} else if entityType == "deal" && entityID != "" {
		if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), entityID)) {
			return
		}
	}
	if exolveID == "" {
		exolveID = parseExolveRecordingKey(recordingKey)
	}
	if voxURL := parseVoxRecordingKey(recordingKey); voxURL != "" {
		a.proxyVoxRecording(w, r, voxURL)
		return
	}
	if exolveID == "" {
		writeJSON(w, 404, map[string]string{"error": "нет записи"})
		return
	}
	if strings.TrimSpace(a.cfg.ExolveAPIKey) == "" {
		writeJSON(w, 503, map[string]string{"error": "нет EXOLVE_API_KEY"})
		return
	}
	url := exolveDownloadURL(exolveID, "")
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "прокси"})
		return
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.ExolveAPIKey)
	client := &http.Client{Timeout: 60 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "Exolve недоступен"})
		return
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		writeJSON(w, 502, map[string]string{"error": fmt.Sprintf("Exolve download %d", res.StatusCode)})
		return
	}
	ct := res.Header.Get("Content-Type")
	if ct == "" || strings.Contains(ct, "json") {
		ct = "audio/wav"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, io.LimitReader(res.Body, 64<<20))
}
