package server

import (
	"encoding/json"
	"os"
	"time"
)

// agentDebugLog — session debug NDJSON (folded regions in call sites).
func agentDebugLog(hypothesisID, location, message string, data map[string]any) {
	payload := map[string]any{
		"sessionId":    "a5b483",
		"hypothesisId": hypothesisID,
		"location":     location,
		"message":      message,
		"data":         data,
		"timestamp":    time.Now().UnixMilli(),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	f, err := os.OpenFile("/Users/khamzat/bitrix-isolation/.cursor/debug-a5b483.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func tailDigits(s string, n int) string {
	d := digitsOnly(s)
	if len(d) <= n {
		return d
	}
	return d[len(d)-n:]
}
