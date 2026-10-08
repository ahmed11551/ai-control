package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func autodialLogf(format string, args ...any) {
	log.Printf("autodial| "+format, args...)
	// #region agent log
	msg := fmt.Sprintf(format, args...)
	payload, _ := json.Marshal(map[string]any{
		"sessionId":    "a5b483",
		"runId":        "post-fix17",
		"hypothesisId": "BE",
		"location":     "autodialLogf",
		"message":      msg,
		"timestamp":    time.Now().UnixMilli(),
	})
	if f, err := os.OpenFile("/Users/khamzat/bitrix-isolation/.cursor/debug-a5b483.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		_, _ = f.Write(append(payload, '\n'))
		_ = f.Close()
	}
	// #endregion
}

type exolveInboundNumber struct {
	Number         string `json:"number"`
	OnSoftphone    bool   `json:"on_softphone"`
	RedirectTo     string `json:"redirect_to,omitempty"`
	Source         string `json:"source,omitempty"`
	ForwardingType int    `json:"forwarding_type,omitempty"`
	SipURI         string `json:"sip_uri,omitempty"`
}

func (a *App) exolveInboundStatus(w http.ResponseWriter, r *http.Request) {
	if !a.canUseExolveInbound(r) {
		writeJSON(w, 403, map[string]string{"error": "нет права настраивать телефонию"})
		return
	}

	sip := a.cfg.ExolveSIPLogin
	configured := a.cfg.ExolveAPIKey != "" && sip != ""
	out := map[string]any{
		"configured": configured,
		"api_key":    a.cfg.ExolveAPIKey != "",
		"sip_login":  sip,
		"numbers":    []exolveInboundNumber{},
		"hint":       "",
	}
	if a.cfg.ExolveAPIKey == "" {
		out["hint"] = "Добавьте EXOLVE_API_KEY в .env (ЛК Exolve → приложение → API-ключ) и перезапустите API"
		writeJSON(w, 200, out)
		return
	}
	if sip == "" {
		out["hint"] = "Добавьте EXOLVE_SIP_LOGIN или VITE_EXOLVE_SIP_LOGIN в .env"
		writeJSON(w, 200, out)
		return
	}

	numbers, err := a.listExolveInboundNumbers(r.Context())
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	out["numbers"] = numbers
	if len(numbers) == 0 {
		out["hint"] = "Укажите DID в EXOLVE_INBOUND_NUMBERS или введите номер вручную ниже. Номер — городской Exolve, не SIP 88314…"
	}
	writeJSON(w, 200, out)
}

func (a *App) exolveEnableInbound(w http.ResponseWriter, r *http.Request) {
	if !a.canUseExolveInbound(r) {
		writeJSON(w, 403, map[string]string{"error": "нет права настраивать телефонию"})
		return
	}
	if !a.cfg.ExolveReady() {
		writeJSON(w, 503, map[string]string{
			"error": "Задайте EXOLVE_API_KEY и EXOLVE_SIP_LOGIN (или VITE_EXOLVE_SIP_LOGIN) в .env и перезапустите API",
		})
		return
	}

	var in struct {
		Number string `json:"number"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "некорректный JSON"})
		return
	}
	number := normalizeExolveDID(in.Number)
	if number == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите номер Exolve (DID), на который будут звонить"})
		return
	}
	if strings.HasPrefix(number, "88314") {
		writeJSON(w, 400, map[string]string{"error": "нужен городской/мобильный номер Exolve, не SIP login 88314…"})
		return
	}

	sip := a.cfg.ExolveSIPLogin
	if err := a.exolveSetCallForwarding(r.Context(), number, sip); err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok":     true,
		"number": number,
		"sip":    sip,
		"message": fmt.Sprintf(
			"Входящие на %s направлены на SIP %s. Softphone должен быть «На линии», затем позвоните на %s для проверки.",
			number, sip, number,
		),
	})
}

func (a *App) exolveSetCallForwarding(ctx context.Context, number, sipLogin string) error {
	code, err := strconv.ParseUint(number, 10, 64)
	if err != nil {
		return fmt.Errorf("некорректный номер")
	}
	sipLogin = strings.TrimSpace(sipLogin)
	if sipLogin == "" {
		return fmt.Errorf("нет SIP login для переадресации")
	}
	body := map[string]any{
		"number_code":          code,
		"call_forwarding_type": 2,
		"call_forwarding_number": map[string]any{
			"redirect_type": 1,
			"call_control": []map[string]any{
				{
					"redirect_number": sipLogin,
					"active":          true,
					"timeout":         60,
					"name":            "ForgeCRM softphone",
				},
			},
		},
	}
	return a.exolvePOST(ctx, "https://api.exolve.ru/number/v1/SetCallForwarding", body, nil)
}

func (a *App) exolveSetDisplayNumber(ctx context.Context, sipResourceID, number string) error {
	rid, err := strconv.ParseUint(strings.TrimSpace(sipResourceID), 10, 64)
	if err != nil || rid == 0 {
		return fmt.Errorf("нет sip_resource_id — переимпортируйте SIP менеджера")
	}
	code, err := strconv.ParseUint(number, 10, 64)
	if err != nil {
		return fmt.Errorf("некорректный номер CLI")
	}
	return a.exolvePOST(ctx, "https://api.exolve.ru/sip/v1/SetDisplayNumber", map[string]any{
		"sip_resource_id": rid,
		"number":          code,
	}, nil)
}

func (a *App) canUseExolveInbound(r *http.Request) bool {
	if isAdmin(r) {
		return true
	}
	uid := actorID(r)
	if uid == "" {
		return false
	}
	var can bool
	err := a.db.QueryRow(r.Context(), `SELECT can_call FROM users WHERE id=$1 AND is_active=true`, uid).Scan(&can)
	return err == nil && can
}

func (a *App) listExolveInboundNumbers(ctx context.Context) ([]exolveInboundNumber, error) {
	candidates := map[string]string{} // number -> source

	for _, raw := range strings.Split(a.cfg.ExolveInboundNums, ",") {
		n := normalizeExolveDID(raw)
		if n != "" && !strings.HasPrefix(n, "88314") {
			candidates[n] = "env"
		}
	}

	sipID, cli, forwarded, err := a.exolveSIPContext(ctx, a.cfg.ExolveSIPLogin)
	if err == nil {
		_ = sipID
		if c := normalizeExolveDID(cli); c != "" && !strings.HasPrefix(c, "88314") {
			if _, ok := candidates[c]; !ok {
				candidates[c] = "sip_cli"
			}
		}
		for _, n := range forwarded {
			nn := normalizeExolveDID(n)
			if nn == "" || strings.HasPrefix(nn, "88314") {
				continue
			}
			if _, ok := candidates[nn]; !ok {
				candidates[nn] = "forwarding"
			}
		}
	}

	// Collect CLIs from other SIP IDs in the app (often = purchased DIDs used as caller ID)
	if clis, err := a.exolveListSIPCLIs(ctx); err == nil {
		for _, c := range clis {
			nn := normalizeExolveDID(c)
			if nn == "" || strings.HasPrefix(nn, "88314") {
				continue
			}
			if _, ok := candidates[nn]; !ok {
				candidates[nn] = "sip_list"
			}
		}
	}

	out := make([]exolveInboundNumber, 0, len(candidates))
	for number, source := range candidates {
		item := exolveInboundNumber{Number: number, Source: source}
		attrs, err := a.exolveNumberAttributes(ctx, number)
		if err == nil {
			item.RedirectTo = attrs.redirectTo
			item.ForwardingType = attrs.forwardingType
			item.SipURI = attrs.sipURI
			item.OnSoftphone = attrs.forwardingType == 2 && attrs.redirectTo == a.cfg.ExolveSIPLogin
		}
		out = append(out, item)
	}
	// Stable-ish order: on softphone first, then by number
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			swap := false
			if out[j].OnSoftphone && !out[i].OnSoftphone {
				swap = true
			} else if out[j].OnSoftphone == out[i].OnSoftphone && out[j].Number < out[i].Number {
				swap = true
			}
			if swap {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

type numberFwdAttrs struct {
	forwardingType int
	redirectTo     string
	sipURI         string
}

func (a *App) exolveNumberAttributes(ctx context.Context, number string) (numberFwdAttrs, error) {
	code, err := strconv.ParseUint(number, 10, 64)
	if err != nil {
		return numberFwdAttrs{}, err
	}
	var raw map[string]any
	if err := a.exolvePOST(ctx, "https://api.exolve.ru/number/v1/GetAttributes", map[string]any{
		"number_code": code,
	}, &raw); err != nil {
		return numberFwdAttrs{}, err
	}
	attrs, _ := raw["attributes"].(map[string]any)
	if attrs == nil {
		return numberFwdAttrs{}, nil
	}
	ft := anyToInt(attrs["call_forwarding_type"])
	redirect := ""
	if fn, ok := attrs["call_forwarding_number"].(map[string]any); ok {
		if cc, ok := fn["call_control"].([]any); ok && len(cc) > 0 {
			if first, ok := cc[0].(map[string]any); ok {
				redirect = strings.TrimSpace(fmt.Sprint(first["redirect_number"]))
				if redirect == "<nil>" {
					redirect = ""
				}
			}
		}
	}
	sipURI := ""
	if fs, ok := attrs["call_forwarding_sip"].(map[string]any); ok {
		sipURI = strings.TrimSpace(fmt.Sprint(fs["sip_uri"]))
		if sipURI == "<nil>" {
			sipURI = ""
		}
		sipURI = strings.TrimPrefix(strings.ToLower(sipURI), "sip:")
	}
	out := numberFwdAttrs{forwardingType: ft, sipURI: sipURI}
	if redirect != "" {
		out.redirectTo = normalizeExolveDID(redirect)
	} else if sipURI != "" {
		out.redirectTo = sipURI
	}
	return out, nil
}

func (a *App) exolveSIPContext(ctx context.Context, sipLogin string) (resourceID, cli string, forwarded []string, err error) {
	var list struct {
		Sips []struct {
			SipResourceID json.Number `json:"sip_resource_id"`
			UserName      string      `json:"user_name"`
			NumberCode    string      `json:"number_code"`
			CLI           string      `json:"cli"`
		} `json:"sips"`
	}
	if err := a.exolvePOST(ctx, "https://api.exolve.ru/number/customer/v1/GetSIPList", map[string]any{
		"limit":           256,
		"offset":          0,
		"username_filter": sipLogin,
	}, &list); err != nil {
		return "", "", nil, err
	}
	var id string
	for _, s := range list.Sips {
		u := strings.TrimSpace(s.UserName)
		if u == "" {
			u = strings.TrimSpace(s.NumberCode)
		}
		if u == sipLogin {
			id = s.SipResourceID.String()
			cli = s.CLI
			break
		}
	}
	if id == "" && len(list.Sips) == 1 {
		id = list.Sips[0].SipResourceID.String()
		cli = list.Sips[0].CLI
	}
	if id == "" {
		return "", cli, nil, fmt.Errorf("SIP %s не найден в приложении Exolve", sipLogin)
	}

	var detail struct {
		Attributes struct {
			CLI string `json:"cli"`
		} `json:"attributes"`
		NumbersWithCallForwarding []json.Number `json:"numbers_with_call_forwarding"`
	}
	rid, _ := strconv.ParseUint(id, 10, 64)
	if err := a.exolvePOST(ctx, "https://api.exolve.ru/sip/v1/GetAttributes", map[string]any{
		"sip_resource_id": rid,
	}, &detail); err == nil {
		if detail.Attributes.CLI != "" {
			cli = detail.Attributes.CLI
		}
		for _, n := range detail.NumbersWithCallForwarding {
			forwarded = append(forwarded, n.String())
		}
	}
	return id, cli, forwarded, nil
}

func (a *App) exolveListSIPCLIs(ctx context.Context) ([]string, error) {
	var list struct {
		Sips []struct {
			CLI string `json:"cli"`
		} `json:"sips"`
	}
	if err := a.exolvePOST(ctx, "https://api.exolve.ru/number/customer/v1/GetSIPList", map[string]any{
		"limit":  256,
		"offset": 0,
	}, &list); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(list.Sips))
	for _, s := range list.Sips {
		if strings.TrimSpace(s.CLI) != "" {
			out = append(out, s.CLI)
		}
	}
	return out, nil
}

func (a *App) exolvePOST(ctx context.Context, url string, body any, dest any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.ExolveAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 25 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Exolve недоступен: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = res.Status
		}
		var er struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &er) == nil {
			if er.Error != "" {
				msg = er.Error
			} else if er.Message != "" {
				msg = er.Message
			}
		}
		if res.StatusCode == 404 || strings.Contains(strings.ToLower(msg), "number not found") {
			return fmt.Errorf("номер не найден в приложении Exolve (купите DID в ЛК или укажите номер из этого приложения, не личный мобильный)")
		}
		return fmt.Errorf("Exolve API (%d): %s", res.StatusCode, truncateRunes(msg, 240))
	}
	if dest == nil || len(raw) == 0 || string(raw) == "{}" || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("ответ Exolve: %w", err)
	}
	return nil
}

func normalizeExolveDID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	digits := make([]rune, 0, len(raw))
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	s := string(digits)
	if len(s) == 11 && s[0] == '8' {
		return "7" + s[1:]
	}
	return s
}

func anyToInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	case int:
		return t
	default:
		n, _ := strconv.Atoi(fmt.Sprint(v))
		return n
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
