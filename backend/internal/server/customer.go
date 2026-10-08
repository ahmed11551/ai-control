package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

type customerSiteDef struct {
	Code  string
	Name  string
	Login string
}

func customerSiteCatalog() []customerSiteDef {
	return []customerSiteDef{
		{Code: "msk_sao", Name: "Москва — САО", Login: "zakazchik.msk"},
		{Code: "novgorod", Name: "Великий Новгород", Login: "zakazchik.novgorod"},
		{Code: "saransk", Name: "Саранск", Login: "zakazchik.saransk"},
		{Code: "smolensk", Name: "Смоленск", Login: "zakazchik.smolensk"},
	}
}

func customerSiteByCode(code string) (customerSiteDef, bool) {
	code = strings.TrimSpace(code)
	for _, site := range customerSiteCatalog() {
		if site.Code == code {
			return site, true
		}
	}
	return customerSiteDef{}, false
}

func isCustomerRole(role string) bool {
	return strings.EqualFold(strings.TrimSpace(role), "customer")
}

// customerAPIAllowed — заказчик ходит только в свой кабинет и в /me.
func customerAPIAllowed(path string) bool {
	path = strings.TrimSpace(path)
	if path == "/me" || path == "/customer" {
		return true
	}
	return strings.HasPrefix(path, "/customer/")
}

func (a *App) customerCRMGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isCustomerRole(roleName(r)) || customerAPIAllowed(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		writeJSON(w, 403, map[string]string{"error": "недоступно"})
	})
}

func customerFilesRoot() string {
	root, _ := os.Getwd()
	return filepath.Join(root, "data", "customer-files")
}

func (a *App) ensureCustomerSchema(ctx context.Context) {
	a.customerSchemaOnce.Do(func() {
		a.ensureCustomerSchemaRun(ctx)
	})
}

func (a *App) ensureCustomerSchemaRun(ctx context.Context) {
	if a.db == nil {
		return
	}
	_, _ = a.db.Exec(ctx, `
		INSERT INTO roles (name, permissions)
		VALUES ('customer', '{"customer":true}'::jsonb)
		ON CONFLICT (name) DO NOTHING`)
	_, _ = a.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS customer_sites (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			code TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL
		)`)
	_, _ = a.db.Exec(ctx, `
		ALTER TABLE users ADD COLUMN IF NOT EXISTS customer_site_id UUID REFERENCES customer_sites(id)`)
	_, _ = a.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS customer_submissions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
			site_id UUID NOT NULL REFERENCES customer_sites(id),
			full_name TEXT NOT NULL,
			birth_date DATE NOT NULL,
			passport_number TEXT NOT NULL,
			arrival_date DATE NOT NULL,
			position TEXT NOT NULL,
			sent_by UUID,
			sent_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			recalled_at TIMESTAMPTZ
		)`)
	_, _ = a.db.Exec(ctx, `
		ALTER TABLE customer_submissions
			ADD COLUMN IF NOT EXISTS arrived_at TIMESTAMPTZ,
			ADD COLUMN IF NOT EXISTS arrived_by UUID`)
	_, _ = a.db.Exec(ctx, `
		CREATE UNIQUE INDEX IF NOT EXISTS customer_submissions_deal_site
		ON customer_submissions (deal_id, site_id)`)
	_, _ = a.db.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS customer_submissions_site_open_idx
		ON customer_submissions (site_id, arrival_date)
		WHERE recalled_at IS NULL`)
	_, _ = a.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS customer_submission_files (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			submission_id UUID NOT NULL REFERENCES customer_submissions(id) ON DELETE CASCADE,
			source_file_id UUID,
			kind TEXT NOT NULL DEFAULT '',
			filename TEXT NOT NULL DEFAULT '',
			mime TEXT NOT NULL DEFAULT '',
			size_bytes BIGINT NOT NULL DEFAULT 0,
			storage_key TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	for _, site := range customerSiteCatalog() {
		_, _ = a.db.Exec(ctx, `
			INSERT INTO customer_sites (code, name) VALUES ($1, $2)
			ON CONFLICT (code) DO UPDATE SET name=EXCLUDED.name`, site.Code, site.Name)
	}
	a.seedCustomerUsers(ctx)
}

func (a *App) seedCustomerUsers(ctx context.Context) {
	if a.db == nil {
		return
	}
	var roleID string
	if err := a.db.QueryRow(ctx, `SELECT id::text FROM roles WHERE name='customer'`).Scan(&roleID); err != nil || roleID == "" {
		return
	}
	var created []string
	for _, site := range customerSiteCatalog() {
		var exists int
		_ = a.db.QueryRow(ctx, `SELECT count(*) FROM users WHERE lower(coalesce(login,''))=lower($1)`, site.Login).Scan(&exists)
		if exists > 0 {
			continue
		}
		pass, err := randomCustomerPassword()
		if err != nil {
			continue
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(pass), 12)
		if err != nil {
			continue
		}
		var siteID string
		if err := a.db.QueryRow(ctx, `SELECT id::text FROM customer_sites WHERE code=$1`, site.Code).Scan(&siteID); err != nil || siteID == "" {
			continue
		}
		tag, err := a.db.Exec(ctx, `
			INSERT INTO users (login, password_hash, full_name, role_id, can_call, is_active, customer_site_id)
			SELECT $1, $2, $3, $4::uuid, false, true, $5::uuid
			WHERE NOT EXISTS (SELECT 1 FROM users WHERE lower(coalesce(login,''))=lower($1))`,
			site.Login, string(hash), site.Name, roleID, siteID)
		if err != nil || tag.RowsAffected() == 0 {
			continue
		}
		created = append(created, site.Name+";"+site.Login+";"+pass)
	}
	if len(created) == 0 {
		return
	}
	writeCustomerLoginFile(created)
}

func randomCustomerPassword() (string, error) {
	const alphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, 12)
	max := big.NewInt(int64(len(alphabet)))
	for i := range buf {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		buf[i] = alphabet[n.Int64()]
	}
	return string(buf), nil
}

func writeCustomerLoginFile(rows []string) {
	home, err := os.UserHomeDir()
	path := filepath.Join("data", "customer-logins.csv")
	if err == nil {
		desktop := filepath.Join(home, "Desktop")
		if st, derr := os.Stat(desktop); derr == nil && st.IsDir() {
			path = filepath.Join(desktop, "zakazchiki-lokalno.csv")
		}
	}
	fresh := true
	if _, err := os.Stat(path); err == nil {
		fresh = false
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	if fresh {
		_, _ = f.Write([]byte{0xEF, 0xBB, 0xBF})
		_, _ = io.WriteString(f, "Город;Логин;Пароль\n")
	}
	for _, row := range rows {
		_, _ = io.WriteString(f, row+"\n")
	}
}

type customerForm struct {
	FullName string
	Birth    string
	Passport string
	Arrival  string
	Position string
}

func validateCustomerForm(name, birth, passport, arrival, position string) (customerForm, string) {
	name = strings.TrimSpace(name)
	position = strings.TrimSpace(position)
	if n := len([]rune(name)); n < 2 || n > 200 {
		return customerForm{}, "укажите ФИО"
	}
	if n := len([]rune(position)); n < 1 || n > 120 {
		return customerForm{}, "укажите должность"
	}
	birthDay, ok := parseISODate(birth)
	if !ok {
		return customerForm{}, "укажите дату рождения"
	}
	if dateAfterMoscowToday(birthDay) {
		return customerForm{}, "дата рождения не может быть в будущем"
	}
	if birthDay.Year() < 1920 {
		return customerForm{}, "укажите дату рождения"
	}
	arrivalDay, ok := parseISODate(arrival)
	if !ok || arrivalDay.Year() < 2000 || arrivalDay.Year() > 2100 {
		return customerForm{}, "укажите дату прибытия"
	}
	pass, ok := cleanPassport(passport)
	if !ok {
		return customerForm{}, "укажите серию (4 цифры) и номер (6 цифр)"
	}
	return customerForm{
		FullName: name,
		Birth:    birthDay.Format("2006-01-02"),
		Passport: pass,
		Arrival:  arrivalDay.Format("2006-01-02"),
		Position: position,
	}, ""
}

func parseISODate(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 10 {
		raw = raw[:10]
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func moscowToday() time.Time {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.FixedZone("MSK", 3*3600)
	}
	now := time.Now().In(loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func dateAfterMoscowToday(day time.Time) bool {
	today := moscowToday()
	y, m, d := day.Date()
	ty, tm, td := today.Date()
	if y != ty {
		return y > ty
	}
	if m != tm {
		return m > tm
	}
	return d > td
}

func cleanPassport(raw string) (string, bool) {
	digits := make([]rune, 0, 10)
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9':
			digits = append(digits, r)
		case r == ' ' || r == '-' || r == '\t':
		default:
			return "", false
		}
	}
	if len(digits) != 10 {
		return "", false
	}
	return string(digits[:4]) + " " + string(digits[4:]), true
}

func (a *App) customerSiteOf(ctx context.Context, userID string) (name, code, id string) {
	userID = strings.TrimSpace(userID)
	if userID == "" || a.db == nil {
		return "", "", ""
	}
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(s.name,''), coalesce(s.code,''), coalesce(s.id::text,'')
		FROM users u
		JOIN customer_sites s ON s.id=u.customer_site_id
		WHERE u.id=$1::uuid`, userID).Scan(&name, &code, &id)
	return name, code, id
}

func customerSender(role string) bool {
	return isLogistRole(role)
}

func customerSenderOrAdmin(role string) bool {
	return isLogistRole(role) || strings.EqualFold(strings.TrimSpace(role), "admin")
}

func (a *App) getDealCustomerSends(w http.ResponseWriter, r *http.Request) {
	if !customerSenderOrAdmin(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "недоступно"})
		return
	}
	id := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	a.ensureCustomerSchema(r.Context())
	var suggested string
	_ = a.db.QueryRow(r.Context(), `
		SELECT coalesce(nullif(c.full_name,''), nullif(lc.full_name,''), '')
		FROM deals d
		LEFT JOIN contacts c ON c.id=d.contact_id
		LEFT JOIN leads l ON l.id=d.lead_id
		LEFT JOIN contacts lc ON lc.id=l.contact_id
		WHERE d.id=$1::uuid`, id).Scan(&suggested)
	sites := a.queryMaps(r.Context(), `SELECT code, name FROM customer_sites ORDER BY name`)
	files := a.queryMaps(r.Context(), `
		SELECT id::text, kind, filename, coalesce(mime,'') AS mime, size_bytes
		FROM deal_ticket_files
		WHERE deal_id=$1::uuid AND coalesce(kind,'') <> 'note' AND coalesce(storage_key,'') <> ''
		ORDER BY created_at DESC`, id)
	sent := a.queryMaps(r.Context(), `
		SELECT s.id::text, cs.code AS site_code, cs.name AS site_name,
		       s.full_name, to_char(s.birth_date, 'YYYY-MM-DD') AS birth_date,
		       s.passport_number, to_char(s.arrival_date, 'YYYY-MM-DD') AS arrival_date,
		       s.position, s.sent_at,
		       to_char(timezone('Europe/Moscow', s.arrived_at), 'YYYY-MM-DD HH24:MI') AS arrived_at
		FROM customer_submissions s
		JOIN customer_sites cs ON cs.id=s.site_id
		WHERE s.deal_id=$1::uuid AND s.recalled_at IS NULL
		ORDER BY cs.name`, id)
	writeJSON(w, 200, map[string]any{
		"suggested_name": suggested,
		"sites":          sites,
		"files":          files,
		"sent":           sent,
	})
}

func (a *App) postDealCustomerSend(w http.ResponseWriter, r *http.Request) {
	if !customerSenderOrAdmin(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "недоступно"})
		return
	}
	dealID := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), dealID)) {
		return
	}
	a.ensureCustomerSchema(r.Context())
	var in struct {
		SiteCode       string   `json:"site_code"`
		FullName       string   `json:"full_name"`
		BirthDate      string   `json:"birth_date"`
		PassportNumber string   `json:"passport_number"`
		ArrivalDate    string   `json:"arrival_date"`
		Position       string   `json:"position"`
		FileIDs        []string `json:"file_ids"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	site, ok := customerSiteByCode(in.SiteCode)
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "выберите заказчика"})
		return
	}
	form, msg := validateCustomerForm(in.FullName, in.BirthDate, in.PassportNumber, in.ArrivalDate, in.Position)
	if msg != "" {
		writeJSON(w, 400, map[string]string{"error": msg})
		return
	}
	fileIDs := uniqueIDs(in.FileIDs)
	if len(fileIDs) > 30 {
		writeJSON(w, 400, map[string]string{"error": "слишком много файлов"})
		return
	}
	var siteID string
	if err := a.db.QueryRow(r.Context(), `SELECT id::text FROM customer_sites WHERE code=$1`, site.Code).Scan(&siteID); err != nil || siteID == "" {
		writeJSON(w, 400, map[string]string{"error": "заказчик не найден"})
		return
	}
	var submissionID string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO customer_submissions (
			deal_id, site_id, full_name, birth_date, passport_number, arrival_date, position, sent_by, sent_at, recalled_at
		) VALUES (
			$1::uuid, $2::uuid, $3, $4::date, $5, $6::date, $7, nullif($8,'')::uuid, now(), NULL
		)
		ON CONFLICT (deal_id, site_id) DO UPDATE SET
			full_name=EXCLUDED.full_name,
			birth_date=EXCLUDED.birth_date,
			passport_number=EXCLUDED.passport_number,
			arrival_date=EXCLUDED.arrival_date,
			position=EXCLUDED.position,
			sent_by=EXCLUDED.sent_by,
			sent_at=now(),
			recalled_at=NULL
		RETURNING id::text`,
		dealID, siteID, form.FullName, form.Birth, form.Passport, form.Arrival, form.Position, actorID(r),
	).Scan(&submissionID)
	if err != nil || submissionID == "" {
		writeJSON(w, 400, map[string]string{"error": "не удалось отправить"})
		return
	}
	copied, missing := a.snapshotCustomerFiles(r.Context(), dealID, submissionID, fileIDs)
	if len(copied) > 0 || len(fileIDs) == 0 {
		a.replaceCustomerFiles(r.Context(), submissionID, copied)
	}
	a.logDealLeadAct(r.Context(), dealID, actorID(r), "customer_sent", map[string]any{
		"site":      site.Code,
		"site_name": site.Name,
		"full_name": form.FullName,
		"files":     len(copied),
	})
	writeJSON(w, 201, map[string]any{
		"ok":            true,
		"id":            submissionID,
		"site_name":     site.Name,
		"files":         len(copied),
		"missing_files": missing,
	})
}

type customerFileCopy struct {
	SourceID string
	Kind     string
	Filename string
	Mime     string
	Size     int
	Key      string
}

func uniqueIDs(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (a *App) snapshotCustomerFiles(ctx context.Context, dealID, submissionID string, fileIDs []string) ([]customerFileCopy, []string) {
	if len(fileIDs) == 0 {
		return nil, nil
	}
	copied := make([]customerFileCopy, 0, len(fileIDs))
	var missing []string
	_ = os.MkdirAll(filepath.Join(customerFilesRoot(), submissionID), 0o750)
	for _, fileID := range fileIDs {
		var key, filename, mime, kind string
		err := a.db.QueryRow(ctx, `
			SELECT storage_key, filename, coalesce(mime,''), coalesce(kind,'')
			FROM deal_ticket_files
			WHERE id=$1::uuid AND deal_id=$2::uuid
			  AND coalesce(kind,'') <> 'note' AND coalesce(storage_key,'') <> ''`,
			fileID, dealID).Scan(&key, &filename, &mime, &kind)
		if err != nil {
			missing = append(missing, fileID)
			continue
		}
		data, ok := readTicketFileBytes(key)
		if !ok || len(data) == 0 {
			missing = append(missing, filename)
			continue
		}
		name := filepath.Base(filename)
		if name == "" || name == "." || name == string(os.PathSeparator) {
			name = "document.bin"
		}
		newKey := submissionID + "/" + fmt.Sprintf("%d_%s", time.Now().UnixNano(), name)
		path := filepath.Join(customerFilesRoot(), filepath.FromSlash(newKey))
		if err := os.WriteFile(path, data, 0o640); err != nil {
			missing = append(missing, name)
			continue
		}
		copied = append(copied, customerFileCopy{
			SourceID: fileID,
			Kind:     kind,
			Filename: name,
			Mime:     mime,
			Size:     len(data),
			Key:      newKey,
		})
	}
	return copied, missing
}

func readTicketFileBytes(key string) ([]byte, bool) {
	root, err := filepath.Abs(ticketFilesRoot())
	if err != nil {
		return nil, false
	}
	abs, err := filepath.Abs(filepath.Join(ticketFilesRoot(), filepath.FromSlash(key)))
	if err != nil || !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return nil, false
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, false
	}
	return data, true
}

func (a *App) replaceCustomerFiles(ctx context.Context, submissionID string, files []customerFileCopy) {
	old := a.queryMaps(ctx, `SELECT storage_key FROM customer_submission_files WHERE submission_id=$1::uuid`, submissionID)
	_, _ = a.db.Exec(ctx, `DELETE FROM customer_submission_files WHERE submission_id=$1::uuid`, submissionID)
	for _, f := range files {
		_, err := a.db.Exec(ctx, `
			INSERT INTO customer_submission_files
				(submission_id, source_file_id, kind, filename, mime, size_bytes, storage_key)
			VALUES ($1::uuid, nullif($2,'')::uuid, $3, $4, $5, $6, $7)`,
			submissionID, f.SourceID, f.Kind, f.Filename, f.Mime, f.Size, f.Key)
		if err != nil {
			_ = os.Remove(filepath.Join(customerFilesRoot(), filepath.FromSlash(f.Key)))
		}
	}
	fresh := map[string]bool{}
	for _, f := range files {
		fresh[f.Key] = true
	}
	for _, row := range old {
		key := strings.TrimSpace(fmt.Sprint(row["storage_key"]))
		if key == "" || key == "<nil>" || fresh[key] {
			continue
		}
		abs, ok := customerFileAbs(key)
		if ok {
			_ = os.Remove(abs)
		}
	}
}

func (a *App) deleteDealCustomerSend(w http.ResponseWriter, r *http.Request) {
	if !customerSenderOrAdmin(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "недоступно"})
		return
	}
	dealID := chi.URLParam(r, "id")
	code := chi.URLParam(r, "code")
	if _, ok := customerSiteByCode(code); !ok {
		writeJSON(w, 400, map[string]string{"error": "заказчик не найден"})
		return
	}
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), dealID)) {
		return
	}
	a.ensureCustomerSchema(r.Context())
	tag, err := a.db.Exec(r.Context(), `
		UPDATE customer_submissions s
		SET recalled_at=now()
		FROM customer_sites cs
		WHERE s.site_id=cs.id AND s.deal_id=$1::uuid AND cs.code=$2 AND s.recalled_at IS NULL`,
		dealID, code)
	if err != nil || tag.RowsAffected() == 0 {
		writeJSON(w, 404, map[string]string{"error": "отправка не найдена"})
		return
	}
	a.logDealLeadAct(r.Context(), dealID, actorID(r), "customer_recalled", map[string]any{"site": code})
	writeJSON(w, 200, map[string]any{"ok": true})
}

const customerPeoplePageSize = 20

func cleanDeptFilter(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "none" {
		return "none"
	}
	if len(raw) != 36 {
		return ""
	}
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F', r == '-':
		default:
			return ""
		}
	}
	return raw
}

func customerPeopleFilter(siteID, deptID, q, position, when, from, to, arrived string) (string, []any) {
	args := []any{}
	where := []string{"s.recalled_at IS NULL"}
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if strings.TrimSpace(siteID) != "" {
		where = append(where, "s.site_id = "+next(siteID)+"::uuid")
	}
	switch cleanDeptFilter(deptID) {
	case "none":
		where = append(where, `NOT EXISTS (
			SELECT 1 FROM deals od
			JOIN users ou ON ou.id = od.owner_id
			WHERE od.id = s.deal_id AND ou.department_id IS NOT NULL
		)`)
	case "":
	default:
		where = append(where, `EXISTS (
			SELECT 1 FROM deals od
			JOIN users ou ON ou.id = od.owner_id
			WHERE od.id = s.deal_id AND ou.department_id = `+next(cleanDeptFilter(deptID))+`::uuid
		)`)
	}
	if q != "" {
		where = append(where, "s.full_name ILIKE "+next(likePattern(q)))
	}
	if position != "" {
		where = append(where, "s.position = "+next(position))
	}
	switch when {
	case "today":
		where = append(where, "s.arrival_date = (timezone('Europe/Moscow', now()))::date")
	case "week":
		where = append(where, "s.arrival_date >= (timezone('Europe/Moscow', now()))::date AND s.arrival_date < (timezone('Europe/Moscow', now()))::date + 7")
	case "past":
		where = append(where, "s.arrival_date < (timezone('Europe/Moscow', now()))::date")
	case "range":
		if fromDate, ok := parseISODate(from); ok {
			where = append(where, "s.arrival_date >= "+next(fromDate.Format("2006-01-02"))+"::date")
		}
		if toDate, ok := parseISODate(to); ok {
			where = append(where, "s.arrival_date <= "+next(toDate.Format("2006-01-02"))+"::date")
		}
	}
	switch arrived {
	case "yes":
		where = append(where, "s.arrived_at IS NOT NULL")
	case "no":
		where = append(where, "s.arrived_at IS NULL")
	}
	return strings.Join(where, " AND "), args
}

func customerPeoplePage(raw string) int {
	page, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || page < 1 {
		return 1
	}
	if page > 10000 {
		return 10000
	}
	return page
}

func (a *App) listCustomerPeople(w http.ResponseWriter, r *http.Request) {
	if !isCustomerRole(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "недоступно"})
		return
	}
	a.ensureCustomerSchema(r.Context())
	_, _, siteID := a.customerSiteOf(r.Context(), actorID(r))
	if siteID == "" {
		writeJSON(w, 403, map[string]string{"error": "кабинет не привязан"})
		return
	}
	a.writeCustomerPeopleList(w, r, siteID, false)
}

func (a *App) listAdminArrivals(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "недоступно"})
		return
	}
	a.ensureCustomerSchema(r.Context())
	code := strings.TrimSpace(r.URL.Query().Get("site"))
	siteID := ""
	if code != "" {
		if _, ok := customerSiteByCode(code); !ok {
			writeJSON(w, 400, map[string]string{"error": "неизвестный город"})
			return
		}
		if err := a.db.QueryRow(r.Context(), `SELECT id::text FROM customer_sites WHERE code=$1`, code).Scan(&siteID); err != nil || siteID == "" {
			writeJSON(w, 400, map[string]string{"error": "город не найден"})
			return
		}
	}
	a.writeCustomerPeopleList(w, r, siteID, true)
}

func (a *App) writeCustomerPeopleList(w http.ResponseWriter, r *http.Request, siteID string, withSites bool) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) > 80 {
		q = string([]rune(q)[:80])
	}
	position := strings.TrimSpace(r.URL.Query().Get("position"))
	if len([]rune(position)) > 120 {
		position = string([]rune(position)[:120])
	}
	when := strings.TrimSpace(r.URL.Query().Get("when"))
	arrived := strings.TrimSpace(r.URL.Query().Get("arrived"))
	if arrived != "yes" && arrived != "no" {
		arrived = ""
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	deptID := ""
	if withSites {
		deptID = cleanDeptFilter(r.URL.Query().Get("dept"))
	}
	page := customerPeoplePage(r.URL.Query().Get("page"))
	listWhere, listArgs := customerPeopleFilter(siteID, deptID, q, position, when, from, to, arrived)
	whenWhere, whenArgs := customerPeopleFilter(siteID, deptID, q, position, "", "", "", arrived)
	arrivedWhere, arrivedArgs := customerPeopleFilter(siteID, deptID, q, position, when, from, to, "")

	var total, documents int
	_ = a.db.QueryRow(r.Context(), `
		SELECT count(*)::int,
		       coalesce(sum((SELECT count(*) FROM customer_submission_files f WHERE f.submission_id=s.id)), 0)::int
		FROM customer_submissions s
		WHERE `+listWhere, listArgs...).Scan(&total, &documents)

	var todayN, weekN, pastN, allN int
	_ = a.db.QueryRow(r.Context(), `
		SELECT
		  count(*) FILTER (WHERE s.arrival_date = (timezone('Europe/Moscow', now()))::date)::int,
		  count(*) FILTER (WHERE s.arrival_date >= (timezone('Europe/Moscow', now()))::date
		                    AND s.arrival_date < (timezone('Europe/Moscow', now()))::date + 7)::int,
		  count(*) FILTER (WHERE s.arrival_date < (timezone('Europe/Moscow', now()))::date)::int,
		  count(*)::int
		FROM customer_submissions s
		WHERE `+whenWhere, whenArgs...).Scan(&todayN, &weekN, &pastN, &allN)

	var arrivedAll, arrivedYes, arrivedNo int
	_ = a.db.QueryRow(r.Context(), `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE s.arrived_at IS NOT NULL)::int,
		       count(*) FILTER (WHERE s.arrived_at IS NULL)::int
		FROM customer_submissions s
		WHERE `+arrivedWhere, arrivedArgs...).Scan(&arrivedAll, &arrivedYes, &arrivedNo)

	pageArgs := append(append([]any{}, listArgs...), customerPeoplePageSize, (page-1)*customerPeoplePageSize)
	phoneSelect := "'' AS phone,"
	phoneJoin := ""
	if withSites {
		phoneSelect = "coalesce(nullif(btrim(c.phone), ''), nullif(btrim(lc.phone), ''), '') AS phone,"
		phoneJoin = `
		LEFT JOIN deals d ON d.id = s.deal_id
		LEFT JOIN contacts c ON c.id = d.contact_id
		LEFT JOIN leads ld ON ld.id = d.lead_id
		LEFT JOIN contacts lc ON lc.id = ld.contact_id`
	}
	rows := a.queryMaps(r.Context(), `
		SELECT s.id::text, s.full_name,
		       cs.code AS site_code, cs.name AS site_name,
		       to_char(s.birth_date, 'YYYY-MM-DD') AS birth_date,
		       s.passport_number,
		       `+phoneSelect+`
		       to_char(s.arrival_date, 'YYYY-MM-DD') AS arrival_date,
		       s.position, s.sent_at,
		       (s.arrival_date = (timezone('Europe/Moscow', now()))::date) AS arrives_today,
		       to_char(timezone('Europe/Moscow', s.arrived_at), 'YYYY-MM-DD HH24:MI') AS arrived_at,
		       coalesce((
		         SELECT json_agg(json_build_object(
		           'id', f.id::text,
		           'filename', f.filename,
		           'mime', coalesce(f.mime,''),
		           'size_bytes', f.size_bytes
		         ) ORDER BY f.filename)
		         FROM customer_submission_files f
		         WHERE f.submission_id=s.id
		       ), '[]'::json) AS files
		FROM customer_submissions s
		JOIN customer_sites cs ON cs.id = s.site_id`+phoneJoin+`
		WHERE `+listWhere+`
		ORDER BY (s.arrived_at IS NOT NULL),
		         CASE WHEN s.arrived_at IS NULL THEN s.arrival_date END ASC NULLS LAST,
		         CASE WHEN s.arrived_at IS NOT NULL THEN s.arrival_date END DESC NULLS LAST,
		         s.full_name ASC
		LIMIT $`+strconv.Itoa(len(pageArgs)-1)+` OFFSET $`+strconv.Itoa(len(pageArgs)), pageArgs...)
	for _, row := range rows {
		row["files"] = decodeCustomerFiles(row["files"])
		if !withSites {
			delete(row, "phone")
		}
	}
	out := map[string]any{
		"items":          rows,
		"total":          total,
		"documents":      documents,
		"page":           page,
		"page_size":      customerPeoplePageSize,
		"when_counts":    map[string]int{"today": todayN, "week": weekN, "past": pastN, "all": allN},
		"arrived_counts": map[string]int{"all": arrivedAll, "yes": arrivedYes, "no": arrivedNo},
	}
	if withSites {
		siteWhere, siteArgs := customerPeopleFilter("", deptID, q, position, when, from, to, arrived)
		deptWhere, deptArgs := customerPeopleFilter(siteID, "", q, position, when, from, to, arrived)
		deptRows := a.queryMaps(r.Context(), `
			SELECT coalesce(dep.id::text, 'none') AS id,
			       coalesce(nullif(dep.name, ''), 'Без отдела') AS name,
			       count(s.id)::int AS count
			FROM customer_submissions s
			LEFT JOIN deals d ON d.id = s.deal_id
			LEFT JOIN users u ON u.id = d.owner_id
			LEFT JOIN departments dep ON dep.id = u.department_id
			WHERE `+deptWhere+`
			GROUP BY dep.id, dep.name
			HAVING count(s.id) > 0
			ORDER BY count(s.id) DESC, name`, deptArgs...)
		out["dept_counts"] = deptRows
		siteRows := a.queryMaps(r.Context(), `
			SELECT cs.code, cs.name, count(s.id)::int AS count
			FROM customer_sites cs
			LEFT JOIN customer_submissions s ON s.site_id = cs.id AND `+siteWhere+`
			GROUP BY cs.code, cs.name
			ORDER BY CASE cs.code
				WHEN 'msk_sao' THEN 1
				WHEN 'novgorod' THEN 2
				WHEN 'saransk' THEN 3
				WHEN 'smolensk' THEN 4
				ELSE 9 END`, siteArgs...)
		out["site_counts"] = siteRows
	}
	writeJSON(w, 200, out)
}

func decodeCustomerFiles(raw any) []any {
	switch v := raw.(type) {
	case nil:
		return []any{}
	case []any:
		if v == nil {
			return []any{}
		}
		return v
	case string:
		return unmarshalCustomerFiles(v)
	case []byte:
		return unmarshalCustomerFiles(string(v))
	default:
		buf, err := json.Marshal(v)
		if err != nil {
			return []any{}
		}
		return unmarshalCustomerFiles(string(buf))
	}
}

func unmarshalCustomerFiles(text string) []any {
	text = strings.TrimSpace(text)
	if text == "" || text == "<nil>" || text == "[]" || text == "null" {
		return []any{}
	}
	var files []any
	if err := json.Unmarshal([]byte(text), &files); err != nil || files == nil {
		return []any{}
	}
	return files
}

func (a *App) listCustomerPositions(w http.ResponseWriter, r *http.Request) {
	if !isCustomerRole(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "недоступно"})
		return
	}
	a.ensureCustomerSchema(r.Context())
	_, _, siteID := a.customerSiteOf(r.Context(), actorID(r))
	if siteID == "" {
		writeJSON(w, 403, map[string]string{"error": "кабинет не привязан"})
		return
	}
	rows := a.queryMaps(r.Context(), `
		SELECT DISTINCT position
		FROM customer_submissions
		WHERE site_id=$1::uuid AND recalled_at IS NULL AND coalesce(position,'') <> ''
		ORDER BY position`, siteID)
	writeJSON(w, 200, map[string]any{"items": rows})
}

func (a *App) getCustomerPerson(w http.ResponseWriter, r *http.Request) {
	if !isCustomerRole(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "недоступно"})
		return
	}
	a.ensureCustomerSchema(r.Context())
	_, _, siteID := a.customerSiteOf(r.Context(), actorID(r))
	if siteID == "" {
		writeJSON(w, 403, map[string]string{"error": "кабинет не привязан"})
		return
	}
	id := chi.URLParam(r, "id")
	rows := a.queryMaps(r.Context(), `
		SELECT s.id::text, s.full_name,
		       to_char(s.birth_date, 'YYYY-MM-DD') AS birth_date,
		       s.passport_number,
		       to_char(s.arrival_date, 'YYYY-MM-DD') AS arrival_date,
		       s.position, s.sent_at,
		       (s.arrival_date = (timezone('Europe/Moscow', now()))::date) AS arrives_today,
		       to_char(timezone('Europe/Moscow', s.arrived_at), 'YYYY-MM-DD HH24:MI') AS arrived_at
		FROM customer_submissions s
		WHERE s.id=$1::uuid AND s.site_id=$2::uuid AND s.recalled_at IS NULL`, id, siteID)
	if len(rows) == 0 {
		writeJSON(w, 404, map[string]string{"error": "не найдено"})
		return
	}
	files := a.queryMaps(r.Context(), `
		SELECT id::text, filename, coalesce(mime,'') AS mime, size_bytes, coalesce(kind,'') AS kind
		FROM customer_submission_files
		WHERE submission_id=$1::uuid
		ORDER BY filename`, id)
	person := rows[0]
	person["files"] = files
	writeJSON(w, 200, person)
}

func (a *App) putCustomerPersonArrived(w http.ResponseWriter, r *http.Request) {
	if !isCustomerRole(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "недоступно"})
		return
	}
	a.ensureCustomerSchema(r.Context())
	_, _, siteID := a.customerSiteOf(r.Context(), actorID(r))
	if siteID == "" {
		writeJSON(w, 403, map[string]string{"error": "кабинет не привязан"})
		return
	}
	var in struct {
		Arrived bool `json:"arrived"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	var dealID, arrivedAt string
	err := a.db.QueryRow(r.Context(), `
		UPDATE customer_submissions
		SET arrived_at = CASE WHEN $3 THEN coalesce(arrived_at, now()) ELSE NULL END,
		    arrived_by = CASE WHEN $3 THEN coalesce(arrived_by, nullif($4,'')::uuid) ELSE NULL END
		WHERE id=$1::uuid AND site_id=$2::uuid AND recalled_at IS NULL
		RETURNING deal_id::text, coalesce(to_char(timezone('Europe/Moscow', arrived_at), 'YYYY-MM-DD HH24:MI'), '')`,
		chi.URLParam(r, "id"), siteID, in.Arrived, actorID(r)).Scan(&dealID, &arrivedAt)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "не найдено"})
		return
	}
	action := "customer_arrival_unmarked"
	if in.Arrived {
		action = "customer_arrival_marked"
	}
	a.logDealLeadAct(r.Context(), dealID, actorID(r), action, map[string]any{"submission": chi.URLParam(r, "id")})
	var out any
	if arrivedAt != "" {
		out = arrivedAt
	}
	writeJSON(w, 200, map[string]any{"ok": true, "arrived_at": out})
}

func (a *App) putAdminArrival(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "недоступно"})
		return
	}
	a.ensureCustomerSchema(r.Context())
	var in struct {
		Arrived bool `json:"arrived"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	var dealID, arrivedAt string
	err := a.db.QueryRow(r.Context(), `
		UPDATE customer_submissions
		SET arrived_at = CASE WHEN $2 THEN coalesce(arrived_at, now()) ELSE NULL END,
		    arrived_by = CASE WHEN $2 THEN coalesce(arrived_by, nullif($3,'')::uuid) ELSE NULL END
		WHERE id=$1::uuid AND recalled_at IS NULL
		RETURNING deal_id::text, coalesce(to_char(timezone('Europe/Moscow', arrived_at), 'YYYY-MM-DD HH24:MI'), '')`,
		chi.URLParam(r, "id"), in.Arrived, actorID(r)).Scan(&dealID, &arrivedAt)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "не найдено"})
		return
	}
	action := "customer_arrival_unmarked"
	if in.Arrived {
		action = "customer_arrival_marked"
	}
	a.logDealLeadAct(r.Context(), dealID, actorID(r), action, map[string]any{
		"submission": chi.URLParam(r, "id"),
		"by":         "admin",
	})
	var out any
	if arrivedAt != "" {
		out = arrivedAt
	}
	writeJSON(w, 200, map[string]any{"ok": true, "arrived_at": out})
}

func (a *App) getAdminArrivalFile(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	a.ensureCustomerSchema(r.Context())
	var key, filename, mime string
	err := a.db.QueryRow(r.Context(), `
		SELECT f.storage_key, f.filename, coalesce(f.mime,'')
		FROM customer_submission_files f
		JOIN customer_submissions s ON s.id=f.submission_id
		WHERE f.id=$1::uuid AND s.id=$2::uuid AND s.recalled_at IS NULL`,
		chi.URLParam(r, "fileId"), chi.URLParam(r, "id")).Scan(&key, &filename, &mime)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	a.writeCustomerFile(w, r, key, filename, mime)
}

func (a *App) getCustomerPersonFile(w http.ResponseWriter, r *http.Request) {
	if !isCustomerRole(roleName(r)) {
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	a.ensureCustomerSchema(r.Context())
	_, _, siteID := a.customerSiteOf(r.Context(), actorID(r))
	if siteID == "" {
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	var key, filename, mime string
	err := a.db.QueryRow(r.Context(), `
		SELECT f.storage_key, f.filename, coalesce(f.mime,'')
		FROM customer_submission_files f
		JOIN customer_submissions s ON s.id=f.submission_id
		WHERE f.id=$1::uuid AND s.id=$2::uuid AND s.site_id=$3::uuid AND s.recalled_at IS NULL`,
		chi.URLParam(r, "fileId"), chi.URLParam(r, "id"), siteID).Scan(&key, &filename, &mime)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	a.writeCustomerFile(w, r, key, filename, mime)
}

func (a *App) writeCustomerFile(w http.ResponseWriter, r *http.Request, key, filename, mime string) {
	abs, ok := customerFileAbs(key)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	disp := "attachment"
	if r.URL.Query().Get("inline") == "1" {
		disp = "inline"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", disp+`; filename="`+strings.ReplaceAll(filename, `"`, "")+`"`)
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

func customerFileAbs(key string) (string, bool) {
	root, err := filepath.Abs(customerFilesRoot())
	if err != nil {
		return "", false
	}
	abs, err := filepath.Abs(filepath.Join(customerFilesRoot(), filepath.FromSlash(key)))
	if err != nil || !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return "", false
	}
	return abs, true
}
