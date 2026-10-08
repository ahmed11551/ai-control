package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forgecrm/server/internal/config"
	"github.com/go-chi/chi/v5"
)

func TestCanSeeTicketSensitive(t *testing.T) {
	if !canSeeTicketSensitive("logist") || !canSeeTicketSensitive("admin") {
		t.Fatal("logist/admin must see wagon/seat")
	}
	if canSeeTicketSensitive("manager") || canSeeTicketSensitive("sales") {
		t.Fatal("manager must not see sensitive ticket fields")
	}
}

func TestCanDownloadProtectedDocs(t *testing.T) {
	if !canDownloadProtectedDocs("logist") || !canDownloadProtectedDocs("admin") {
		t.Fatal("admin/logist must download protected docs")
	}
	if canDownloadProtectedDocs("rop") {
		t.Fatal("ROP must not download passport/docs")
	}
	if canDownloadProtectedDocs("manager") || canDownloadProtectedDocs("sales") {
		t.Fatal("manager must not download passport/docs")
	}
}

func TestLogistChatDocWanted(t *testing.T) {
	if !logistChatDocWanted("image", "image/jpeg", "a.jpg") {
		t.Fatal("photo must be included")
	}
	if !logistChatDocWanted("document", "application/pdf", "pass.pdf") {
		t.Fatal("pdf must be included")
	}
	if !logistChatDocWanted("", "image/png", "scan.png") {
		t.Fatal("png without kind must be included")
	}
	if logistChatDocWanted("voice", "audio/ogg", "voice.ogg") || logistChatDocWanted("ptt", "", "") {
		t.Fatal("voice must be skipped")
	}
	if logistChatDocWanted("sticker", "image/webp", "sticker.webp") {
		t.Fatal("sticker must be skipped")
	}
	if logistChatDocWanted("video", "video/mp4", "clip.mp4") {
		t.Fatal("video must be skipped")
	}
}

func TestTicketFileKindSensitive(t *testing.T) {
	if !ticketFileKindSensitive("doc") || !ticketFileKindSensitive("passport") {
		t.Fatal("doc/passport must be sensitive")
	}
	if ticketFileKindSensitive("ticket") {
		t.Fatal("ticket kind uses separate ACL from passport")
	}
}

func TestCanSeeLogistTicketFiles(t *testing.T) {
	if canSeeLogistTicketFiles("manager") || canSeeLogistTicketFiles("sales") || canSeeLogistTicketFiles("rop") {
		t.Fatal("manager/rop must not see logist files")
	}
	if !canSeeLogistTicketFiles("logist") || !canSeeLogistTicketFiles("admin") {
		t.Fatal("logist/admin must see ticket files")
	}
}

// TestManagerNeverSeesLogistUploads — билет/фото/паспорт от логиста менеджеру недоступны.
func TestManagerNeverSeesLogistUploads(t *testing.T) {
	logistUploads := []map[string]any{
		{"kind": "ticket", "filename": "bilet.pdf"},
		{"kind": "ticket", "filename": "photo.jpg"},
		{"kind": "doc", "filename": "passport.png"},
		{"kind": "passport", "filename": "scan.jpeg"},
	}

	t.Run("list_empty_for_manager", func(t *testing.T) {
		for _, role := range []string{"manager", "sales", "Manager", " SALES ", "rop", "ROP"} {
			got := filterTicketFilesForRole(logistUploads, role)
			if len(got) != 0 {
				t.Fatalf("%q must get empty files list, got %#v", role, got)
			}
		}
	})

	t.Run("list_full_for_logist_admin", func(t *testing.T) {
		for _, role := range []string{"logist", "night_logist", "admin"} {
			got := filterTicketFilesForRole(logistUploads, role)
			if len(got) != len(logistUploads) {
				t.Fatalf("%s must see all %d files, got %d", role, len(logistUploads), len(got))
			}
		}
	})

	t.Run("matrix_kind_x_role", func(t *testing.T) {
		type cell struct {
			role, kind  string
			wantVisible bool
		}
		matrix := []cell{
			{"manager", "ticket", false},
			{"manager", "doc", false},
			{"manager", "passport", false},
			{"sales", "ticket", false},
			{"sales", "doc", false},
			{"rop", "ticket", false},
			{"rop", "doc", false},
			{"rop", "passport", false},
			{"logist", "ticket", true},
			{"logist", "doc", true},
			{"night_logist", "ticket", true},
			{"night_logist", "doc", true},
			{"admin", "ticket", true},
			{"admin", "passport", true},
		}
		for _, c := range matrix {
			files := []map[string]any{{"kind": c.kind, "filename": "x"}}
			got := filterTicketFilesForRole(files, c.role)
			visible := len(got) == 1
			if visible != c.wantVisible {
				t.Fatalf("%s+%s visible=%v want %v", c.role, c.kind, visible, c.wantVisible)
			}
		}
	})
}

func TestTransportLabel(t *testing.T) {
	if transportLabel("rail") != "жд" {
		t.Fatal(transportLabel("rail"))
	}
	if transportLabel("air") != "авиа" {
		t.Fatal(transportLabel("air"))
	}
}

func TestTicketFileSignStable(t *testing.T) {
	a := &App{cfg: config.Config{JWTSecret: "test-secret"}}
	s1 := a.ticketFileSign("d1", "f1", 100)
	s2 := a.ticketFileSign("d1", "f1", 100)
	if s1 == "" || s1 != s2 {
		t.Fatalf("sign unstable: %q %q", s1, s2)
	}
	if a.ticketFileSign("d1", "f1", 101) == s1 {
		t.Fatal("exp must change signature")
	}
}

func TestTicketFilePublicURLRequiresBase(t *testing.T) {
	a := &App{cfg: config.Config{JWTSecret: "x"}}
	if a.ticketFilePublicURL("d", "f") != "" {
		t.Fatal("empty PublicBaseURL must yield empty url")
	}
	a.cfg.PublicBaseURL = "https://crm.example"
	u := a.ticketFilePublicURL("d", "f")
	if !strings.Contains(u, "/api/public/ticket-file?") || !strings.Contains(u, "d=d") {
		t.Fatalf("bad url %q", u)
	}
}

func TestEscortStagesHaveTicketRequest(t *testing.T) {
	found := false
	for _, s := range escortStages {
		if s.key == "ticket_request" && s.short == "Заявка" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing ticket_request stage")
	}
	keys := map[string]bool{}
	for _, s := range escortStages {
		keys[s.key] = true
	}
	for _, want := range []string{"ticket_request", "ticket", "boarding", "transit", "arrived", "housing", "vvk", "region", "success"} {
		if !keys[want] {
			t.Fatalf("missing stage %s", want)
		}
	}
}

func TestAdaptationHasBiletStage(t *testing.T) {
	found := false
	docsBefore := false
	for _, s := range adaptationStages {
		if s.key == "docs" {
			docsBefore = true
		}
		if s.key == "bilet" && s.short == "Билет" {
			if !docsBefore {
				t.Fatal("Билет must be after Документы")
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing adaptation bilet stage")
	}
}

func TestAdaptationHasPostBiletBitrixStages(t *testing.T) {
	order := []string{"bilet", "departure", "selection", "prescription", "close"}
	idx := map[string]int{}
	for i, s := range adaptationStages {
		idx[s.key] = i
	}
	for _, k := range order {
		if _, ok := idx[k]; !ok {
			t.Fatalf("missing adaptation stage %s", k)
		}
	}
	for i := 1; i < len(order); i++ {
		if idx[order[i]] <= idx[order[i-1]] {
			t.Fatalf("%s must follow %s", order[i], order[i-1])
		}
	}
	for _, s := range adaptationStages {
		if s.key == "close" && !s.won {
			t.Fatal("close must be won")
		}
		if s.key == "departure" && s.short != "Выезд" {
			t.Fatalf("departure short=%q", s.short)
		}
		if s.key == "selection" && s.short != "Пункт отбора" {
			t.Fatalf("selection short=%q", s.short)
		}
		if s.key == "prescription" && s.short != "Предписание" {
			t.Fatalf("prescription short=%q", s.short)
		}
	}
}

func TestRemapHireBiletAsLogistRequest(t *testing.T) {
	items := []map[string]any{
		{"logistics_needed": true, "stage_key": hirePipelineKey + ":bilet", "stage_id": "hire-bilet", "pipeline_id": "hire"},
		{"logistics_needed": true, "stage_key": hirePipelineKey + ":docs", "stage_id": "hire-docs", "pipeline_id": "hire"},
		{"logistics_needed": false, "stage_key": hirePipelineKey + ":bilet", "stage_id": "hire-bilet", "pipeline_id": "hire"},
		{"logistics_needed": true, "stage_key": escortPipelineKey + ":ticket", "stage_id": "esc-ticket", "pipeline_id": "esc"},
		{"logistics_needed": false, "stage_key": hirePipelineKey + ":departure", "stage_id": "hire-dep", "pipeline_id": "hire"},
	}
	a := &App{}
	// без БД escortStageID пустой — remap no-op
	a.remapHireBiletAsLogistRequest(context.Background(), items, true)
	if items[0]["stage_id"] != "hire-bilet" {
		t.Fatalf("without DB must not remap: %#v", items[0])
	}
	// с заглушкой escort id
	remapHireLogisticsNeededForTest(items, "esc-pipe", "esc-req")
	if items[0]["stage_id"] != "esc-req" || items[1]["stage_id"] != "esc-req" {
		t.Fatalf("hire+logistics_needed must remap to Заявка: %#v %#v", items[0], items[1])
	}
	if items[2]["stage_id"] != "hire-bilet" {
		t.Fatalf("bilet without logistics_needed must stay: %#v", items[2])
	}
	if items[3]["stage_id"] != "esc-ticket" {
		t.Fatalf("already escort must stay: %#v", items[3])
	}
	if items[4]["stage_id"] != "esc-req" {
		t.Fatalf("departure without queue flag must remap to Заявка: %#v", items[4])
	}
}

func TestDealMirrorsToManagerBilet(t *testing.T) {
	if !dealMirrorsToManagerBilet(escortPipelineKey+":ticket", escortPipelineKey, true) {
		t.Fatal("purchased ticket must stay on manager bilet")
	}
	if !dealMirrorsToManagerBilet(escortPipelineKey+":ticket_request", escortPipelineKey, false) {
		t.Fatal("logist request column must stay on manager bilet")
	}
	if !dealMirrorsToManagerBilet(hirePipelineKey+":docs", hirePipelineKey, true) {
		t.Fatal("hire queue sent to logist must stay on manager bilet")
	}
	if dealMirrorsToManagerBilet(hirePipelineKey+":departure", hirePipelineKey, false) {
		t.Fatal("ordinary later stage must not be pulled into bilet")
	}
	if dealMirrorsToManagerBilet(hirePipelineKey+":departure", hirePipelineKey, true) ||
		dealMirrorsToManagerBilet(hirePipelineKey+":selection", hirePipelineKey, true) {
		t.Fatal("post-bilet stage must stay on its own column")
	}
	if !isHirePostBiletKey(hirePipelineKey+":selection") || isHirePostBiletKey(hirePipelineKey+":bilet") {
		t.Fatal("post-bilet keys")
	}
	if dealMirrorsToManagerBilet(escortPipelineKey+":transit", escortPipelineKey, true) {
		t.Fatal("later escort stage stays off the manager board")
	}
}

// remapHireLogisticsNeededForTest — логика remap без БД (для unit-теста).
func remapHireLogisticsNeededForTest(items []map[string]any, escortPipe, escortReq string) {
	for _, it := range items {
		key := fmt.Sprint(it["stage_key"])
		if !logistPaintsHireStageAsRequest(key, truthy(it["logistics_needed"]), true) {
			continue
		}
		it["pipeline_id"] = escortPipe
		it["stage_id"] = escortReq
		it["stage"] = "Заявка на билет"
		it["logist_view"] = "ticket_request"
	}
}

func TestPostBiletNeedsSameTicketAndShowsAsRequest(t *testing.T) {
	for _, key := range []string{"departure", "selection", "prescription", "close"} {
		full := hirePipelineKey + ":" + key
		if !stageNeedsFilledBiletKey(full) {
			t.Fatalf("%s must require a filled ticket", key)
		}
		if !logistPaintsHireStageAsRequest(full, false, true) {
			t.Fatalf("%s must show as logist request without the queue flag", key)
		}
		if logistPaintsHireStageAsRequest(full, false, false) {
			t.Fatalf("%s must stay on the manager board when handoff paint is off", key)
		}
	}
	if !stageNeedsFilledBiletKey(hirePipelineKey+":bilet") || !stageNeedsFilledBiletKey(oldLeadsPipelineKey+":bilet") {
		t.Fatal("bilet itself still requires a ticket")
	}
	if stageNeedsFilledBiletKey(hirePipelineKey+":docs") || stageNeedsFilledBiletKey(escortPipelineKey+":ticket") {
		t.Fatal("other stages must not take the ticket gate")
	}
	if logistPaintsHireStageAsRequest(hirePipelineKey+":bilet", false, true) {
		t.Fatal("empty bilet is not a logist request")
	}
	if !logistPaintsHireStageAsRequest(hirePipelineKey+":departure", true, false) {
		t.Fatal("queued departure still paints as request")
	}
	if logistPaintsHireStageAsRequest(escortPipelineKey+":boarding", true, true) {
		t.Fatal("logist column must stay")
	}
	if !strings.Contains(sqlDealOnManagerHandoffStage("d"), hirePipelineKey+":departure") {
		t.Fatal("handoff sql")
	}
	if strings.Contains(sqlDealOnManagerHandoffStage("d"), hirePipelineKey+":bilet") {
		t.Fatal("bilet must stay off the logist board")
	}
	link := sqlDealOnLogistLinkStage("d")
	for _, key := range logistLinkStageKeys() {
		if !strings.Contains(link, key) {
			t.Fatalf("link open missing %s", key)
		}
	}
	if !strings.Contains(link, hirePipelineKey+":bilet") {
		t.Fatal("bilet must open by link")
	}
	ready := sqlDealReadyForLogistRequest("d")
	if !strings.Contains(ready, "depart_at") || !strings.Contains(ready, "deal_ticket_files") {
		t.Fatal("request needs a filled ticket or a file")
	}
	handoff := sqlLogistHandoffOnBoard("d")
	if !strings.Contains(handoff, hirePipelineKey+":close") || !strings.Contains(handoff, hirePipelineKey+":departure") {
		t.Fatal("handoff board")
	}
	closeAt := strings.Index(handoff, hirePipelineKey+":close")
	if closeAt < 0 || strings.Contains(handoff[closeAt:], "deal_ticket_files") {
		t.Fatal("close stays off the logist board without a filled ticket")
	}
	reqCol := sqlLogistRequestColumn("d", "$1")
	if !strings.Contains(reqCol, "deal_ticket_files") || !strings.Contains(reqCol, "depart_at") {
		t.Fatal("request column must require a file or a filled ticket")
	}
	if !strings.Contains(sqlLogistEscortExceptEmptyRequest("d"), escortPipelineKey+":ticket_request") {
		t.Fatal("empty request column")
	}
}

func TestManagerTicketFileDownloadDeniedBeforeDB(t *testing.T) {
	a := &App{}
	req := httptest.NewRequest(http.MethodGet, "/deals/d/ticket/files/f", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxRole, "manager"))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "00000000-0000-0000-0000-000000000001")
	rctx.URLParams.Add("fileId", "00000000-0000-0000-0000-000000000002")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	a.getDealTicketFile(rr, req)
	if rr.Code != 404 {
		t.Fatalf("manager download must be 404, code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSendTicketToCandidateForbiddenForManager(t *testing.T) {
	a := &App{}
	req := httptest.NewRequest(http.MethodPost, "/deals/x/ticket/send-to-candidate", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxRole, "manager"))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "00000000-0000-0000-0000-000000000099")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	a.sendDealTicketToCandidate(rr, req)
	if rr.Code != 403 {
		t.Fatalf("manager send must be 403, code=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "логист") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestSalesSameAsManagerForTicketFiles(t *testing.T) {
	a := &App{}
	for _, role := range []string{"sales", "manager", "rop"} {
		req := httptest.NewRequest(http.MethodGet, "/deals/d/ticket/files/f", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxRole, role))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "00000000-0000-0000-0000-000000000001")
		rctx.URLParams.Add("fileId", "00000000-0000-0000-0000-000000000002")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		a.getDealTicketFile(rr, req)
		if rr.Code != 404 {
			t.Fatalf("%s download code=%d", role, rr.Code)
		}
	}
}

func TestRopCannotSendTicketToCandidate(t *testing.T) {
	a := &App{}
	req := httptest.NewRequest(http.MethodPost, "/deals/x/ticket/send-to-candidate", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxRole, "rop"))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "00000000-0000-0000-0000-000000000099")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	a.sendDealTicketToCandidate(rr, req)
	if rr.Code != 403 {
		t.Fatalf("rop send must be 403, code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestTowerViewerSeesAllTicketFiles(t *testing.T) {
	files := []map[string]any{
		{"kind": "ticket", "filename": "bilet.pdf"},
		{"kind": "doc", "filename": "passport.png"},
		{"kind": "passport", "filename": "scan.jpeg"},
	}
	if got := filterTicketFilesForViewer(files, "manager", false); len(got) != 0 {
		t.Fatalf("other manager must see nothing, got %#v", got)
	}
	if got := filterTicketFilesForViewer(files, "rop", false); len(got) != 0 {
		t.Fatalf("other rop must see nothing, got %#v", got)
	}
	for _, role := range []string{"manager", "sales", "rop"} {
		got := filterTicketFilesForViewer(files, role, true)
		if len(got) != len(files) {
			t.Fatalf("tower %s must see all files, got %d", role, len(got))
		}
	}
	if seeClientPhonesAllowed("manager", false) || seeClientPhonesAllowed("rop", false) {
		t.Fatal("phones stay hidden when the department flag is off")
	}
}
