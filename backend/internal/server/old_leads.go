package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// oldLeadImportMaxRows — сколько строк с именем, телефоном или ссылкой берёт одна заливка.
const oldLeadImportMaxRows = 30000

// temaDepartmentName — отделение, которому видна вкладка «Старые лиды».
const temaDepartmentName = "отдел: тема"

type oldLeadRow struct {
	Line       int
	FormName   string
	FormID     string
	Campaign   string
	Group      string
	Ad         string
	When       time.Time
	WhenRaw    string
	Name       string
	Phone      string
	TG         string
	WantsPhone string
}

func foldDeptName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "ё", "е")
	return s
}

func isTemaDepartmentName(name string) bool {
	return foldDeptName(name) == temaDepartmentName
}

func (a *App) userInTemaDepartment(ctx context.Context, userID string) bool {
	userID = strings.TrimSpace(userID)
	if a == nil || a.db == nil || userID == "" {
		return false
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1
		FROM users u
		LEFT JOIN departments own ON own.id=u.department_id
		WHERE u.id=$1::uuid
		  AND (
		    lower(replace(coalesce(own.name,''), 'ё', 'е')) = $2
		    OR EXISTS (
		      SELECT 1 FROM departments hd
		      WHERE hd.head_user_id=u.id
		        AND lower(replace(hd.name, 'ё', 'е')) = $2
		    )
		  )`, userID, temaDepartmentName).Scan(&n)
	return err == nil
}

func (a *App) userCanSeeOldLeads(r *http.Request) bool {
	if isAdmin(r) {
		return true
	}
	return a.userInTemaDepartment(r.Context(), actorID(r))
}

// temaSeesSleepingDeal — РОП отдела видит весь банк. Менеджер видит карточку только после выдачи ему.
func (a *App) temaSeesSleepingDeal(ctx context.Context, s crmScope, dealID string) bool {
	if !a.userInTemaDepartment(ctx, s.UserID) {
		return false
	}
	if !s.IsManager {
		return true
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM deals WHERE id=$1::uuid AND owner_id=$2::uuid`, dealID, s.UserID).Scan(&n)
	return err == nil && n == 1
}

// sqlDealNotOldLeads — карточка не из спящих. Флаг sleeping остаётся,
// даже если логист потом перевёл её в сопровождение.
func sqlDealNotOldLeads(dealAlias string) string {
	if dealAlias == "" {
		dealAlias = "d"
	}
	return `NOT EXISTS (
		SELECT 1 FROM pipelines op
		WHERE op.id=` + dealAlias + `.pipeline_id
		  AND coalesce(op.bitrix_id,'')='` + oldLeadsPipelineKey + `'
	) AND NOT coalesce(` + dealAlias + `.sleeping,false)`
}

// sqlDealOnLogisticsSourcePipeline — очередь логиста: найм и спящие, не чужие воронки.
func sqlDealOnLogisticsSourcePipeline(dealAlias string) string {
	if dealAlias == "" {
		dealAlias = "d"
	}
	return `EXISTS (
		SELECT 1 FROM pipelines lqp
		WHERE lqp.id=` + dealAlias + `.pipeline_id
		  AND coalesce(lqp.bitrix_id,'') IN ('` + hirePipelineKey + `','` + oldLeadsPipelineKey + `')
	)`
}

// logisticsQueueStageKey — стадия, с которой карточка рисуется у логиста как «Заявка».
func logisticsQueueStageKey(key string) bool {
	return strings.HasPrefix(key, hirePipelineKey) || strings.HasPrefix(key, oldLeadsPipelineKey)
}

// hirePostBiletKeys — колонки адаптации после «Билет». Их ставит менеджер,
// не забирая карточку из воронки логиста.
func hirePostBiletKeys() []string {
	return []string{
		hirePipelineKey + ":departure",
		hirePipelineKey + ":selection",
		hirePipelineKey + ":prescription",
		hirePipelineKey + ":close",
	}
}

func isHirePostBiletKey(key string) bool {
	for _, k := range hirePostBiletKeys() {
		if key == k {
			return true
		}
	}
	return false
}

// stageNeedsFilledBiletKey — в «Билет» и в колонки после него нельзя войти с пустым билетом.
func stageNeedsFilledBiletKey(key string) bool {
	return key == hirePipelineKey+":bilet" || key == oldLeadsPipelineKey+":bilet" || isHirePostBiletKey(key)
}

// logistPaintsHireStageAsRequest — карточка найма рисуется у логиста как «Заявка».
// Колонки после «Билет» видны и без флага очереди: менеджер мог поставить их раньше.
// handoffWithoutFlag включает этот показ. Своя стадия логиста в сопровождении не подменяется.
func logistPaintsHireStageAsRequest(key string, logisticsNeeded, handoffWithoutFlag bool) bool {
	if strings.HasPrefix(key, escortPipelineKey) {
		return false
	}
	if handoffWithoutFlag && isHirePostBiletKey(key) {
		return true
	}
	if !logisticsNeeded {
		return false
	}
	return key == "" || logisticsQueueStageKey(key)
}

// logistLinkStageKeys — стадии, которые логист и ночной логист открывают по ссылке.
// На канбан и в «Новые заявки» из-за этого карточка не попадает.
func logistLinkStageKeys() []string {
	return append([]string{hirePipelineKey + ":bilet"}, hirePostBiletKeys()...)
}

func sqlInQuoted(keys []string) string {
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, "'"+k+"'")
	}
	return strings.Join(parts, ",")
}

// sqlDealOnLogistLinkStage — «Билет», «Выезд», «Пункт отбора», «Предписание», «Завершить».
func sqlDealOnLogistLinkStage(dealAlias string) string {
	if dealAlias == "" {
		dealAlias = "d"
	}
	return `EXISTS (
		SELECT 1 FROM pipeline_stages lls
		WHERE lls.id=` + dealAlias + `.stage_id
		  AND coalesce(lls.bitrix_id,'') IN (` + sqlInQuoted(logistLinkStageKeys()) + `)
	)`
}

// sqlLogistReadyExists — у сделки есть заполненный билет или файл.
// Проверка идёт по сделке, которая уже отобрана стадией, а не по всей таблице.
func sqlLogistReadyExists(dealAlias string) string {
	if dealAlias == "" {
		dealAlias = "d"
	}
	return `(
		EXISTS (
			SELECT 1 FROM deal_tickets ft
			WHERE ft.deal_id=` + dealAlias + `.id
			  AND coalesce(ft.from_city,'') <> ''
			  AND coalesce(ft.to_city,'') <> ''
			  AND coalesce(ft.transport,'') <> ''
			  AND ft.depart_at IS NOT NULL
		)
		OR EXISTS (
			SELECT 1 FROM deal_ticket_files df
			WHERE df.deal_id=` + dealAlias + `.id
		)
	)`
}

// sqlDealReadyForLogistRequest — в «Заявку» попадает карточка с документом или с заполненным билетом.
func sqlDealReadyForLogistRequest(dealAlias string) string {
	return sqlLogistReadyExists(dealAlias)
}

// sqlLogistQueueAndHandoffIDs — очередь и колонки менеджера после «Билет», уже с билетом или файлом.
// «Завершить» входит только с заполненным билетом. Каждая ветка начинается со стадии или флага.
func sqlLogistQueueAndHandoffIDs() string {
	return `
		SELECT qq.id FROM deals qq
		WHERE coalesce(qq.logistics_needed,false)=true
		  AND EXISTS (
			SELECT 1 FROM pipelines qp
			WHERE qp.id=qq.pipeline_id
			  AND coalesce(qp.bitrix_id,'') IN ('` + hirePipelineKey + `','` + oldLeadsPipelineKey + `')
		  )
		  AND ` + sqlLogistReadyExists("qq") + `
		UNION
		SELECT hq.id FROM deals hq
		WHERE hq.stage_id IN (
			SELECT hs.id FROM pipeline_stages hs
			WHERE coalesce(hs.bitrix_id,'') IN (` + sqlInQuoted([]string{
		hirePipelineKey + ":departure",
		hirePipelineKey + ":selection",
		hirePipelineKey + ":prescription",
	}) + `)
		)
		  AND ` + sqlLogistReadyExists("hq") + `
		UNION
		SELECT cq.id FROM deals cq
		JOIN deal_tickets cft ON cft.deal_id=cq.id
		WHERE coalesce(cft.from_city,'') <> ''
		  AND coalesce(cft.to_city,'') <> ''
		  AND coalesce(cft.transport,'') <> ''
		  AND cft.depart_at IS NOT NULL
		  AND cq.stage_id IN (
			SELECT cs.id FROM pipeline_stages cs
			WHERE coalesce(cs.bitrix_id,'')='` + hirePipelineKey + `:close'
		  )`
}

// sqlLogistEscortExceptEmptyRequest — колонки сопровождения без пустой «Заявки».
// Пустая «Заявка» — нет ни файла, ни заполненного билета.
func sqlLogistEscortExceptEmptyRequest(dealAlias string) string {
	if dealAlias == "" {
		dealAlias = "d"
	}
	return `(
		NOT EXISTS (
			SELECT 1 FROM pipeline_stages ers
			WHERE ers.id=` + dealAlias + `.stage_id
			  AND coalesce(ers.bitrix_id,'')='` + escortPipelineKey + `:ticket_request'
		)
		OR ` + sqlDealReadyForLogistRequest(dealAlias) + `
	)`
}

// sqlLogistHandoffOnBoard — «Выезд», «Пункт отбора» и «Предписание» в «Заявке»,
// если есть документ или заполненный билет. «Завершить» — только с заполненным билетом.
func sqlLogistHandoffOnBoard(dealAlias string) string {
	if dealAlias == "" {
		dealAlias = "d"
	}
	return dealAlias + `.id IN (
		SELECT hq.id FROM deals hq
		WHERE hq.stage_id IN (
			SELECT hs.id FROM pipeline_stages hs
			WHERE coalesce(hs.bitrix_id,'') IN (` + sqlInQuoted([]string{
		hirePipelineKey + ":departure",
		hirePipelineKey + ":selection",
		hirePipelineKey + ":prescription",
	}) + `)
		)
		  AND ` + sqlLogistReadyExists("hq") + `
		UNION
		SELECT cq.id FROM deals cq
		JOIN deal_tickets cft ON cft.deal_id=cq.id
		WHERE coalesce(cft.from_city,'') <> ''
		  AND coalesce(cft.to_city,'') <> ''
		  AND coalesce(cft.transport,'') <> ''
		  AND cft.depart_at IS NOT NULL
		  AND cq.stage_id IN (
			SELECT cs.id FROM pipeline_stages cs
			WHERE coalesce(cs.bitrix_id,'')='` + hirePipelineKey + `:close'
		  )
	)`
}

// sqlLogistBoardVisible — доска логиста: сопровождение, очередь и колонки после «Билет».
// В «Заявку» не входят карточки без документа и без заполненного билета.
func sqlLogistBoardVisible(dealAlias string) string {
	if dealAlias == "" {
		dealAlias = "d"
	}
	return `(
		(
			` + dealAlias + `.pipeline_id IN (
				SELECT ep.id FROM pipelines ep
				WHERE coalesce(ep.bitrix_id,'')='` + escortPipelineKey + `'
			)
			AND ` + sqlLogistEscortExceptEmptyRequest(dealAlias) + `
		)
		OR ` + dealAlias + `.id IN (` + sqlLogistQueueAndHandoffIDs() + `)
	)`
}

// sqlLogistRequestColumn — колонка «Заявка»: реальная стадия, очередь и передача менеджера.
func sqlLogistRequestColumn(dealAlias string, stageParam string) string {
	if dealAlias == "" {
		dealAlias = "d"
	}
	return dealAlias + `.id IN (
		SELECT rq.id FROM deals rq
		WHERE rq.stage_id = ` + stageParam + `::uuid
		  AND ` + sqlLogistReadyExists("rq") + `
		UNION
		` + sqlLogistQueueAndHandoffIDs() + `
	)`
}

// sqlDealOnManagerHandoffStage — реальная стадия «Выезд» / «Пункт отбора» / «Предписание» / «Завершить».
func sqlDealOnManagerHandoffStage(dealAlias string) string {
	if dealAlias == "" {
		dealAlias = "d"
	}
	return `EXISTS (
		SELECT 1 FROM pipeline_stages mhs
		WHERE mhs.id=` + dealAlias + `.stage_id
		  AND coalesce(mhs.bitrix_id,'') IN (` + sqlHirePostBiletIn() + `)
	)`
}

func sqlHirePostBiletIn() string {
	return sqlInQuoted(hirePostBiletKeys())
}

// sleepingHandoffAllowed — спящая карточка может остаться у себя или уйти логисту в сопровождение.
// В адаптацию она не переезжает. Обычная карточка в спящие не переезжает.
func sleepingHandoffAllowed(fromSleeping bool, targetKey string) bool {
	if targetKey == oldLeadsPipelineKey {
		return fromSleeping
	}
	if !fromSleeping {
		return true
	}
	return targetKey == escortPipelineKey || targetKey == oldLeadsPipelineKey
}

// sqlLeadNotOldLeads — лид не связан со сделкой в «Старых лидах».
func sqlLeadNotOldLeads(leadAlias string) string {
	if leadAlias == "" {
		leadAlias = "l"
	}
	return `NOT EXISTS (
		SELECT 1 FROM deals od
		LEFT JOIN pipelines op ON op.id=od.pipeline_id
		WHERE (coalesce(op.bitrix_id,'')='` + oldLeadsPipelineKey + `' OR coalesce(od.sleeping,false))
		  AND (od.lead_id=` + leadAlias + `.id OR od.id=` + leadAlias + `.converted_deal_id)
	) AND coalesce(` + leadAlias + `.source,'') <> 'old_excel'`
}

func (a *App) isOldLeadsPipeline(ctx context.Context, pipeID string) bool {
	if a == nil || a.db == nil || strings.TrimSpace(pipeID) == "" {
		return false
	}
	var key string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(bitrix_id,'') FROM pipelines WHERE id=$1::uuid`, pipeID).Scan(&key)
	return key == oldLeadsPipelineKey
}

func (a *App) dealOnOldLeadsPipeline(ctx context.Context, dealID string) bool {
	if a == nil || a.db == nil || strings.TrimSpace(dealID) == "" {
		return false
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM deals d
		JOIN pipelines p ON p.id=d.pipeline_id
		WHERE d.id=$1::uuid AND coalesce(p.bitrix_id,'')=$2
		LIMIT 1`, dealID, oldLeadsPipelineKey).Scan(&n)
	return err == nil
}

func (a *App) dealIsSleeping(ctx context.Context, dealID string) bool {
	if a == nil || a.db == nil || strings.TrimSpace(dealID) == "" {
		return false
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM deals WHERE id=$1::uuid AND sleeping LIMIT 1`, dealID).Scan(&n)
	return err == nil
}

func (a *App) dealLogisticsNeeded(ctx context.Context, dealID string) bool {
	if a == nil || a.db == nil || strings.TrimSpace(dealID) == "" {
		return false
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM deals WHERE id=$1::uuid AND logistics_needed LIMIT 1`, dealID).Scan(&n)
	return err == nil
}

func (a *App) oldLeadsStageID(ctx context.Context, stageKey string) (pipeID, stageID string) {
	if a == nil || a.db == nil {
		return "", ""
	}
	a.ensureRecruitFunnels(ctx)
	_ = a.db.QueryRow(ctx, `
		SELECT p.id::text, s.id::text
		FROM pipelines p
		JOIN pipeline_stages s ON s.pipeline_id=p.id
		WHERE p.bitrix_id=$1 AND s.bitrix_id=$2`,
		oldLeadsPipelineKey, oldLeadsPipelineKey+":"+stageKey).Scan(&pipeID, &stageID)
	return pipeID, stageID
}

func (a *App) isOldLeadsBiletStage(ctx context.Context, stageID string) bool {
	stageID = strings.TrimSpace(stageID)
	if a == nil || a.db == nil || stageID == "" {
		return false
	}
	var key string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(bitrix_id,'') FROM pipeline_stages WHERE id=$1::uuid`, stageID).Scan(&key)
	return key == oldLeadsPipelineKey+":bilet"
}

func oldLeadColumn(header string) string {
	h := foldDeptName(header)
	switch {
	case h == "form_name" || h == "форма":
		return "form_name"
	case h == "form_id":
		return "form_id"
	case strings.Contains(h, "кампан"):
		return "campaign"
	case strings.Contains(h, "групп"):
		return "group"
	case strings.Contains(h, "объявлен"):
		return "ad"
	case strings.Contains(h, "время"):
		return "time"
	case h == "имя" || h == "фио" || h == "name":
		return "name"
	case strings.Contains(h, "хочешь") || strings.Contains(h, "хочет"):
		return "wants_phone"
	case strings.Contains(h, "телефон") || h == "phone":
		return "phone"
	case strings.Contains(h, "тг") || strings.Contains(h, "telegram"):
		return "tg"
	default:
		return ""
	}
}

func normOldLeadPhone(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) == 11 && d[0] == '8' {
		d = "7" + d[1:]
	}
	if len(d) == 10 {
		d = "7" + d
	}
	if len(d) == 11 && d[0] == '7' {
		return d
	}
	return ""
}

func parseOldLeadTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{
		"2006-01-02",
		"02.01.2006",
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"02.01.2006 15:04",
		"02.01.2006 15:04:05",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t, true
		}
	}
	if n, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64); err == nil && n > 20000 && n < 80000 {
		// Excel serial: дни с 1899-12-30.
		base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.Local)
		return base.Add(time.Duration(n * float64(24*time.Hour))), true
	}
	return time.Time{}, false
}

func parseOldLeadTable(rows [][]string) (out []oldLeadRow, skippedEmpty int) {
	headerAt := -1
	cols := map[string]int{}
	for i, row := range rows {
		found := map[string]int{}
		for j, cell := range row {
			key := oldLeadColumn(cell)
			if key != "" {
				if _, ok := found[key]; !ok {
					found[key] = j
				}
			}
		}
		if _, ok := found["name"]; ok {
			headerAt = i
			cols = found
			break
		}
		if _, ok := found["phone"]; ok {
			headerAt = i
			cols = found
			break
		}
	}
	if headerAt < 0 {
		return nil, 0
	}
	cell := func(row []string, key string) string {
		j, ok := cols[key]
		if !ok || j >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[j])
	}
	for i := headerAt + 1; i < len(rows); i++ {
		row := rows[i]
		empty := true
		for _, c := range row {
			if strings.TrimSpace(c) != "" {
				empty = false
				break
			}
		}
		if empty {
			continue
		}
		item := oldLeadRow{
			Line:       i + 1,
			FormName:   cell(row, "form_name"),
			FormID:     cell(row, "form_id"),
			Campaign:   cell(row, "campaign"),
			Group:      cell(row, "group"),
			Ad:         cell(row, "ad"),
			WhenRaw:    cell(row, "time"),
			Name:       cell(row, "name"),
			Phone:      normOldLeadPhone(cell(row, "phone")),
			TG:         cell(row, "tg"),
			WantsPhone: cell(row, "wants_phone"),
		}
		if t, ok := parseOldLeadTime(item.WhenRaw); ok {
			item.When = t
		}
		if item.Name == "" && item.Phone == "" && item.TG == "" {
			skippedEmpty++
			continue
		}
		out = append(out, item)
	}
	return out, skippedEmpty
}

func oldLeadTitle(row oldLeadRow) string {
	form := strings.TrimSpace(row.FormName)
	name := strings.TrimSpace(row.Name)
	switch {
	case form != "" && name != "":
		return form + " " + name
	case form != "":
		return form
	case name != "":
		return name
	case row.Phone != "":
		return row.Phone
	default:
		return "Старый лид"
	}
}

func oldLeadContactName(row oldLeadRow) string {
	return strings.TrimSpace(row.Name)
}

func readOldLeadTable(filename string, r io.Reader) ([][]string, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 20<<20))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("пустой файл")
	}
	name := strings.ToLower(filename)
	if strings.HasSuffix(name, ".csv") || (!strings.HasSuffix(name, ".xlsx") && utf8.Valid(raw) && !bytes.HasPrefix(raw, []byte("PK"))) {
		return readOldLeadCSV(raw)
	}
	if bytes.HasPrefix(raw, []byte("PK")) || strings.HasSuffix(name, ".xlsx") {
		return readOldLeadXLSX(raw)
	}
	return nil, fmt.Errorf("нужен файл Excel (.xlsx) или CSV")
}

func readOldLeadCSV(raw []byte) ([][]string, error) {
	text := string(raw)
	if strings.HasPrefix(text, "\uFEFF") {
		text = strings.TrimPrefix(text, "\uFEFF")
	}
	cr := csv.NewReader(strings.NewReader(text))
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	sample := text
	if i := strings.IndexAny(sample, "\n"); i > 0 {
		sample = sample[:i]
	}
	if strings.Count(sample, ";") > strings.Count(sample, ",") {
		cr.Comma = ';'
	}
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("не разобрать CSV: %w", err)
	}
	return rows, nil
}

func readOldLeadXLSX(raw []byte) ([][]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("не разобрать Excel: %w", err)
	}
	var shared, sheet []byte
	for _, f := range zr.File {
		name := strings.TrimPrefix(f.Name, "/")
		switch {
		case strings.EqualFold(name, "xl/sharedStrings.xml"):
			shared, err = readZipFile(f)
		case strings.EqualFold(name, "xl/worksheets/sheet1.xml"):
			sheet, err = readZipFile(f)
		}
		if err != nil {
			return nil, err
		}
	}
	if sheet == nil {
		return nil, fmt.Errorf("в файле нет первого листа")
	}
	stringsTable, err := xlsxSharedStrings(shared)
	if err != nil {
		return nil, err
	}
	return xlsxSheetRows(sheet, stringsTable)
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 20<<20))
}

func xlsxSharedStrings(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var out []string
	var cur strings.Builder
	inSI := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("не прочитать строки Excel: %w", err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			if el.Name.Local == "si" {
				inSI = true
				cur.Reset()
			}
		case xml.EndElement:
			if el.Name.Local == "si" {
				out = append(out, cur.String())
				inSI = false
			}
		case xml.CharData:
			if inSI {
				cur.Write(el)
			}
		}
	}
	return out, nil
}

func xlsxCol(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A'+1)
	}
	if n == 0 {
		return 0
	}
	return n - 1
}

func xlsxSheetRows(raw []byte, shared []string) ([][]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var rows [][]string
	var row []string
	var col int
	var kind string
	var val strings.Builder
	inCell := false
	inV := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("не прочитать лист Excel: %w", err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "row":
				row = nil
			case "c":
				inCell = true
				kind = ""
				col = len(row)
				val.Reset()
				for _, a := range el.Attr {
					if a.Name.Local == "r" {
						col = xlsxCol(strings.ToUpper(a.Value))
					}
					if a.Name.Local == "t" {
						kind = a.Value
					}
				}
			case "v", "t":
				if inCell {
					inV = true
				}
			}
		case xml.CharData:
			if inV {
				val.Write(el)
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "v", "t":
				inV = false
			case "c":
				text := strings.TrimSpace(val.String())
				if kind == "s" {
					if i, err := strconv.Atoi(text); err == nil && i >= 0 && i < len(shared) {
						text = shared[i]
					}
				}
				for len(row) <= col {
					row = append(row, "")
				}
				row[col] = text
				inCell = false
			case "row":
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

type oldLeadInsert struct {
	Name  string
	Phone string
	Title string
	When  time.Time
}

func oldLeadPhoneTails(ctx context.Context, tx pgx.Tx) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT right(regexp_replace(coalesce(c.phone,''), '[^0-9]', '', 'g'), 10)
		FROM contacts c
		JOIN deals d ON d.contact_id=c.id
		JOIN pipelines p ON p.id=d.pipeline_id
		WHERE coalesce(p.bitrix_id,'')=$1
		  AND right(regexp_replace(coalesce(c.phone,''), '[^0-9]', '', 'g'), 10) <> ''`, oldLeadsPipelineKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var tail string
		if err := rows.Scan(&tail); err != nil {
			return nil, err
		}
		out[tail] = true
	}
	return out, rows.Err()
}

func writeOldLeadBatch(ctx context.Context, tx pgx.Tx, pipe, stage string, items []oldLeadInsert) error {
	if len(items) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE old_lead_import (
			contact_id uuid NOT NULL DEFAULT gen_random_uuid(),
			lead_id uuid NOT NULL DEFAULT gen_random_uuid(),
			deal_id uuid NOT NULL DEFAULT gen_random_uuid(),
			full_name text NOT NULL,
			phone text NOT NULL,
			title text NOT NULL,
			created_at timestamptz NOT NULL
		) ON COMMIT DROP`); err != nil {
		return err
	}
	if _, err := tx.CopyFrom(ctx,
		pgx.Identifier{"old_lead_import"},
		[]string{"full_name", "phone", "title", "created_at"},
		pgx.CopyFromSlice(len(items), func(i int) ([]any, error) {
			return []any{items[i].Name, items[i].Phone, items[i].Title, items[i].When}, nil
		}),
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO contacts (id, full_name, phone, source)
		SELECT contact_id, full_name, NULLIF(phone, ''), 'old_excel'
		FROM old_lead_import`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO leads (id, title, source, status, contact_id, city, card_note, created_at)
		SELECT lead_id, title, 'old_excel', 'new', contact_id, '', '', created_at
		FROM old_lead_import`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO deals (id, title, contact_id, lead_id, pipeline_id, stage_id, amount, created_at, sleeping)
		SELECT deal_id, title, contact_id, lead_id, $1::uuid, $2::uuid, 0, created_at, true
		FROM old_lead_import`, pipe, stage); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE leads l
		SET converted_deal_id=i.deal_id
		FROM old_lead_import i
		WHERE l.id=i.lead_id`)
	return err
}

func (a *App) importOldLeads(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "заливать спящие лиды может только администратор"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось прочитать файл"})
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "приложите файл Excel"})
		return
	}
	defer file.Close()
	table, err := readOldLeadTable(hdr.Filename, file)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	rows, skippedEmpty := parseOldLeadTable(table)
	if len(rows) == 0 && skippedEmpty == 0 {
		writeJSON(w, 400, map[string]string{"error": "в файле нет строк с именем, телефоном или ссылкой"})
		return
	}
	if len(rows) > oldLeadImportMaxRows {
		writeJSON(w, 400, map[string]string{"error": "в файле больше 30000 строк — разбейте его"})
		return
	}
	ctx := r.Context()
	a.ensureRecruitFunnels(ctx)
	a.ensureLogisticsSchema(ctx)
	var pipe, stage string
	err = a.db.QueryRow(ctx, `
		SELECT p.id::text, s.id::text
		FROM pipelines p
		JOIN pipeline_stages s ON s.pipeline_id=p.id
		WHERE p.bitrix_id=$1 AND s.is_won=false AND s.is_lost=false
		ORDER BY s.sort_order LIMIT 1`, oldLeadsPipelineKey).Scan(&pipe, &stage)
	if err != nil || pipe == "" || stage == "" {
		writeJSON(w, 500, map[string]string{"error": "воронка «Спящие лиды» не готова"})
		return
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось начать заливку"})
		return
	}
	defer tx.Rollback(ctx)

	existing, err := oldLeadPhoneTails(ctx, tx)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось проверить уже залитые номера: " + err.Error()})
		return
	}
	seen := map[string]bool{}
	batch := make([]oldLeadInsert, 0, len(rows))
	skippedDup := 0
	for _, row := range rows {
		if row.Phone != "" {
			tail := row.Phone
			if len(tail) > 10 {
				tail = tail[len(tail)-10:]
			}
			if seen[tail] || existing[tail] {
				seen[tail] = true
				skippedDup++
				continue
			}
			seen[tail] = true
		}
		when := row.When
		if when.IsZero() {
			when = time.Now()
		}
		batch = append(batch, oldLeadInsert{
			Name:  oldLeadContactName(row),
			Phone: row.Phone,
			Title: oldLeadTitle(row),
			When:  when,
		})
	}
	if err := writeOldLeadBatch(ctx, tx, pipe, stage, batch); err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось создать карточки: " + err.Error()})
		return
	}
	created := len(batch)
	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось сохранить заливку"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok":                true,
		"created":           created,
		"skipped_empty":     skippedEmpty,
		"skipped_duplicate": skippedDup,
	})
}

func (a *App) claimOldLead(w http.ResponseWriter, r *http.Request) {
	if isManager(r) || !a.userCanSeeOldLeads(r) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа к спящим лидам"})
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if !a.dealOnOldLeadsPipeline(ctx, id) {
		writeJSON(w, 404, map[string]string{"error": "карточка не из спящих лидов"})
		return
	}
	uid := actorID(r)
	tag, err := a.db.Exec(ctx, `
		UPDATE deals SET owner_id=$2::uuid, updated_at=now()
		WHERE id=$1::uuid AND owner_id IS NULL`, id, uid)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, 409, map[string]string{"error": "карточка уже у кого-то в работе"})
		return
	}
	_, _ = a.db.Exec(ctx, `
		UPDATE leads SET owner_id=$2::uuid, updated_at=now()
		WHERE id = (SELECT lead_id FROM deals WHERE id=$1::uuid)
		   OR converted_deal_id=$1::uuid`, id, uid)
	a.logDealLeadAct(ctx, id, uid, "claimed", map[string]any{"owner_id": uid})
	a.broadcastDealUpdated(ctx, id, map[string]any{"owner_id": uid})
	writeJSON(w, 200, map[string]any{"ok": true, "owner_id": uid})
}
