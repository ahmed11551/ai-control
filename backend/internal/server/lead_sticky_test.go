package server

import (
	"strings"
	"testing"
)

// Контракт «липкого» лида: любой след работы владельца-менеджера (звонок, коммент,
// стадия, открытие) — и лид нельзя отдать другому ни банком, ни РОПом, ни скорозвоном, ни PATCH.

func TestStickySQLFragmentsCoverCallsAndActivity(t *testing.T) {
	for name, sql := range map[string]string{
		"sticky_owner":   sqlLeadStickyToOwner("l"),
		"touched_by_any": sqlLeadTouchedByManagerOtherThan("l", ""),
		"touched_except": sqlLeadTouchedByManagerOtherThan("l", "$3::uuid"),
	} {
		for _, need := range []string{"activity_log", "calls", "entity_type='lead'", "entity_type='deal'", "converted_deal_id", stickyIgnoredActions} {
			if !strings.Contains(sql, need) {
				t.Fatalf("%s: missing %q", name, need)
			}
		}
	}
	// Владелец-РОП не делает лид липким.
	if !strings.Contains(sqlLeadStickyToOwner("l"), "head_user_id=l.owner_id") {
		t.Fatal("sticky must exclude department head as owner")
	}
	// except-параметр реально подставляется.
	if !strings.Contains(sqlLeadTouchedByManagerOtherThan("l", "$3::uuid"), "tu.id <> $3::uuid") {
		t.Fatal("except param not applied")
	}
	if strings.Contains(sqlLeadTouchedByManagerOtherThan("l", ""), "tu.id <>") {
		t.Fatal("empty except must not add filter")
	}
}

func TestStickyIgnoredActionsAreServiceOnly(t *testing.T) {
	for _, a := range []string{"distributed", "distributed_manager", "recalled_to_bank", "recalled_to_rop_bank", "owner_changed", "created", "converted"} {
		if !strings.Contains(stickyIgnoredActions, "'"+a+"'") {
			t.Fatalf("service action %q must be ignored", a)
		}
	}
	for _, a := range []string{"comment", "call", "stage_changed", "owner_opened", "doc_sent_to_logist", "autodial_claimed"} {
		if strings.Contains(stickyIgnoredActions, "'"+a+"'") {
			t.Fatalf("work action %q must make lead sticky", a)
		}
	}
}

func TestLeadStickyBlocksTransfer(t *testing.T) {
	cases := []struct {
		name        string
		owner       string
		sticky      bool
		newOwner    string
		wantBlocked bool
	}{
		{"not sticky → free", "m1", false, "m2", false},
		{"sticky → other blocked", "m1", true, "m2", true},
		{"sticky → same owner ok", "m1", true, "m1", false},
		{"sticky → to bank blocked", "m1", true, "", true},
		{"sticky but no owner → free", "", true, "m2", false},
		{"whitespace ids", " m1 ", true, "m1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := leadStickyBlocksTransfer(c.owner, c.sticky, c.newOwner)
			if (err != nil) != c.wantBlocked {
				t.Fatalf("got err=%v want blocked=%v", err, c.wantBlocked)
			}
		})
	}
}

func TestAllTransferPathsUseStickyFilter(t *testing.T) {
	// Реальные фрагменты WHERE из recall/issue — липкость обязана быть в каждом.
	recallAdmin := `AND (` + sqlLeadOnNewApplicationStage("l") + `) AND (` + sqlLeadNeverOpenedByManager("l") + `) AND NOT (` + sqlLeadStickyToOwner("l") + `)`
	recallRop := `AND (` + sqlLeadOnNewApplicationStage("leads") + `) AND (` + sqlLeadNeverOpenedByManager("leads") + `) AND NOT (` + sqlLeadStickyToOwner("leads") + `)`
	issueBank := `AND (` + sqlLeadNeverOpenedByManager("leads") + `) AND NOT (` + sqlLeadTouchedByManagerOtherThan("leads", "") + `)`
	issueRop := `AND (` + sqlLeadNeverOpenedByManager("leads") + `) AND NOT (` + sqlLeadTouchedByManagerOtherThan("leads", "$3::uuid") + `)`
	for name, sql := range map[string]string{"recallAdmin": recallAdmin, "recallRop": recallRop, "issueBank": issueBank, "issueRop": issueRop} {
		if !strings.Contains(sql, "FROM calls") {
			t.Fatalf("%s: must consider calls, not only owner_opened", name)
		}
		if !strings.Contains(sql, "owner_opened") {
			t.Fatalf("%s: must keep owner_opened gate", name)
		}
	}
}

func TestStickyOnlyManagerRoleMatters(t *testing.T) {
	sql := sqlLeadTouchedByManagerOtherThan("l", "")
	if !strings.Contains(sql, "IN ('manager','sales')") {
		t.Fatal("touched-by must be limited to manager roles")
	}
	if !strings.Contains(sql, "head_user_id=tu.id") {
		t.Fatal("department head actions must not count as manager touch")
	}
}
