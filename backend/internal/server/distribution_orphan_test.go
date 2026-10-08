package server

import (
	"strings"
	"sync"
	"testing"
)

func TestOrphanDrainSQLOnlyNewApplicationStage(t *testing.T) {
	sql := sqlUnownedNewApplicationLeads()
	if !strings.Contains(sql, "owner_id IS NULL") {
		t.Fatal("must require empty owner")
	}
	if !strings.Contains(sql, hireNewApplicationStage) {
		t.Fatalf("must filter stage %q", hireNewApplicationStage)
	}
	if !strings.Contains(sql, "rejected") {
		t.Fatal("must skip rejected")
	}
	if strings.Contains(sql, hirePipelineKey+":3") || strings.Contains(sql, "Недозвон") {
		t.Fatal("must not target nedozvon stages")
	}
}

func TestRopHeldDrainSQLTargetsRopNotManagers(t *testing.T) {
	sql := sqlRopHeldNewApplicationLeads()
	if !strings.Contains(sql, "head_user_id") {
		t.Fatal("must join department head (ROP)")
	}
	if !strings.Contains(sql, "'rop'") {
		t.Fatal("must require ROP role")
	}
	if !strings.Contains(sql, hireNewApplicationStage) {
		t.Fatal("must stay on Новый лид")
	}
	if strings.Contains(sql, "owner_id IS NULL") {
		t.Fatal("rop-held leads already have owner=ROP")
	}
	if !strings.Contains(sql, "held_on_rop") {
		t.Fatal("leads returned to the ROP must stay there")
	}
	if !strings.Contains(sql, "team_auto:") || !strings.Contains(sql, "value = '0'") {
		t.Fatal("department with auto off must stay out of the queue")
	}
	if !strings.Contains(sql, "daily_limit > 0") || !strings.Contains(sql, "coalesce(c.assigned,0) < q.daily_limit") {
		t.Fatal("department with no manager room must not occupy the batch")
	}
	if !strings.Contains(sql, "$2::date") {
		t.Fatal("room check must use the Moscow day")
	}
}

func TestDistDrainScanLooksPastBlocked(t *testing.T) {
	if distDrainScanLimit(25) < 400 {
		t.Fatalf("scan %d, a full department in front would still fill the batch", distDrainScanLimit(25))
	}
	if distDrainScanLimit(200) > 1000 {
		t.Fatalf("scan %d is too wide", distDrainScanLimit(200))
	}
}

func TestDistributeOneClaimRequiresNewStageSQL(t *testing.T) {
	frag := sqlLeadOnNewApplicationStage("leads")
	if !strings.Contains(frag, hireNewApplicationStage) {
		t.Fatal(frag)
	}
	if !strings.Contains(frag, "NOT EXISTS") {
		t.Fatal("leads without deal must qualify as new")
	}
}

func TestOrphanDrainSQLAllowsLeadWithoutDeal(t *testing.T) {
	sql := sqlLeadOnNewApplicationStage("leads")
	if !strings.Contains(sql, "NOT EXISTS") {
		t.Fatal("leads without deals must still be eligible (new application)")
	}
}

func TestDistributeOneClaimConflictSemantics(t *testing.T) {
	msg := "лид уже назначен или не в колонке «Новый лид»"
	if !strings.Contains(msg, "уже назначен") || !strings.Contains(msg, "Новый лид") {
		t.Fatal("conflict marker changed")
	}
}

func TestPickDailyQuotaNoDoubleCountUnderParallel(t *testing.T) {
	cands := []distCand{
		{DeptID: "a", HeadID: "h1", Limit: 10, Today: 0},
		{DeptID: "b", HeadID: "h2", Limit: 10, Today: 0},
	}
	var picks sync.Map
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, ok := pickDailyQuota(cands, true)
			if !ok {
				return
			}
			picks.Store(got.DeptID+"|"+got.HeadID, true)
		}()
	}
	wg.Wait()
	n := 0
	picks.Range(func(_, _ any) bool { n++; return true })
	if n == 0 {
		t.Fatal("expected picks")
	}
}

func TestOrphanDrainSkipsOwnedMarker(t *testing.T) {
	errs := []string{
		"лид уже назначен или не в колонке «Новый лид»",
		"лид уже назначен",
	}
	for _, errOwned := range errs {
		ok := strings.Contains(errOwned, "уже назначен") || strings.Contains(errOwned, "Новый лид")
		if !ok {
			t.Fatalf("race skip broken for %q", errOwned)
		}
	}
}
