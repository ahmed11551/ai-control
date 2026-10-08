package server

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseOldLeadTable(t *testing.T) {
	rows := [][]string{
		{"form_name", "form_id", "ID Кампании", "ID Группы", "ID Объявления", "Время лида", "Имя", "Телефон", "Ссылка на ТГ", "Хочешь номер телефона указан"},
		{"Лиды - 001", "966069", "29153414", "15179450", "234361220", "2026-08-21", "Фандеев А", "+79538160556", "https://t.me/example", "Да"},
		{"Лиды - 002", "966069", "29157392", "15180001", "234367499", "2026-08-21", "максим", "79155302275", "", "Нет"},
		{"", "", "", "", "", "", "", "", "", ""},
		{"Лиды - 002", "966155", "", "", "", "2026-08-21", "", "", "", ""},
		{"", "", "", "", "", "2026-08-21", "", "", "https://t.me/oleg", "Нет"},
	}
	got, skipped := parseOldLeadTable(rows)
	if skipped != 1 {
		t.Fatalf("skipped empty = %d, want 1", skipped)
	}
	if len(got) != 3 {
		t.Fatalf("rows = %d, want 3", len(got))
	}
	if got[0].Name != "Фандеев А" || got[0].Phone != "79538160556" || got[0].Campaign != "29153414" {
		t.Fatalf("first row: %+v", got[0])
	}
	if got[0].TG == "" || got[0].WantsPhone != "Да" {
		t.Fatalf("tg/wants: %+v", got[0])
	}
	if got[0].When.Format("2006-01-02") != "2026-08-21" {
		t.Fatalf("when = %s", got[0].When)
	}
	if got[1].Phone != "79155302275" || got[1].Name != "максим" {
		t.Fatalf("second row: %+v", got[1])
	}
	if got[2].Phone != "" || got[2].TG != "https://t.me/oleg" {
		t.Fatalf("tg-only row: %+v", got[2])
	}
	if oldLeadTitle(got[0]) != "Лиды - 001 Фандеев А" || oldLeadContactName(got[0]) != "Фандеев А" {
		t.Fatalf("title/name = %q / %q", oldLeadTitle(got[0]), oldLeadContactName(got[0]))
	}
	if oldLeadTitle(got[1]) != "Лиды - 002 максим" {
		t.Fatalf("title = %s", oldLeadTitle(got[1]))
	}
	if oldLeadTitle(got[2]) != "Старый лид" || oldLeadContactName(got[2]) != "" {
		t.Fatalf("tg-only title/name = %q / %q", oldLeadTitle(got[2]), oldLeadContactName(got[2]))
	}
}

func TestNormOldLeadPhone(t *testing.T) {
	cases := map[string]string{
		"+7 (953) 816-05-56": "79538160556",
		"89538160556":        "79538160556",
		"9538160556":         "79538160556",
		"123":                "",
		"":                   "",
	}
	for in, want := range cases {
		if got := normOldLeadPhone(in); got != want {
			t.Fatalf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestIsTemaDepartmentName(t *testing.T) {
	if !isTemaDepartmentName("Отдел: Тёма") {
		t.Fatal("Отдел: Тёма")
	}
	if !isTemaDepartmentName("  отдел: тема ") {
		t.Fatal("folded")
	}
	if isTemaDepartmentName("Отдел Миши") || isTemaDepartmentName("Отдел Эдуарда") {
		t.Fatal("other department matched")
	}
}

func TestParseOldLeadTime(t *testing.T) {
	tm, ok := parseOldLeadTime("21.08.2026")
	if !ok || tm.Format("2006-01-02") != "2026-08-21" {
		t.Fatalf("dot date: %v %v", tm, ok)
	}
	if _, ok := parseOldLeadTime(""); ok {
		t.Fatal("empty must fail")
	}
}

func TestReadOldLeadXLSX(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"xl/sharedStrings.xml": `<sst><si><t>Имя</t></si><si><t>Телефон</t></si><si><t>Фандеев А</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData>` +
			`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>` +
			`<row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2"><v>79538160556</v></c></row>` +
			`</sheetData></worksheet>`,
	}
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err := readOldLeadXLSX(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	got, skipped := parseOldLeadTable(rows)
	if skipped != 0 || len(got) != 1 || got[0].Name != "Фандеев А" || got[0].Phone != "79538160556" {
		t.Fatalf("got %+v skipped %d rows %#v", got, skipped, rows)
	}
}

func TestTemaManagerSleepingNeedsOwner(t *testing.T) {
	a := &App{}
	ctx := context.Background()
	if a.temaSeesSleepingDeal(ctx, crmScope{UserID: "mgr", IsManager: true}, "deal") {
		t.Fatal("manager must not see the sleeping bank without a database owner check")
	}
	if a.temaSeesSleepingDeal(ctx, crmScope{UserID: "rop"}, "deal") {
		t.Fatal("rop outside tema must not see sleeping cards")
	}
}

func TestOldLeadsStayOffMainCRM(t *testing.T) {
	if oldLeadsPipelineKey == hirePipelineKey || oldLeadsPipelineKey == escortPipelineKey {
		t.Fatal("old leads must be a separate pipeline")
	}
	for _, sql := range []string{sqlDealNotOldLeads("d"), sqlLeadNotOldLeads("l")} {
		if !strings.Contains(sql, oldLeadsPipelineKey) {
			t.Fatalf("exclusion missing old pipeline: %s", sql)
		}
		if strings.Contains(sql, hirePipelineKey) {
			t.Fatalf("exclusion must not target the main funnel: %s", sql)
		}
	}
	dist := sqlLeadOnNewApplicationStage("l")
	if !strings.Contains(dist, hireNewApplicationStage) || strings.Contains(dist, oldLeadsPipelineKey) {
		t.Fatalf("distribution must stay on the main new-lead column: %s", dist)
	}
	sleeping := sqlUnownedSleepingLeads()
	if !strings.Contains(sleeping, oldLeadsPipelineKey) || !strings.Contains(sleeping, oldLeadsPipelineKey+":1") {
		t.Fatalf("sleeping pool must stay on its own first column: %s", sleeping)
	}
	if strings.Contains(sleeping, hireNewApplicationStage) || strings.Contains(sleeping, hirePipelineKey) {
		t.Fatalf("sleeping pool must not read new leads: %s", sleeping)
	}
	if strings.Contains(sqlUnownedNewApplicationLeads(), oldLeadsPipelineKey) {
		t.Fatal("new-lead drain must not read sleeping leads")
	}
	if !strings.Contains(sqlRopBankLeadCount(), oldLeadsPipelineKey) {
		t.Fatal("ROP new-lead queue must exclude sleeping leads")
	}
	if sleepingAutoSettingKey("dept") == teamAutoSettingKey("dept") {
		t.Fatal("sleeping auto switch must not reuse the new-lead switch")
	}
	if !strings.Contains(sqlDealNotOldLeads("d"), "sleeping") {
		t.Fatal("main board must keep excluding a sleeping card after it is handed to the logist")
	}
	if !logisticsQueueStageKey(oldLeadsPipelineKey+":bilet") || !logisticsQueueStageKey(hirePipelineKey+":bilet") {
		t.Fatal("bilet handoff")
	}
	if logisticsQueueStageKey(escortPipelineKey + ":ticket") {
		t.Fatal("escort stage is the logist's own column")
	}
	if oldLeadImportMaxRows != 30000 {
		t.Fatalf("one sleeping-lead file must accept 30000 rows, got %d", oldLeadImportMaxRows)
	}
	if !strings.Contains(sqlDealOnLogisticsSourcePipeline("d"), oldLeadsPipelineKey) || !strings.Contains(sqlDealOnLogisticsSourcePipeline("d"), hirePipelineKey) {
		t.Fatal("logist queue must see both funnels")
	}
	if sleepingHandoffAllowed(true, hirePipelineKey) || sleepingHandoffAllowed(false, oldLeadsPipelineKey) {
		t.Fatal("sleeping and adaptation must not switch funnels")
	}
	if !sleepingHandoffAllowed(true, escortPipelineKey) || !sleepingHandoffAllowed(true, oldLeadsPipelineKey) {
		t.Fatal("sleeping card may stay or go to the logist funnel")
	}
	if !sleepingHandoffAllowed(false, hirePipelineKey) || !sleepingHandoffAllowed(false, escortPipelineKey) {
		t.Fatal("ordinary card handoff changed")
	}

	req := httptest.NewRequest(http.MethodGet, "/deals", nil)
	ctx := context.WithValue(req.Context(), ctxRole, "admin")
	ctx = context.WithValue(ctx, ctxUserID, "admin-1")
	clause, _ := (&App{}).dealListWhere(req.WithContext(ctx))
	if !strings.Contains(clause, oldLeadsPipelineKey) {
		t.Fatalf("main board must exclude old leads: %s", clause)
	}

	req = httptest.NewRequest(http.MethodGet, "/leads", nil)
	ctx = context.WithValue(req.Context(), ctxRole, "manager")
	ctx = context.WithValue(ctx, ctxUserID, "mgr-1")
	// listLeads builds the filter inline; the same predicate is what the leads tab uses.
	if !strings.Contains(sqlLeadNotOldLeads("l"), "converted_deal_id") {
		t.Fatal("lead list exclusion must follow the linked deal")
	}
	_ = ctx
}

func TestReadOldLeadCSVSemicolon(t *testing.T) {
	raw := []byte("Имя;Телефон;Время лида\nОлег;+79834677378;2026-08-21\n")
	rows, err := readOldLeadCSV(raw)
	if err != nil {
		t.Fatal(err)
	}
	got, skipped := parseOldLeadTable(rows)
	if skipped != 0 || len(got) != 1 || got[0].Phone != "79834677378" {
		t.Fatalf("got %+v skipped %d", got, skipped)
	}
	if got[0].When.Before(time.Date(2026, 8, 21, 0, 0, 0, 0, time.Local)) {
		t.Fatal(got[0].When)
	}
}
