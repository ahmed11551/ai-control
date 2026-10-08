package server

import (
	"os"
	"strings"
	"testing"
)

// --- Admin: отделы по дневным лимитам ---

func TestAdminDailyQuotaNeverExceedsWhileRoomExists(t *testing.T) {
	cands := []distCand{
		{DeptID: "d1", Limit: 100, Today: 0},
		{DeptID: "d2", Limit: 60, Today: 0},
		{DeptID: "d3", Limit: 100, Today: 0},
	}
	for i := 0; i < 260; i++ {
		got, err := pickDistribution(cands)
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		var cur *distCand
		for j := range cands {
			if cands[j].DeptID == got.DeptID {
				cur = &cands[j]
				break
			}
		}
		if cur == nil {
			t.Fatal("unknown dept")
		}
		// Пока хоть у кого-то есть room — нельзя давать сверх лимита.
		hasRoom := false
		for _, c := range cands {
			if c.Today < c.Limit {
				hasRoom = true
				break
			}
		}
		if hasRoom && cur.Today >= cur.Limit {
			t.Fatalf("step %d: gave to %s at cap while room exists %+v", i, cur.DeptID, cands)
		}
		cur.Today++
	}
	for _, c := range cands {
		if c.Today != c.Limit {
			t.Fatalf("%s today=%d want %d", c.DeptID, c.Today, c.Limit)
		}
	}
}

func TestAdminDailyQuotaFillRatioMonotonicCatchUp(t *testing.T) {
	// После каждого шага выбранный должен иметь минимальный fill среди имеющих room.
	cands := []distCand{
		{DeptID: "a", Limit: 100, Today: 0},
		{DeptID: "b", Limit: 60, Today: 0},
		{DeptID: "c", Limit: 100, Today: 0},
	}
	for i := 0; i < 200; i++ {
		got, err := pickDistribution(cands)
		if err != nil {
			t.Fatal(err)
		}
		gotFill := -1.0
		minFill := 1e9
		for _, c := range cands {
			if c.Today >= c.Limit {
				continue
			}
			f := float64(c.Today) / float64(c.Limit)
			if f < minFill {
				minFill = f
			}
			if c.DeptID == got.DeptID {
				gotFill = f
			}
		}
		if gotFill < 0 {
			t.Fatalf("step %d picked capped %s", i, got.DeptID)
		}
		if gotFill > minFill+1e-9 {
			t.Fatalf("step %d: picked fill=%.4f but min=%.4f (%+v)", i, gotFill, minFill, cands)
		}
		for j := range cands {
			if cands[j].DeptID == got.DeptID {
				cands[j].Today++
			}
		}
	}
}

func TestAdminDailyQuotaDeterministic(t *testing.T) {
	base := []distCand{
		{DeptID: "a", Limit: 40, Today: 0, HeadID: "ha"},
		{DeptID: "b", Limit: 25, Today: 0, HeadID: "hb"},
		{DeptID: "c", Limit: 35, Today: 0, HeadID: "hc"},
	}
	run := func() []string {
		cands := append([]distCand(nil), base...)
		var seq []string
		for i := 0; i < 100; i++ {
			got, err := pickDistribution(cands)
			if err != nil {
				t.Fatal(err)
			}
			seq = append(seq, got.DeptID)
			for j := range cands {
				if cands[j].DeptID == got.DeptID {
					cands[j].Today++
				}
			}
		}
		return seq
	}
	a, b := run(), run()
	if len(a) != len(b) {
		t.Fatal("len")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("nondeterministic at %d: %s vs %s", i, a[i], b[i])
		}
	}
}

// --- ROP: менеджеры по дневным лимитам (тот же pickDistribution) ---

func TestRopDailyQuotaManagers100_60_40(t *testing.T) {
	// HeadID = manager id; DeptID одинаковый (внутри одного отдела).
	cands := []distCand{
		{DeptID: "dept", Limit: 100, Today: 0, HeadID: "m1"},
		{DeptID: "dept", Limit: 60, Today: 0, HeadID: "m2"},
		{DeptID: "dept", Limit: 40, Today: 0, HeadID: "m3"},
	}
	counts := map[string]int{}
	for i := 0; i < 200; i++ {
		got, err := pickDistribution(cands)
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		counts[got.HeadID]++
		for j := range cands {
			if cands[j].HeadID == got.HeadID {
				cands[j].Today++
			}
		}
	}
	if counts["m1"] != 100 || counts["m2"] != 60 || counts["m3"] != 40 {
		t.Fatalf("mgr counts=%v", counts)
	}
}

func TestRopDailyQuotaHardCapAmongManagers(t *testing.T) {
	cands := []distCand{
		{DeptID: "d", Limit: 5, Today: 5, HeadID: "full"},
		{DeptID: "d", Limit: 5, Today: 1, HeadID: "open"},
	}
	for i := 0; i < 4; i++ {
		got, err := pickDistribution(cands)
		if err != nil {
			t.Fatal(err)
		}
		if got.HeadID != "open" {
			t.Fatalf("step %d got %s", i, got.HeadID)
		}
		for j := range cands {
			if cands[j].HeadID == got.HeadID {
				cands[j].Today++
			}
		}
	}
}

// --- Двухшаговая симуляция: админ→отдел, РОП→менеджер ---

func TestAdminThenRopTwoStageDailyQuota(t *testing.T) {
	depts := []distCand{
		{DeptID: "d1", Limit: 50, Today: 0, HeadID: "rop1"},
		{DeptID: "d2", Limit: 30, Today: 0, HeadID: "rop2"},
	}
	mgrs := map[string][]distCand{
		"d1": {
			{DeptID: "d1", Limit: 30, Today: 0, HeadID: "d1m1"},
			{DeptID: "d1", Limit: 20, Today: 0, HeadID: "d1m2"},
		},
		"d2": {
			{DeptID: "d2", Limit: 15, Today: 0, HeadID: "d2m1"},
			{DeptID: "d2", Limit: 15, Today: 0, HeadID: "d2m2"},
		},
	}
	deptCounts := map[string]int{}
	mgrCounts := map[string]int{}
	total := 80
	for i := 0; i < total; i++ {
		dept, err := pickDistribution(depts)
		if err != nil {
			t.Fatalf("dept step %d: %v", i, err)
		}
		deptCounts[dept.DeptID]++
		for j := range depts {
			if depts[j].DeptID == dept.DeptID {
				depts[j].Today++
			}
		}
		pool := mgrs[dept.DeptID]
		mgr, err := pickDistribution(pool)
		if err != nil {
			t.Fatalf("mgr step %d: %v", i, err)
		}
		mgrCounts[mgr.HeadID]++
		for j := range pool {
			if pool[j].HeadID == mgr.HeadID {
				pool[j].Today++
			}
		}
		mgrs[dept.DeptID] = pool
	}
	if deptCounts["d1"] != 50 || deptCounts["d2"] != 30 {
		t.Fatalf("depts=%v", deptCounts)
	}
	// внутри d1: 30+20, внутри d2: 15+15
	if mgrCounts["d1m1"] != 30 || mgrCounts["d1m2"] != 20 {
		t.Fatalf("d1 mgrs=%v", mgrCounts)
	}
	if mgrCounts["d2m1"] != 15 || mgrCounts["d2m2"] != 15 {
		t.Fatalf("d2 mgrs=%v", mgrCounts)
	}
}

func TestAdminSaveWritesShareZeroContract(t *testing.T) {
	src, err := os.ReadFile("distribution.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	i := strings.Index(s, "func (a *App) saveDistribution")
	if i < 0 {
		t.Fatal("saveDistribution missing")
	}
	j := strings.Index(s[i+1:], "\nfunc (a *App)")
	if j < 0 {
		j = len(s) - i
	}
	fn := s[i : i+j]
	if !strings.Contains(fn, "share=0") && !strings.Contains(fn, "VALUES ($1::uuid,$2,0,$3)") {
		t.Fatal("admin save must persist share=0 (daily-limit model)")
	}
	if strings.Contains(fn, "сумма долей") {
		t.Fatal("admin save must not validate 100% shares")
	}
}

func TestRopSaveWritesShareZeroContract(t *testing.T) {
	src, err := os.ReadFile("distribution_team.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	i := strings.Index(s, "func (a *App) saveDistributionTeam")
	if i < 0 {
		t.Fatal("saveDistributionTeam missing")
	}
	j := strings.Index(s[i+1:], "\nfunc (a *App)")
	if j < 0 {
		j = len(s) - i
	}
	fn := s[i : i+j]
	if !strings.Contains(fn, "share=0") {
		t.Fatal("rop save must persist share=0")
	}
	if strings.Contains(fn, "сумма долей не может быть больше 100") {
		t.Fatal("rop save must not enforce 100% share sum")
	}
	if !strings.Contains(fn, "flushed_to_managers\": 0") && !strings.Contains(fn, `"flushed_to_managers": 0`) {
		// JSON uses flushed_to_managers: 0 without flush call
		if strings.Contains(fn, "flushROPOwnedToManagers") {
			t.Fatal("rop save must not flush ROP pool")
		}
	}
	if strings.Contains(fn, "flushROPOwnedToManagers") {
		t.Fatal("rop save must not flush ROP pool")
	}
}

func TestUIDailyLimitNotPercentContract(t *testing.T) {
	admin, err := os.ReadFile("../../../src/pages/distribution/DistributionPage.tsx")
	if err != nil {
		// path from backend/internal/server
		admin, err = os.ReadFile("../../../../src/pages/distribution/DistributionPage.tsx")
	}
	if err != nil {
		t.Skip("frontend path not available from test cwd")
	}
	rop, err := os.ReadFile("../../../src/pages/distribution/RopTeamDistributionPage.tsx")
	if err != nil {
		rop, err = os.ReadFile("../../../../src/pages/distribution/RopTeamDistributionPage.tsx")
	}
	if err != nil {
		t.Skip("rop frontend path missing")
	}
	as, rs := string(admin), string(rop)
	for name, s := range map[string]string{"admin": as, "rop": rs} {
		if !strings.Contains(s, "Лидов/день") && !strings.Contains(s, "лидов/день") && !strings.Contains(s, "Лидов в день") {
			t.Fatalf("%s UI must show daily lead field", name)
		}
		if strings.Contains(s, ">Доля %<") || strings.Contains(s, "На 100%") {
			t.Fatalf("%s UI still has percent share controls", name)
		}
	}
}

func TestPickUsesLimitNotShareWhenBothSet(t *testing.T) {
	// Share огромный у B, но Limit говорит иначе — побеждает fill по Limit.
	got, err := pickDistribution([]distCand{
		{DeptID: "a", Share: 1, Limit: 100, Today: 0},
		{DeptID: "b", Share: 99, Limit: 10, Today: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	// оба fill=0; tie-break больший Limit → a
	if got.DeptID != "a" {
		t.Fatalf("limit weight should prefer a, got %s", got.DeptID)
	}
	// a уже 50%, b 0% → b
	got, err = pickDistribution([]distCand{
		{DeptID: "a", Share: 1, Limit: 100, Today: 50},
		{DeptID: "b", Share: 99, Limit: 10, Today: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.DeptID != "b" {
		t.Fatalf("lagging b by limit fill, got %s", got.DeptID)
	}
}
