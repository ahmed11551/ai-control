package server

import "testing"

func TestPickDailyQuota100_60_100(t *testing.T) {
	// Классика: A=100 B=60 C=100 — всегда минимальный today/limit.
	cands := []distCand{
		{DeptID: "a", Limit: 100, Today: 0},
		{DeptID: "b", Limit: 60, Today: 0},
		{DeptID: "c", Limit: 100, Today: 0},
	}
	counts := map[string]int{}
	for i := 0; i < 260; i++ {
		got, err := pickDistribution(cands)
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		counts[got.DeptID]++
		for j := range cands {
			if cands[j].DeptID == got.DeptID {
				cands[j].Today++
			}
		}
	}
	if counts["a"] != 100 || counts["b"] != 60 || counts["c"] != 100 {
		t.Fatalf("final counts=%v want a100 b60 c100", counts)
	}
}

func TestPickDailyQuotaMiddayPace(t *testing.T) {
	// После пропорционального набора ~половина плана.
	cands := []distCand{
		{DeptID: "a", Limit: 100, Today: 0},
		{DeptID: "b", Limit: 60, Today: 0},
		{DeptID: "c", Limit: 100, Today: 0},
	}
	for i := 0; i < 130; i++ {
		got, err := pickDistribution(cands)
		if err != nil {
			t.Fatal(err)
		}
		for j := range cands {
			if cands[j].DeptID == got.DeptID {
				cands[j].Today++
			}
		}
	}
	by := map[string]int{}
	for _, c := range cands {
		by[c.DeptID] = c.Today
	}
	// ~50/30/50 (±2 из‑за дискретности)
	if by["a"] < 48 || by["a"] > 52 || by["c"] < 48 || by["c"] > 52 {
		t.Fatalf("mid a/c=%v", by)
	}
	if by["b"] < 28 || by["b"] > 32 {
		t.Fatalf("mid b=%v", by)
	}
}

func TestPickDailyQuotaHardCap(t *testing.T) {
	got, err := pickDistribution([]distCand{
		{DeptID: "full", Limit: 10, Today: 10},
		{DeptID: "open", Limit: 10, Today: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.DeptID != "open" {
		t.Fatalf("got %s", got.DeptID)
	}
}

func TestPickDailyQuotaHardStopWhenFull(t *testing.T) {
	_, err := pickDistribution([]distCand{
		{DeptID: "a", Limit: 10, Today: 10},
		{DeptID: "b", Limit: 10, Today: 12},
	})
	if err == nil {
		t.Fatal("expected hard stop when all at/over limit")
	}
}

func TestPickDailyQuotaIgnoresZeroLimit(t *testing.T) {
	got, err := pickDistribution([]distCand{
		{DeptID: "off", Limit: 0, Today: 0},
		{DeptID: "on", Limit: 50, Today: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.DeptID != "on" {
		t.Fatalf("got %s", got.DeptID)
	}
}

func TestManagerZeroPlanStaysOff(t *testing.T) {
	// План 0 и даже старая доля не выбирают менеджера.
	eligible := managerAutoCands([]distCand{
		{HeadID: "a", Limit: 0, Share: 50, Today: 0},
		{HeadID: "b", Limit: 0, Share: 50, Today: 7},
	})
	if len(eligible) != 0 {
		t.Fatalf("zero plan must be excluded, got %d", len(eligible))
	}
	if _, err := pickDistribution(eligible); err == nil {
		t.Fatal("empty plan must not assign a manager")
	}
}

func TestManagerZeroPlanDoesNotStealFromOpenPlan(t *testing.T) {
	eligible := managerAutoCands([]distCand{
		{HeadID: "off", Limit: 0, Share: 100, Today: 0},
		{HeadID: "on", Limit: 10, Share: 0, Today: 3},
	})
	got, err := pickDistribution(eligible)
	if err != nil {
		t.Fatal(err)
	}
	if got.HeadID != "on" {
		t.Fatalf("got %s", got.HeadID)
	}
}

func TestPickDailyQuotaNoCandidates(t *testing.T) {
	_, err := pickDistribution([]distCand{
		{DeptID: "x", Limit: 0, Today: 0},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPickDailyQuotaManagerLag(t *testing.T) {
	got, err := pickDistribution([]distCand{
		{DeptID: "d", Limit: 20, Today: 10, HeadID: "mgr-a"},
		{DeptID: "d", Limit: 20, Today: 2, HeadID: "mgr-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.HeadID != "mgr-b" {
		t.Fatalf("got %s", got.HeadID)
	}
}

func TestPickDailyQuotaTieStable(t *testing.T) {
	// одинаковый fill и limit — стабильный HeadID/DeptID
	got, err := pickDistribution([]distCand{
		{DeptID: "b", Limit: 10, Today: 0, HeadID: "h2"},
		{DeptID: "a", Limit: 10, Today: 0, HeadID: "h1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.HeadID != "h1" {
		t.Fatalf("stable tie-break HeadID, got %s", got.HeadID)
	}
}

func TestEqualShares(t *testing.T) {
	s := equalShares(3)
	sum := 0.0
	for _, n := range s {
		sum += n
	}
	if sum < 99.9 || sum > 100.1 || len(s) != 3 {
		t.Fatalf("%v sum=%v", s, sum)
	}
}

func TestInferPoolFromSource(t *testing.T) {
	if inferPoolFromSource("manual") != "" {
		t.Fatal("manual")
	}
	if inferPoolFromSource("Скорозвон") != "skorozvon" {
		t.Fatal("skorozvon")
	}
	if inferPoolFromSource("avito") != "depts" {
		t.Fatal("avito")
	}
	if inferPoolFromSource("bars") != "depts" {
		t.Fatal("bars→depts")
	}
}

func TestDistSeedHint(t *testing.T) {
	p, lim := distSeedHint("Фонтанка 2")
	if len(p) != 1 || p[0] != "depts" || lim != 200 {
		t.Fatalf("%v %d", p, lim)
	}
	p, lim = distSeedHint("Екатеринбург HR")
	if len(p) != 2 || lim != 150 {
		t.Fatalf("%v %d", p, lim)
	}
	p, _ = distSeedHint("Башня")
	if p != nil {
		t.Fatalf("excluded %v", p)
	}
}

func TestPickDistributionAllLimitedStops(t *testing.T) {
	_, err := pickDistribution([]distCand{
		{DeptID: "a", Limit: 10, Today: 10},
		{DeptID: "b", Limit: 10, Today: 12},
	})
	if err == nil {
		t.Fatal("expected stop, not soft overflow")
	}
}
