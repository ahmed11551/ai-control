package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const ticketPublicTTL = 45 * time.Minute

func (a *App) ticketFileSign(dealID, fileID string, exp int64) string {
	secret := strings.TrimSpace(a.cfg.JWTSecret)
	if secret == "" {
		secret = "forge"
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%s:%s:%d", dealID, fileID, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *App) ticketFilePublicURL(dealID, fileID string) string {
	base := strings.TrimRight(strings.TrimSpace(a.cfg.PublicBaseURL), "/")
	if base == "" {
		return ""
	}
	exp := time.Now().Add(ticketPublicTTL).Unix()
	sig := a.ticketFileSign(dealID, fileID, exp)
	q := url.Values{}
	q.Set("d", dealID)
	q.Set("f", fileID)
	q.Set("e", strconv.FormatInt(exp, 10))
	q.Set("s", sig)
	return base + "/api/public/ticket-file?" + q.Encode()
}

// publicTicketFile — короткоживущая ссылка для Wappi (скачать файл билета без JWT).
func (a *App) publicTicketFile(w http.ResponseWriter, r *http.Request) {
	dealID := strings.TrimSpace(r.URL.Query().Get("d"))
	fileID := strings.TrimSpace(r.URL.Query().Get("f"))
	sig := strings.TrimSpace(r.URL.Query().Get("s"))
	exp, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("e")), 10, 64)
	if dealID == "" || fileID == "" || sig == "" || exp == 0 {
		writeJSON(w, 400, map[string]string{"error": "bad token"})
		return
	}
	if time.Now().Unix() > exp {
		writeJSON(w, 410, map[string]string{"error": "ссылка истекла"})
		return
	}
	want := a.ticketFileSign(dealID, fileID, exp)
	if !hmac.Equal([]byte(want), []byte(sig)) {
		writeJSON(w, 403, map[string]string{"error": "неверная подпись"})
		return
	}
	var key, filename, mime, kind string
	err := a.db.QueryRow(r.Context(), `
		SELECT storage_key, filename, coalesce(mime,''), coalesce(kind,'')
		FROM deal_ticket_files WHERE id=$1::uuid AND deal_id=$2::uuid`, fileID, dealID).
		Scan(&key, &filename, &mime, &kind)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	if ticketFileKindSensitive(kind) {
		writeJSON(w, 403, map[string]string{"error": "запрещено"})
		return
	}
	data, err := os.ReadFile(filepath.Join(ticketFilesRoot(), key))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "файл отсутствует"})
		return
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

// dealOwnerForTicketSend — менеджер сделки (от чьего имени шлём билет).
func (a *App) dealOwnerForTicketSend(ctx context.Context, dealID string) (ownerID, ownerName, leadID string) {
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(d.owner_id::text,''), coalesce(u.full_name,''),
		       coalesce(nullif(d.lead_id::text,''),
		         (SELECT l.id::text FROM leads l WHERE l.converted_deal_id=d.id LIMIT 1), '')
		FROM deals d
		LEFT JOIN users u ON u.id=d.owner_id
		WHERE d.id=$1::uuid`, dealID).Scan(&ownerID, &ownerName, &leadID)
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "<nil>" {
		ownerID = ""
	}
	ownerName = strings.TrimSpace(ownerName)
	leadID = strings.TrimSpace(leadID)
	if leadID == "<nil>" {
		leadID = ""
	}
	return
}

func (a *App) wappiSendFileURL(ctx context.Context, channel, profileID, chatID, phone, fileURL, fileName, caption string) (messageID string, err error) {
	profileID = strings.TrimSpace(profileID)
	fileURL = strings.TrimSpace(fileURL)
	if profileID == "" || fileURL == "" {
		return "", fmt.Errorf("нет профиля или ссылки на файл")
	}
	payload := map[string]any{
		"url":       fileURL,
		"file_name": strings.TrimSpace(fileName),
		"caption":   strings.TrimSpace(caption),
		"as_file":   true,
	}
	ch := normChannel(channel)
	ph := maxNormPhone(phone)
	chat := strings.TrimSpace(chatID)
	switch ch {
	case "max":
		if chat != "" && chatPhone(chat) == "" {
			payload["chat_id"] = chat
		} else if ph != "" {
			payload["recipient"] = ph
		} else if chat != "" {
			payload["recipient"] = chat
		} else {
			return "", fmt.Errorf("нет получателя")
		}
	default:
		recipient, c := inboxRecipient(chatID, phone)
		if c != "" && (strings.Contains(c, "@") || chatPhone(c) == "") {
			payload["chat_id"] = c
		} else if recipient != "" {
			payload["recipient"] = recipient
		} else {
			return "", fmt.Errorf("нет получателя")
		}
	}
	path := "maxapi/sync/message/file/url/send"
	if ch != "max" {
		path = "api/sync/message/file/url/send"
	}
	u := "https://wappi.pro/" + path + "?profile_id=" + url.QueryEscape(profileID)
	bodyResp, err := a.postMessengerBody(ctx, u, map[string]string{
		"Authorization": a.cfg.WappiToken,
		"Content-Type":  "application/json",
	}, payload, "Wappi")
	if err != nil {
		return "", err
	}
	return extractWappiMessageID(bodyResp), nil
}
