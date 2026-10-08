package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
)

func (a *App) yandexMailLoop() {
	if !a.cfg.YandexMailReady() {
		return
	}
	sec := a.cfg.YandexIMAPPollSec
	if sec < 15 {
		sec = 15
	}
	log.Printf("Yandex Mail: IMAP poll каждые %ds (%s)", sec, a.cfg.YandexIMAPUser)
	for {
		n, err := a.yandexMailPollOnce()
		if err != nil {
			log.Println("Yandex Mail:", err)
		} else if n > 0 {
			log.Printf("Yandex Mail: создано лидов %d", n)
		}
		time.Sleep(time.Duration(sec) * time.Second)
	}
}

func (a *App) yandexMailPollOnce() (int, error) {
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp4", "imap.yandex.ru:993", &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: "imap.yandex.ru",
	})
	if err != nil {
		return 0, fmt.Errorf("connect: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	cli, err := imapclient.New(conn)
	if err != nil {
		_ = conn.Close()
		return 0, fmt.Errorf("imap: %w", err)
	}
	defer cli.Logout()

	if err := cli.Login(a.cfg.YandexIMAPUser, a.cfg.YandexIMAPPassword); err != nil {
		return 0, fmt.Errorf("login: %w", err)
	}
	folder := a.cfg.YandexIMAPFolder
	if folder == "" {
		folder = "INBOX"
	}
	mbox, err := cli.Select(folder, false)
	if err != nil {
		return 0, fmt.Errorf("select: %w", err)
	}
	if mbox.Messages == 0 {
		return 0, nil
	}

	crit := imap.NewSearchCriteria()
	// Недавние письма; фильтр VK — в коде. Уже прочитанные тоже берём (дедуп по Message-ID).
	crit.Since = time.Now().Add(-14 * 24 * time.Hour)
	ids, err := cli.Search(crit)
	if err != nil {
		return 0, fmt.Errorf("search: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	// Пачка за один poll. Берём хвост (свежие), но больше 40 — иначе при всплеске
	// старые письма навсегда остаются за окном «последних N».
	if len(ids) > 200 {
		ids = ids[len(ids)-200:]
	}

	seq := new(imap.SeqSet)
	seq.AddNum(ids...)
	section := &imap.BodySectionName{}
	items := []imap.FetchItem{section.FetchItem(), imap.FetchEnvelope, imap.FetchFlags, imap.FetchUid}
	messages := make(chan *imap.Message, 16)
	done := make(chan error, 1)
	go func() {
		done <- cli.Fetch(seq, items, messages)
	}()

	created := 0
	ctx := context.Background()
	scanned := 0
	for msg := range messages {
		if msg == nil {
			continue
		}
		from := ""
		subject := ""
		if msg.Envelope != nil {
			subject = msg.Envelope.Subject
			if len(msg.Envelope.From) > 0 {
				from = msg.Envelope.From[0].Address()
			}
		}
		if !isVKAdsMail(from, subject) {
			continue
		}
		scanned++
		r := msg.GetBody(section)
		if r == nil {
			continue
		}
		body, msgID, err := readMailText(r)
		if err != nil {
			log.Println("Yandex Mail parse body:", err)
			continue
		}
		if msgID == "" {
			msgID = fmt.Sprintf("uid-%d", msg.Uid)
		}
		leadID, err := a.ingestYandexVKMail(ctx, msgID, subject, body, from)
		if err != nil {
			log.Println("Yandex Mail ingest:", err)
			continue
		}
		if leadID != "" {
			created++
		}
	}
	if err := <-done; err != nil {
		return created, err
	}
	log.Printf("Yandex Mail: vk-писем %d, новых лидов %d", scanned, created)
	return created, nil
}

func isVKAdsMail(from, subject string) bool {
	f := strings.ToLower(from)
	s := strings.ToLower(subject)
	if strings.Contains(f, "ads.notifications@vk.company") || strings.Contains(f, "@vk.company") {
		return true
	}
	if strings.Contains(s, "новая заявка") && (strings.Contains(s, "форм") || strings.Contains(s, "лид")) {
		return true
	}
	return false
}

func readMailText(r io.Reader) (body, messageID string, err error) {
	mr, err := mail.CreateReader(r)
	if err != nil {
		return "", "", err
	}
	if h := mr.Header.Get("Message-Id"); h != "" {
		messageID = strings.TrimSpace(h)
	}
	var parts []string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := h.ContentType()
			if strings.HasPrefix(ct, "text/plain") || strings.HasPrefix(ct, "text/html") {
				b, _ := io.ReadAll(io.LimitReader(p.Body, 1<<20))
				txt := string(b)
				if strings.HasPrefix(ct, "text/html") {
					txt = stripHTML(txt)
				}
				parts = append(parts, txt)
			}
		}
	}
	return strings.Join(parts, "\n"), messageID, nil
}

func stripHTML(s string) string {
	re := regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	s = re.ReplaceAllString(s, " ")
	re = regexp.MustCompile(`(?i)<br\s*/?>`)
	s = re.ReplaceAllString(s, "\n")
	re = regexp.MustCompile(`(?i)</p>`)
	s = re.ReplaceAllString(s, "\n")
	re = regexp.MustCompile(`<[^>]+>`)
	s = re.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	return s
}

var (
	reMailPhone  = regexp.MustCompile(`(?i)(?:телефон|phone|мобильный|номер(?:\s+телефона)?|тел\.?)\s*[:\-–]?\s*([+\d][\d\s\-()]{8,20})`)
	reAnyRUPhone = regexp.MustCompile(`(?:\+?7|8)[\s\-()]*\d{3}[\s\-()]*\d{3}[\s\-()]*\d{2}[\s\-()]*\d{2}`)
	reMailEmail  = regexp.MustCompile(`(?i)(?:e-?mail|почта|email)\s*[:\-–]?\s*([^\s<>]+@[^\s<>]+)`)
	reMailName   = regexp.MustCompile(`(?i)(?:имя|фио|name|клиент)\s*[:\-–]?\s*([^\n\r]{2,80})`)
	reMailForm   = regexp.MustCompile(`(?i)(?:форм[аеыу]|form)\s*[:\-–]?\s*([^\n\r]{2,120})`)
	reSubjForm   = regexp.MustCompile(`(?i)форм[аеыу]\s*[:\-–]\s*(.+)`)
)

func extractMailPhone(body string) string {
	phone := ""
	if m := reMailPhone.FindStringSubmatch(body); len(m) > 1 {
		phone = digitsOnly(m[1])
	}
	if phone == "" {
		if m := reAnyRUPhone.FindString(body); m != "" {
			phone = digitsOnly(m)
		}
	}
	if len(phone) == 11 && (phone[0] == '8' || phone[0] == '7') {
		phone = "7" + phone[1:]
	}
	if len(phone) < 10 {
		return ""
	}
	return phone
}

func parseVKAdsMail(subject, body string) map[string]any {
	out := map[string]any{}
	form := ""
	if m := reSubjForm.FindStringSubmatch(subject); len(m) > 1 {
		form = strings.TrimSpace(m[1])
	}
	if form == "" {
		if m := reMailForm.FindStringSubmatch(body); len(m) > 1 {
			form = strings.TrimSpace(m[1])
		}
	}
	name := ""
	if m := reMailName.FindStringSubmatch(body); len(m) > 1 {
		name = strings.TrimSpace(m[1])
	}
	phone := extractMailPhone(body)
	email := ""
	if m := reMailEmail.FindStringSubmatch(body); len(m) > 1 {
		email = strings.TrimSpace(m[1])
	}
	if name == "" && form != "" {
		name = form
	}
	if name == "" {
		name = strings.TrimSpace(subject)
	}
	out["name"] = name
	out["phone"] = phone
	out["email"] = email
	out["form"] = form
	out["title"] = "VK Ads: " + nz(name, form)
	if form != "" {
		out["title"] = "VK Ads · " + form
		if name != "" && !strings.EqualFold(name, form) {
			out["title"] = "VK Ads · " + form + " · " + name
		}
	}
	return out
}

func (a *App) ingestYandexVKMail(ctx context.Context, messageID, subject, body, from string) (string, error) {
	messageID = strings.TrimSpace(messageID)
	ext := "ym:" + messageID
	if len(ext) > 200 {
		ext = ext[:200]
	}
	var existing string
	_ = a.db.QueryRow(ctx, `SELECT id::text FROM leads WHERE source='vk_ads' AND vk_lead_id=$1`, ext).Scan(&existing)
	if existing != "" {
		return "", nil
	}
	// Уже обрабатывали это письмо (в т.ч. когда скипали по телефону/title).
	var seen int
	_ = a.db.QueryRow(ctx, `
		SELECT 1 FROM inbound_webhooks
		WHERE source='yandex_mail' AND coalesce(payload->>'message_id','')=$1
		LIMIT 1`, messageID).Scan(&seen)
	if seen == 1 {
		return "", nil
	}

	fields := parseVKAdsMail(subject, body)
	fields["lead_id"] = ext
	fields["source"] = "vk_ads"
	if form, _ := fields["form"].(string); form != "" {
		fields["campaign_id"] = form
	}

	phone, _ := fields["phone"].(string)
	if dup := a.existingYandexVKAdsLead(ctx, strings.TrimSpace(phone)); dup != "" {
		// #region agent log
		agentDebugLog("H2", "mail_yandex.go:ingest", "skip_duplicate_phone", map[string]any{
			"phone_suffix": tailDigits(phone, 4), "dup": true,
		})
		// #endregion
		_, _ = a.db.Exec(ctx, `INSERT INTO inbound_webhooks (source, payload) VALUES ('yandex_mail', $1::jsonb)`,
			mustJSON(map[string]any{
				"message_id": messageID,
				"from":       from,
				"subject":    subject,
				"parsed":     fields,
				"skipped":    "duplicate",
				"lead_id":    dup,
			}))
		return "", nil
	}

	_, _ = a.db.Exec(ctx, `INSERT INTO inbound_webhooks (source, payload) VALUES ('yandex_mail', $1::jsonb)`,
		mustJSON(map[string]any{
			"message_id": messageID,
			"from":       from,
			"subject":    subject,
			"parsed":     fields,
		}))
	id, err := a.ingestVKLead(ctx, fields)
	if err != nil {
		return "", err
	}
	// #region agent log
	agentDebugLog("H2", "mail_yandex.go:ingest", "lead_created", map[string]any{
		"phone_suffix": tailDigits(phone, 4), "created": true,
	})
	// #endregion
	if a.hub != nil {
		a.hub.Broadcast("lead.created", map[string]any{"id": id, "source": "yandex_mail"})
	}
	return id, nil
}

// existingYandexVKAdsLead — дедуп только по телефону.
// Title/name раньше схлопывали разных людей с одинаковым именем («Дмитрий») в один лид.
func (a *App) existingYandexVKAdsLead(ctx context.Context, phone string) string {
	if phone == "" {
		return ""
	}
	return a.existingVKAdsLeadByPhone(ctx, phone)
}

// existingVKAdsLeadByPhone — лид VK Ads с тем же телефоном.
func (a *App) existingVKAdsLeadByPhone(ctx context.Context, phone string) string {
	norm := normalizePhone(phone)
	if norm == "" {
		return ""
	}
	var id string
	_ = a.db.QueryRow(ctx, `
		SELECT l.id::text
		FROM leads l
		JOIN contacts c ON c.id = l.contact_id
		WHERE l.source = 'vk_ads'
		  AND regexp_replace(coalesce(c.phone,''), '[^0-9]', '', 'g') LIKE '%' || $1
		ORDER BY l.created_at DESC
		LIMIT 1`, norm).Scan(&id)
	return id
}

// reprocessSkippedYandexMail — поднимает письма, ошибочно скипнутые name/title-дедупом.
// Безопасно: только phone-дедуп, message_id → vk_lead_id, без удаления существующих лидов.
func (a *App) reprocessSkippedYandexMail(ctx context.Context, since, until time.Time, limit int) (created, skippedDup, errors int, err error) {
	if limit <= 0 || limit > 5000 {
		limit = 2000
	}
	rows, err := a.db.Query(ctx, `
		SELECT DISTINCT ON (right(regexp_replace(coalesce(payload->'parsed'->>'phone',''), '[^0-9]', '', 'g'), 10))
			id,
			coalesce(payload->>'message_id',''),
			coalesce(payload->'parsed'->>'phone',''),
			coalesce(payload->'parsed'->>'name',''),
			coalesce(payload->'parsed'->>'title',''),
			coalesce(payload->'parsed'->>'email',''),
			coalesce(payload->'parsed'->>'form','')
		FROM inbound_webhooks
		WHERE source='yandex_mail'
		  AND payload->>'skipped'='duplicate'
		  AND created_at >= $1 AND created_at < $2
		  AND coalesce(payload->'parsed'->>'phone','') <> ''
		  AND coalesce(payload->>'reprocessed','') = ''
		ORDER BY right(regexp_replace(coalesce(payload->'parsed'->>'phone',''), '[^0-9]', '', 'g'), 10), created_at
		LIMIT $3`, since, until, limit)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()

	type item struct {
		whID, msgID, phone, name, title, email, form string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.whID, &it.msgID, &it.phone, &it.name, &it.title, &it.email, &it.form); err != nil {
			return created, skippedDup, errors, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return created, skippedDup, errors, err
	}

	// #region agent log
	agentDebugLog("H2", "mail_yandex.go:reprocess", "candidates", map[string]any{
		"count": len(items), "since": since.Format(time.RFC3339), "until": until.Format(time.RFC3339),
	})
	// #endregion

	for _, it := range items {
		if dup := a.existingVKAdsLeadByPhone(ctx, it.phone); dup != "" {
			skippedDup++
			_, _ = a.db.Exec(ctx, `
				UPDATE inbound_webhooks
				SET payload = payload || jsonb_build_object('reprocessed','already_phone','reprocessed_at', $2::text)
				WHERE id=$1::uuid`, it.whID, time.Now().UTC().Format(time.RFC3339))
			continue
		}
		ext := "ym:" + strings.TrimSpace(it.msgID)
		if ext == "ym:" {
			ext = "ym:reprocess:" + it.whID
		}
		if len(ext) > 200 {
			ext = ext[:200]
		}
		fields := map[string]any{
			"lead_id":     ext,
			"source":      "vk_ads",
			"phone":       it.phone,
			"name":        it.name,
			"email":       it.email,
			"title":       it.title,
			"form":        it.form,
			"campaign_id": it.form,
		}
		id, ierr := a.ingestVKLead(ctx, fields)
		if ierr != nil {
			errors++
			log.Println("Yandex Mail reprocess:", ierr)
			continue
		}
		created++
		_, _ = a.db.Exec(ctx, `
			UPDATE inbound_webhooks
			SET payload = payload || jsonb_build_object('reprocessed','created','reprocessed_lead_id',$2::text,'reprocessed_at',$3::text,'skipped',NULL)
			WHERE id=$1::uuid`, it.whID, id, time.Now().UTC().Format(time.RFC3339))
		if a.hub != nil {
			a.hub.Broadcast("lead.created", map[string]any{"id": id, "source": "yandex_mail_reprocess"})
		}
	}
	// #region agent log
	agentDebugLog("H2", "mail_yandex.go:reprocess", "done", map[string]any{
		"created": created, "skipped_dup": skippedDup, "errors": errors,
	})
	// #endregion
	log.Printf("Yandex Mail reprocess: created=%d skipped_phone=%d errors=%d", created, skippedDup, errors)
	return created, skippedDup, errors, nil
}

// POST /distribution/bank/reprocess-mail — admin: поднять ложно-скипнутые письма за день (МСК).
func (a *App) postReprocessYandexMail(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только для администратора"})
		return
	}
	day := strings.TrimSpace(r.URL.Query().Get("day"))
	if day == "" {
		day = "2026-09-21"
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	start, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "day=YYYY-MM-DD"})
		return
	}
	end := start.Add(24 * time.Hour)
	limit := 2000
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 {
			limit = n
		}
	}
	created, skipped, errs, err := a.reprocessSkippedYandexMail(r.Context(), start.UTC(), end.UTC(), limit)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "day": day, "created": created, "skipped_phone": skipped, "errors": errs,
	})
}

// catchupYandexMailDay — дотянуть с IMAP письма за день, которых ещё нет в inbound_webhooks.
// Без лимита «последние N»: проходит все seq за день пачками.
func (a *App) catchupYandexMailDay(ctx context.Context, dayStartMSK time.Time) (scanned, created, skippedExisting int, err error) {
	if !a.cfg.YandexMailReady() {
		return 0, 0, 0, fmt.Errorf("yandex imap not configured")
	}
	dayEnd := dayStartMSK.Add(24 * time.Hour)

	dialer := &net.Dialer{Timeout: 20 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp4", "imap.yandex.ru:993", &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: "imap.yandex.ru",
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("connect: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(15 * time.Minute))
	cli, err := imapclient.New(conn)
	if err != nil {
		_ = conn.Close()
		return 0, 0, 0, fmt.Errorf("imap: %w", err)
	}
	defer cli.Logout()

	if err := cli.Login(a.cfg.YandexIMAPUser, a.cfg.YandexIMAPPassword); err != nil {
		return 0, 0, 0, fmt.Errorf("login: %w", err)
	}
	folder := a.cfg.YandexIMAPFolder
	if folder == "" {
		folder = "INBOX"
	}
	if _, err := cli.Select(folder, false); err != nil {
		return 0, 0, 0, fmt.Errorf("select: %w", err)
	}

	crit := imap.NewSearchCriteria()
	crit.Since = dayStartMSK
	crit.Before = dayEnd
	ids, err := cli.Search(crit)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("search: %w", err)
	}
	// #region agent log
	agentDebugLog("H-catchup", "mail_yandex.go:catchup", "search_ids", map[string]any{
		"day": dayStartMSK.Format("2006-01-02"), "ids": len(ids),
	})
	// #endregion

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{section.FetchItem(), imap.FetchEnvelope, imap.FetchUid}
	const batch = 40
	for i := 0; i < len(ids); i += batch {
		end := i + batch
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]
		seq := new(imap.SeqSet)
		seq.AddNum(chunk...)
		messages := make(chan *imap.Message, 16)
		done := make(chan error, 1)
		go func() {
			done <- cli.Fetch(seq, items, messages)
		}()
		for msg := range messages {
			if msg == nil {
				continue
			}
			from, subject := "", ""
			if msg.Envelope != nil {
				subject = msg.Envelope.Subject
				if len(msg.Envelope.From) > 0 {
					from = msg.Envelope.From[0].Address()
				}
			}
			if !isVKAdsMail(from, subject) {
				continue
			}
			scanned++
			r := msg.GetBody(section)
			if r == nil {
				continue
			}
			body, msgID, perr := readMailText(r)
			if perr != nil {
				log.Println("Yandex Mail catchup parse:", perr)
				continue
			}
			if msgID == "" {
				msgID = fmt.Sprintf("uid-%d", msg.Uid)
			}
			// Уже в webhook — не трогаем (reprocess отдельно).
			var seen int
			_ = a.db.QueryRow(ctx, `
				SELECT 1 FROM inbound_webhooks
				WHERE source='yandex_mail' AND coalesce(payload->>'message_id','')=$1
				LIMIT 1`, msgID).Scan(&seen)
			if seen == 1 {
				skippedExisting++
				continue
			}
			leadID, ierr := a.ingestYandexVKMail(ctx, msgID, subject, body, from)
			if ierr != nil {
				log.Println("Yandex Mail catchup ingest:", ierr)
				continue
			}
			if leadID != "" {
				created++
			}
		}
		if ferr := <-done; ferr != nil {
			return scanned, created, skippedExisting, ferr
		}
		_ = conn.SetDeadline(time.Now().Add(15 * time.Minute))
	}
	// #region agent log
	agentDebugLog("H-catchup", "mail_yandex.go:catchup", "done", map[string]any{
		"scanned": scanned, "created": created, "skipped_existing": skippedExisting,
	})
	// #endregion
	log.Printf("Yandex Mail catchup %s: scanned=%d created=%d already=%d",
		dayStartMSK.Format("2006-01-02"), scanned, created, skippedExisting)
	return scanned, created, skippedExisting, nil
}

// POST /distribution/bank/catchup-mail — admin: дотянуть письма дня с IMAP.
func (a *App) postCatchupYandexMail(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только для администратора"})
		return
	}
	day := strings.TrimSpace(r.URL.Query().Get("day"))
	if day == "" {
		day = "2026-09-21"
	}
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.FixedZone("MSK", 3*3600)
	}
	start, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "day=YYYY-MM-DD"})
		return
	}
	scanned, created, already, err := a.catchupYandexMailDay(r.Context(), start)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "day": day, "scanned": scanned, "created": created, "already_in_crm": already,
	})
}
