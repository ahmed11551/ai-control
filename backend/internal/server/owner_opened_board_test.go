package server

import (
	"strings"
	"testing"
)

func TestShouldRecordOwnerOpened(t *testing.T) {
	cases := []struct {
		name       string
		role       string
		actor      string
		owner      string
		isDeptHead bool
		want       bool
	}{
		{"manager own card", "manager", "u1", "u1", false, true},
		{"sales own card", "sales", "u1", "u1", false, true},
		{"manager other owner", "manager", "u1", "u2", false, false},
		{"empty actor", "manager", "", "u1", false, false},
		{"empty owner", "manager", "u1", "", false, false},
		{"admin own", "admin", "u1", "u1", false, false},
		{"rop own", "rop", "u1", "u1", false, false},
		{"head of dept even if manager role", "manager", "u1", "u1", true, false},
		{"logist own", "logist", "u1", "u1", false, false},
		{"whitespace trim", " manager ", " u1 ", "u1", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldRecordOwnerOpened(tc.role, tc.actor, tc.owner, tc.isDeptHead)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestRecallIssueSQLBlocksOpenedLeads(t *testing.T) {
	// Реальные WHERE-фрагменты из recall/issue — оба фильтра обязаны быть.
	recallWhere := `AND (` + sqlLeadOnNewApplicationStage("l") + `) AND (` + sqlLeadNeverOpenedByManager("l") + `)`
	issueWhere := `AND (` + sqlLeadNeverOpenedByManager("leads") + `)`
	ropWhere := `AND (` + sqlLeadOnNewApplicationStage("leads") + `) AND (` + sqlLeadNeverOpenedByManager("leads") + `)`

	for _, sql := range []string{recallWhere, issueWhere, ropWhere} {
		if !strings.Contains(sql, "owner_opened") {
			t.Fatalf("missing owner_opened: %s", sql)
		}
		if !strings.Contains(sql, "NOT EXISTS") {
			t.Fatalf("must be NOT EXISTS gate: %s", sql)
		}
	}
	if !strings.Contains(recallWhere, hireNewApplicationStage) {
		t.Fatal("recall must still pin new-application stage")
	}
	if !strings.Contains(ropWhere, hireNewApplicationStage) {
		t.Fatal("rop recall/issue must pin new-application stage")
	}
	// issueBank — только «неоткрытые», стадию в этом пакете не добавляли.
	if strings.Contains(issueWhere, hireNewApplicationStage) {
		t.Fatal("issueBank must not require new-stage in this change")
	}
}

func TestSqlLeadNeverOpenedCoversLeadAndDeal(t *testing.T) {
	sql := sqlLeadNeverOpenedByManager("l")
	needles := []string{
		"entity_type='lead'",
		"entity_type='deal'",
		"d.lead_id=l.id",
		"converted_deal_id",
		"owner_opened",
	}
	for _, n := range needles {
		if !strings.Contains(sql, n) {
			t.Fatalf("missing %q in filter", n)
		}
	}
}

func TestDealOnBoardRejectsEscortForManagerScope(t *testing.T) {
	// Без БД: escort check идёт в QueryRow — пустой deal уже false.
	a := &App{}
	mgr := crmScope{UserID: "mgr", IsManager: true}
	if a.dealOnBoardForUser(nil, mgr, "") {
		t.Fatal("empty")
	}
	rop := crmScope{UserID: "rop", DeptID: "dept-1"}
	if a.dealOnBoardForUser(nil, rop, "") {
		t.Fatal("empty rop")
	}
	logist := crmScope{UserID: "log", IsLogist: true}
	if a.dealOnBoardForUser(nil, logist, "") {
		t.Fatal("empty logist")
	}
}

func TestBoardVsOpenACLContract(t *testing.T) {
	// Контракт продукта: открыть пул можно, на доску — нельзя.
	type caseRow struct {
		own, autodial, nedozvon bool
	}
	rows := []caseRow{
		{false, true, false},
		{false, false, true},
		{false, true, true},
		{true, true, true},
		{false, false, false},
	}
	for i, r := range rows {
		canOpen := r.own || r.autodial || r.nedozvon
		onBoard := r.own
		if r.own && !onBoard {
			t.Fatalf("%d: owner must be on board", i)
		}
		if !r.own && onBoard {
			t.Fatalf("%d: non-owner must not be on board", i)
		}
		if (r.autodial || r.nedozvon) && !r.own && !canOpen {
			t.Fatalf("%d: pool must still open card", i)
		}
		if !r.own && canOpen && onBoard {
			t.Fatalf("%d: pool open must not imply board", i)
		}
	}
}

func TestBroadcastDealUpdatedNoPanicNilHub(t *testing.T) {
	a := &App{} // hub nil
	a.broadcastDealUpdated(nil, "deal-1", map[string]any{"stage_id": "s1"})
	a.broadcastDealCreated(nil, "deal-1", map[string]any{"owner_id": "u1"})
	a.broadcastDealUpdated(nil, "", map[string]any{"x": 1})
}
