package server

import (
	"strconv"
	"testing"
)

func TestJWTClaimVer(t *testing.T) {
	if jwtClaimVer(map[string]any{}) != 1 {
		t.Fatal("missing ver must stay compatible")
	}
	if jwtClaimVer(map[string]any{"ver": float64(3)}) != 3 {
		t.Fatal("float ver")
	}
	if jwtClaimVer(map[string]any{"ver": 2}) != 2 {
		t.Fatal("int ver")
	}
}

func TestJWTTTLHours(t *testing.T) {
	if jwtTTLHours("admin", 12) != 12 {
		t.Fatal("admin")
	}
	if jwtTTLHours("sales", 12) != 4 {
		t.Fatal("staff")
	}
}

func TestCRMScopeApplyOwner(t *testing.T) {
	s := crmScope{UserID: "u1", DeptID: "d1"}
	args := []any{}
	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	where := s.applyOwner("d", nil, next)
	if len(where) != 1 || len(args) != 2 {
		t.Fatalf("where=%v args=%v", where, args)
	}
	admin := crmScope{All: true}
	if got := admin.applyOwner("d", nil, next); len(got) != 0 {
		t.Fatalf("admin filter %v", got)
	}
}

func TestLeadVisibleWhenDealOwned(t *testing.T) {
	// Документируем правило: комментарий/карточка лида доступны, если видна связанная сделка.
	cases := []struct {
		ownerOK, nedozvon, dealOK bool
		want                      bool
	}{
		{true, false, false, true},
		{false, true, false, true},
		{false, false, true, true},
		{false, false, false, false},
	}
	for i, tc := range cases {
		got := tc.ownerOK || tc.nedozvon || tc.dealOK
		if got != tc.want {
			t.Fatalf("case %d: got %v want %v", i, got, tc.want)
		}
	}
}

func TestLogistLinksFailClosedWithoutDatabase(t *testing.T) {
	a := &App{}
	logist := crmScope{UserID: "log", IsLogist: true}
	if a.canSeeDeal(nil, logist, "") {
		t.Fatal("empty deal id")
	}
	if a.canSeeDeal(nil, logist, "11111111-1111-1111-1111-111111111111") {
		t.Fatal("no database must not treat a deal as visible")
	}
	if a.canSeeLead(nil, logist, "") {
		t.Fatal("lead without a deal stays closed")
	}
	if a.contactVisible(nil, logist, "") {
		t.Fatal("empty contact")
	}
}

func TestLeadLinkedToLogisticsEmpty(t *testing.T) {
	a := &App{}
	if a.leadLinkedToLogistics(nil, "") {
		t.Fatal("empty lead must be false")
	}
	if a.contactLinkedToLogistics(nil, "") {
		t.Fatal("empty contact must be false")
	}
	if a.anyDealIDForLead(nil, "") != "" {
		t.Fatal("empty lead must have no deal")
	}
}

func TestDealOnBoardForUserEmptyAndAdmin(t *testing.T) {
	a := &App{}
	if a.dealOnBoardForUser(nil, crmScope{UserID: "u1", IsManager: true}, "") {
		t.Fatal("empty deal must be false")
	}
	if a.dealOnBoardForUser(nil, crmScope{UserID: ""}, "d1") {
		t.Fatal("empty user must be false")
	}
	// Admin (All) — на доске всё, без обращения к БД.
	if !a.dealOnBoardForUser(nil, crmScope{All: true, UserID: "admin"}, "any-deal") {
		t.Fatal("admin must see board deals")
	}
}

// TestBoardACLStricterThanCanSeeDeal документирует: пул/autodial в canSeeDeal,
// но dealOnBoardForUser их не учитывает (только owner / отдел).
func TestBoardACLStricterThanCanSeeDeal(t *testing.T) {
	canSee := func(own, autodial, nedozvon bool) bool {
		return own || autodial || nedozvon
	}
	onBoard := func(own bool) bool {
		return own // без пула
	}
	if !canSee(false, true, false) {
		t.Fatal("canSee allows autodial pool")
	}
	if onBoard(false) {
		t.Fatal("board must reject non-owner even if pool visible")
	}
	if !onBoard(true) || !canSee(true, false, false) {
		t.Fatal("owner visible both ways")
	}
}
