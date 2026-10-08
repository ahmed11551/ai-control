package server

import (
	"net/http"
)

func (a *App) voxInboundStatus(w http.ResponseWriter, r *http.Request) {
	if !a.canUseExolveInbound(r) {
		writeJSON(w, 403, map[string]string{"error": "нет права настраивать телефонию"})
		return
	}

	user := a.cfg.VoxInboundUser()
	full := a.cfg.VoxSIPURI()
	out := map[string]any{
		"configured":   user != "",
		"vox_user":     user,
		"vox_username": full,
		"steps": []string{
			"В manage.voximplant.com купите или возьмите номер и привяжите к приложению bitrix-iso (Numbers → Attach).",
			"Вставьте telephony/scenarios/main.js в сценарий приложения. Заполните EXOLVE_* только для исходящих на город; для входящих — VOX_INBOUND_USER и VOX_INBOUND_DIDS (ваш номер Vox).",
			"Routing rule: pattern на ваш DID (или .*) → этот сценарий. Save.",
			"В CRM вкладка Voximplant → Подключиться (agent «На линии»).",
			"Позвоните на номер Vox с другого телефона → плашка «Ответить».",
		},
		"hint": "",
	}
	if user == "" {
		out["hint"] = "Задайте VITE_VOX_USERNAME в .env (agent1@app.account.voximplant.com)"
	} else {
		out["hint"] = "Exolve здесь не используется. Входящие — только номер и сценарий в кабинете Voximplant."
	}
	writeJSON(w, 200, out)
}

// Vox inbound is configured in Voximplant Control Panel, not via Exolve API.
func (a *App) voxEnableInbound(w http.ResponseWriter, r *http.Request) {
	if !a.canUseExolveInbound(r) {
		writeJSON(w, 403, map[string]string{"error": "нет права настраивать телефонию"})
		return
	}
	writeJSON(w, 400, map[string]string{
		"error": "Входящие Vox настраиваются в кабинете Voximplant (номер + сценарий main.js), не через Exolve. См. блок «Входящие» на вкладке Voximplant.",
	})
}
