package server

import (
	"net/http"
	"os"
	"strings"
)

// autodialPausedMsg — скорозвон временно выключен, код не удаляем.
const autodialPausedMsg = "скорозвон временно приостановлен"

// autodialFeatureEnabled — kill-switch. По умолчанию ВЫКЛ.
// Включить снова: AUTODIAL_ENABLED=1 (или true/on/yes).
func autodialFeatureEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("AUTODIAL_ENABLED")))
	switch v {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

func writeAutodialPaused(w http.ResponseWriter) {
	writeJSON(w, 503, map[string]any{
		"error":   autodialPausedMsg,
		"paused":  true,
		"feature": "autodial",
	})
}
