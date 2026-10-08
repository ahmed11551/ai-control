package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const voxRecordingKeyPrefix = "vox:"

func voxRecordingKey(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}
	if strings.HasPrefix(url, voxRecordingKeyPrefix) {
		return url
	}
	return voxRecordingKeyPrefix + url
}

func parseVoxRecordingKey(key string) string {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(key, voxRecordingKeyPrefix) {
		return strings.TrimSpace(strings.TrimPrefix(key, voxRecordingKeyPrefix))
	}
	return ""
}

func recordingURLFromHook(raw map[string]any) string {
	raw = flattenHook(raw)
	for _, k := range []string{
		"recording_url", "record_url", "recordingUrl", "recordUrl",
		"url", "record", "recording",
	} {
		if v := firstScalar(raw[k]); v != "" {
			if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
				return v
			}
			if strings.HasPrefix(v, voxRecordingKeyPrefix) {
				return parseVoxRecordingKey(v)
			}
		}
	}
	return ""
}

func hookLooksLikeVoxRecording(raw map[string]any) bool {
	raw = flattenHook(raw)
	ev := strings.ToLower(firstScalar(raw["event"]) + firstScalar(raw["EventType"]) + firstScalar(raw["type"]))
	if strings.Contains(ev, "record") {
		return recordingURLFromHook(raw) != ""
	}
	return recordingURLFromHook(raw) != "" && (raw["recording_url"] != nil || raw["record_url"] != nil || raw["recordingUrl"] != nil)
}

func hookPeerPhones(raw map[string]any) []string {
	raw = flattenHook(raw)
	keys := []string{"from", "to", "callerid", "callerId", "destination", "peer", "number_a", "number_b", "src", "dst"}
	seen := map[string]bool{}
	out := make([]string, 0, 4)
	for _, k := range keys {
		n := maxNormPhone(firstScalar(raw[k]))
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// attachVoxRecordingRef — URL записи Vox → calls.recording_key (проигрывание в карточке).
func (a *App) attachVoxRecordingRef(ctx context.Context, recordURL string, peers []string, duration int) bool {
	recordURL = strings.TrimSpace(recordURL)
	if recordURL == "" || (!strings.HasPrefix(recordURL, "http://") && !strings.HasPrefix(recordURL, "https://")) {
		return false
	}
	key := voxRecordingKey(recordURL)
	callID := a.findCallForVoxRecording(ctx, key, peers)
	// Без peers не создаём orphan и не цепляем «любой свежий» звонок — иначе запись уезжает не туда.
	if callID == "" && len(peers) > 0 {
		callID = a.ensureCallForVoxRecording(ctx, peers, duration)
	}
	if callID == "" {
		autodialLogf("vox recording miss peers=%v", peers)
		return false
	}
	_, err := a.db.Exec(ctx, `
		UPDATE calls
		SET recording_key=$2,
		    provider=CASE WHEN coalesce(provider,'')='' THEN 'voximplant' ELSE provider END,
		    duration_sec=CASE WHEN $3>0 AND duration_sec=0 THEN $3 ELSE duration_sec END
		WHERE id=$1::uuid`, callID, key, duration)
	if err != nil {
		autodialLogf("vox recording update fail crm=%s: %v", callID, err)
		return false
	}
	a.patchCallActivityRecording(ctx, callID, "", key, duration, recordURL)
	a.enqueueCallAIEventFromDB(ctx, "call.completed", callID, "", int(time.Now().Unix()))
	a.hub.Broadcast("call.updated", map[string]any{
		"id":            callID,
		"recording_key": key,
		"provider":      "voximplant",
	})
	autodialLogf("vox recording OK crm=%s peers=%v", callID, peers)
	return true
}

func (a *App) findCallForVoxRecording(ctx context.Context, key string, peers []string) string {
	var id string
	if key != "" {
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM calls
			WHERE recording_key=$1
			ORDER BY started_at DESC LIMIT 1`, key).Scan(&id)
		if id != "" {
			return id
		}
	}
	for _, peer := range peers {
		tail := peerTail(peer)
		if tail == "" {
			continue
		}
		// Только по хвосту номера; без записи предпочитаем; без «любой свежий vox».
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM calls
			WHERE started_at > now() - interval '15 minutes'
			  AND (
			    coalesce(provider,'') IN ('voximplant','vox')
			    OR asterisk_uniqueid LIKE 'softphone:voximplant:%'
			    OR asterisk_uniqueid LIKE 'voximplant:%'
			  )
			  AND (
			    right(regexp_replace(coalesce(from_number,''),'[^0-9]','','g'), length($1)) = $1
			    OR right(regexp_replace(coalesce(to_number,''),'[^0-9]','','g'), length($1)) = $1
			  )
			ORDER BY CASE WHEN coalesce(recording_key,'') = '' THEN 0 ELSE 1 END,
			         started_at DESC
			LIMIT 1`, tail).Scan(&id)
		if id != "" {
			return id
		}
	}
	return ""
}

func (a *App) ensureCallForVoxRecording(ctx context.Context, peers []string, duration int) string {
	peer := ""
	if len(peers) > 0 {
		peer = peers[0]
	}
	uid := "voximplant:rec:" + fmt.Sprintf("%d", time.Now().UnixNano())
	ev := callEvent{
		UID:       uid,
		Provider:  "voximplant",
		Direction: "outbound",
		To:        peer,
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
	return id
}

func (a *App) proxyVoxRecording(w http.ResponseWriter, r *http.Request, url string) {
	url = strings.TrimSpace(url)
	if url == "" {
		writeJSON(w, 404, map[string]string{"error": "нет записи Vox"})
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "прокси"})
		return
	}
	if auth, err := a.voxServiceAccountAuthHeader(); err != nil {
		log.Printf("vox recording auth: %v", err)
		writeJSON(w, 503, map[string]string{"error": "нет Vox service account для secure-записи"})
		return
	} else if auth != "" {
		req.Header.Set("Authorization", auth)
	} else if strings.Contains(url, "records-secure") || strings.Contains(url, "securecords") || strings.Contains(url, "logs-secure") {
		writeJSON(w, 503, map[string]string{"error": "secure-запись: положите voximplant-service-account.json и задайте VOX_SERVICE_ACCOUNT_JSON"})
		return
	}
	client := &http.Client{Timeout: 60 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "Vox recording недоступен"})
		return
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		writeJSON(w, 502, map[string]string{"error": fmt.Sprintf("Vox download %d", res.StatusCode)})
		return
	}
	ct := res.Header.Get("Content-Type")
	if ct == "" || strings.Contains(ct, "json") || strings.Contains(ct, "text") {
		ct = "audio/mpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, io.LimitReader(res.Body, 64<<20))
}
