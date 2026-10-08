package server

import (
	"context"
	"strings"
	"testing"
)

func TestIsMskTowerDepartmentName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"МСК БАШНЯ", true},
		{"МСК Башня", true},
		{"мск башня", true},
		{"Отдел МСК Башня", true},
		{"MSK Tower", true},
		{"Москва Башня", true},
		{"Отдел Ивана", false},
		{"Башня", false},
		{"МСК", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isMskTowerDepartmentName(tc.name); got != tc.want {
			t.Fatalf("%q: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestMskTowerRecallDeniedMsg(t *testing.T) {
	if !strings.Contains(mskTowerRecallDeniedMsg, "МСК Башня") {
		t.Fatalf("msg=%q", mskTowerRecallDeniedMsg)
	}
	if errMskTowerRecallDenied() == nil {
		t.Fatal("expected error")
	}
}

func TestTransferLeavesLockedDeptPure(t *testing.T) {
	// Без БД: пустой App не лочит (departmentLeadsLocked → false).
	a := &App{}
	if a.transferLeavesLockedDept(context.Background(), "owner", "") {
		t.Fatal("no db → not locked")
	}
}

func TestMskTowerFlagForCreate(t *testing.T) {
	cases := []struct {
		name string
		role string
		raw  any
		want bool
	}{
		{"rop true", "rop", true, true},
		{"ROP True string", "ROP", "true", true},
		{"rop 1", "rop", "1", true},
		{"rop yes", "rop", "yes", true},
		{"rop false default", "rop", false, false},
		{"rop missing", "rop", nil, false},
		{"rop empty", "rop", "", false},
		{"manager true ignored", "manager", true, false},
		{"sales true ignored", "sales", true, false},
		{"admin true ignored", "admin", true, false},
		{"logist true ignored", "logist", true, false},
		{"empty role", "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mskTowerFlagForCreate(tc.role, tc.raw)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestShouldDistributeMskTower(t *testing.T) {
	cases := []struct {
		name  string
		tower bool
		role  string
		want  bool
	}{
		{"rop tower on", true, "rop", true},
		{"ROP case", true, "ROP", true},
		{"rop tower off", false, "rop", false},
		{"manager tower on", true, "manager", false},
		{"admin tower on", true, "admin", false},
		{"empty role", true, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldDistributeMskTower(tc.tower, tc.role)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestMaybeDistributeMskTowerLeadEarlyExit(t *testing.T) {
	a := &App{} // без БД: пустые id не ходят в QueryRow
	a.maybeDistributeMskTowerLead(context.Background(), "", "owner")
	a.maybeDistributeMskTowerLead(context.Background(), "lead", "")
	a.maybeDistributeMskTowerLead(context.Background(), "  ", "  ")
}

func TestMskTowerDistributeContract(t *testing.T) {
	// Контракт: без галочки лид остаётся на РОПе; с галочкой — только менеджерам отдела.
	if shouldDistributeMskTower(false, "rop") {
		t.Fatal("default (no flag) must keep lead on ROP")
	}
	if !shouldDistributeMskTower(true, "rop") {
		t.Fatal("msk_tower ROP must distribute to department managers")
	}
	if shouldDistributeMskTower(true, "manager") {
		t.Fatal("flag on non-ROP must not trigger department-only distribute")
	}
}
