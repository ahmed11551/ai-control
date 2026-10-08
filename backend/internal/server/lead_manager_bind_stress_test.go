package server

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
)

// In-memory CRM: много отделов/менеджеров/лидов.
// Зеркалит прод-правило: после owner_opened лид нельзя отозвать/перевыдать с банка.

type simLead struct {
	ID          string
	OwnerID     string // "" = общий банк
	DeptID      string // отдел текущего владельца (если есть)
	OnNewStage  bool
	OwnerOpened bool
	Rejected    bool
}

type simWorld struct {
	mu        sync.Mutex
	leads     map[string]*simLead
	managers  []string          // user ids
	mgrDept   map[string]string // manager -> dept
	deptHead  map[string]string // dept -> rop
	deptMgrs  map[string][]string
	opsOK     atomic.Int64
	opsDenied atomic.Int64
}

func newSimWorld(depts, mgrsPerDept, leadsN int) *simWorld {
	w := &simWorld{
		leads:    make(map[string]*simLead),
		mgrDept:  make(map[string]string),
		deptHead: make(map[string]string),
		deptMgrs: make(map[string][]string),
	}
	for d := 0; d < depts; d++ {
		dept := fmt.Sprintf("dept-%d", d)
		rop := fmt.Sprintf("rop-%d", d)
		w.deptHead[dept] = rop
		for m := 0; m < mgrsPerDept; m++ {
			uid := fmt.Sprintf("mgr-%d-%d", d, m)
			w.managers = append(w.managers, uid)
			w.mgrDept[uid] = dept
			w.deptMgrs[dept] = append(w.deptMgrs[dept], uid)
		}
	}
	for i := 0; i < leadsN; i++ {
		id := fmt.Sprintf("lead-%d", i)
		w.leads[id] = &simLead{
			ID:         id,
			OnNewStage: true,
		}
	}
	return w
}

func (w *simWorld) canLeaveOwner(l *simLead) bool {
	return !l.Rejected && !l.OwnerOpened
}

// issueBank → менеджеру (как admin issue / через РОПа).
func (w *simWorld) issueFromBank(leadID, managerID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	l := w.leads[leadID]
	if l == nil {
		return fmt.Errorf("no lead")
	}
	if l.Rejected {
		w.opsDenied.Add(1)
		return fmt.Errorf("rejected")
	}
	if l.OwnerID != "" {
		w.opsDenied.Add(1)
		return fmt.Errorf("not in bank")
	}
	if !w.canLeaveOwner(l) {
		w.opsDenied.Add(1)
		return fmt.Errorf("opened — permanent bind, bank issue forbidden")
	}
	dept := w.mgrDept[managerID]
	if dept == "" {
		w.opsDenied.Add(1)
		return fmt.Errorf("unknown manager")
	}
	l.OwnerID = managerID
	l.DeptID = dept
	w.opsOK.Add(1)
	return nil
}

// recall менеджера в общий банк (admin) или банк РОПа.
func (w *simWorld) recallToBank(leadID, actorKind string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	l := w.leads[leadID]
	if l == nil {
		return fmt.Errorf("no lead")
	}
	if l.OwnerID == "" {
		w.opsDenied.Add(1)
		return fmt.Errorf("already bank")
	}
	if !l.OnNewStage {
		w.opsDenied.Add(1)
		return fmt.Errorf("not new stage")
	}
	if !w.canLeaveOwner(l) {
		w.opsDenied.Add(1)
		return fmt.Errorf("opened — permanent bind, recall forbidden")
	}
	switch actorKind {
	case "admin_bank":
		l.OwnerID = ""
		l.DeptID = ""
	case "rop_bank":
		head := w.deptHead[l.DeptID]
		if head == "" {
			w.opsDenied.Add(1)
			return fmt.Errorf("no rop")
		}
		l.OwnerID = head
	default:
		w.opsDenied.Add(1)
		return fmt.Errorf("bad actor")
	}
	w.opsOK.Add(1)
	return nil
}

// issue из банка РОПа менеджеру своего отдела.
func (w *simWorld) issueRopBank(leadID, managerID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	l := w.leads[leadID]
	if l == nil {
		return fmt.Errorf("no lead")
	}
	dept := w.mgrDept[managerID]
	head := w.deptHead[dept]
	if l.OwnerID != head {
		w.opsDenied.Add(1)
		return fmt.Errorf("not in rop bank")
	}
	if !l.OnNewStage {
		w.opsDenied.Add(1)
		return fmt.Errorf("not new stage")
	}
	if !w.canLeaveOwner(l) {
		w.opsDenied.Add(1)
		return fmt.Errorf("opened — permanent bind, rop issue forbidden")
	}
	l.OwnerID = managerID
	l.DeptID = dept
	w.opsOK.Add(1)
	return nil
}

func (w *simWorld) openCard(leadID, actor string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	l := w.leads[leadID]
	if l == nil {
		return fmt.Errorf("no lead")
	}
	// Только менеджер-владелец (не РОП, не чужой).
	if actor == "" || l.OwnerID == "" || actor != l.OwnerID {
		w.opsDenied.Add(1)
		return fmt.Errorf("only owning manager stamps open")
	}
	if _, isMgr := w.mgrDept[actor]; !isMgr {
		w.opsDenied.Add(1)
		return fmt.Errorf("not manager")
	}
	l.OwnerOpened = true
	w.opsOK.Add(1)
	return nil
}

func (w *simWorld) assertInvariants(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	openedStuck := 0
	bankOpened := 0
	for _, l := range w.leads {
		if l.OwnerOpened {
			openedStuck++
			// Открытый лид НЕ должен оказаться в общем банке после открытия.
			// (мог быть выдан, открыт, и остаться у менеджера — ок)
			if l.OwnerID == "" {
				bankOpened++
				t.Errorf("INVARIANT: opened lead %s in global bank", l.ID)
			}
			// РОП-банк тоже запрещён после открытия (recall blocked).
			if head, ok := w.deptHead[l.DeptID]; ok && l.OwnerID == head {
				t.Errorf("INVARIANT: opened lead %s recalled to ROP bank", l.ID)
			}
		}
		if l.OwnerID != "" {
			if _, isMgr := w.mgrDept[l.OwnerID]; isMgr {
				if w.mgrDept[l.OwnerID] != l.DeptID && l.DeptID != "" {
					t.Errorf("INVARIANT: lead %s owner dept mismatch", l.ID)
				}
			}
		}
	}
	_ = openedStuck
	_ = bankOpened
}

func TestMultiUserHardBankBindStress(t *testing.T) {
	const (
		depts       = 5
		mgrsPerDept = 8 // 40 менеджеров
		leadsN      = 400
		rounds      = 8000
		workers     = 16
	)
	w := newSimWorld(depts, mgrsPerDept, leadsN)
	if len(w.managers) != depts*mgrsPerDept {
		t.Fatalf("managers=%d", len(w.managers))
	}

	leadIDs := make([]string, 0, leadsN)
	for id := range w.leads {
		leadIDs = append(leadIDs, id)
	}

	// Фаза 1: раздаём почти всё с банка разным менеджерам.
	for i, id := range leadIDs {
		mgr := w.managers[i%len(w.managers)]
		if err := w.issueFromBank(id, mgr); err != nil {
			t.Fatalf("seed issue %s: %v", id, err)
		}
	}
	w.assertInvariants(t)

	// Фаза 2: половина менеджеров открывает свои карточки (жёсткая привязка).
	openedTarget := leadsN / 2
	opened := 0
	for _, id := range leadIDs {
		if opened >= openedTarget {
			break
		}
		w.mu.Lock()
		owner := w.leads[id].OwnerID
		w.mu.Unlock()
		if err := w.openCard(id, owner); err == nil {
			opened++
		}
	}
	if opened < openedTarget-5 {
		t.Fatalf("expected ~%d opens, got %d", openedTarget, opened)
	}
	w.assertInvariants(t)

	// Фаза 3: массовый отзыв — открытые должны ОТКАЗАТЬСЯ, неоткрытые уйти.
	recallDeniedOpened := 0
	recallOK := 0
	for _, id := range leadIDs {
		w.mu.Lock()
		wasOpened := w.leads[id].OwnerOpened
		ownerBefore := w.leads[id].OwnerID
		w.mu.Unlock()
		if ownerBefore == "" {
			continue
		}
		kind := "admin_bank"
		if id[len(id)-1]%2 == 0 {
			kind = "rop_bank"
		}
		err := w.recallToBank(id, kind)
		if wasOpened {
			if err == nil {
				t.Fatalf("opened lead %s recalled — HARD FAIL", id)
			}
			recallDeniedOpened++
		} else if err == nil {
			recallOK++
		}
	}
	if recallDeniedOpened == 0 {
		t.Fatal("expected many denies for opened leads")
	}
	if recallOK == 0 {
		t.Fatal("expected some recalls of unopened")
	}
	w.assertInvariants(t)

	// Фаза 4: попытка снова выдать открытых из банка — все должны fail
	// (открытые не в банке; те что в банке без open — ок).
	// Принудительно: если каким-то чудом открытый в банке — issue запрещён.
	for _, id := range leadIDs {
		w.mu.Lock()
		l := w.leads[id]
		openedL := l.OwnerOpened
		inBank := l.OwnerID == ""
		w.mu.Unlock()
		if openedL && inBank {
			t.Fatalf("opened in bank before issue phase: %s", id)
		}
		if inBank {
			mgr := w.managers[rand.Intn(len(w.managers))]
			if err := w.issueFromBank(id, mgr); err != nil {
				t.Fatalf("unopened bank lead should issue: %v", id)
			}
		}
	}
	w.assertInvariants(t)

	// Фаза 5: concurrent chaos — 16 воркеров, тысячи ops.
	var wg sync.WaitGroup
	var hardFail atomic.Int64
	opsPerWorker := rounds / workers
	for wkr := 0; wkr < workers; wkr++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed))
			for i := 0; i < opsPerWorker; i++ {
				id := leadIDs[r.Intn(len(leadIDs))]
				mgr := w.managers[r.Intn(len(w.managers))]
				switch r.Intn(5) {
				case 0:
					_ = w.openCard(id, mgr) // чаще чужой → deny
				case 1:
					w.mu.Lock()
					owner := w.leads[id].OwnerID
					w.mu.Unlock()
					_ = w.openCard(id, owner)
				case 2:
					_ = w.recallToBank(id, "admin_bank")
				case 3:
					_ = w.recallToBank(id, "rop_bank")
				case 4:
					w.mu.Lock()
					l := w.leads[id]
					inBank := l.OwnerID == ""
					openedL := l.OwnerOpened
					head := w.deptHead[w.mgrDept[mgr]]
					inRop := l.OwnerID == head
					w.mu.Unlock()
					if inBank {
						err := w.issueFromBank(id, mgr)
						if openedL && err == nil {
							hardFail.Add(1)
						}
					} else if inRop {
						err := w.issueRopBank(id, mgr)
						if openedL && err == nil {
							hardFail.Add(1)
						}
					}
				}
			}
		}(int64(wkr+1) * 9973)
	}
	wg.Wait()

	if hardFail.Load() > 0 {
		t.Fatalf("HARD FAIL: opened leads reassigned from bank %d times", hardFail.Load())
	}
	w.assertInvariants(t)

	// Финальный аудит: ни один opened не в банке и не у чужого через recall.
	w.mu.Lock()
	openedCount := 0
	boundToMgr := 0
	for _, l := range w.leads {
		if !l.OwnerOpened {
			continue
		}
		openedCount++
		if l.OwnerID == "" {
			t.Errorf("opened %s in global bank", l.ID)
		}
		if _, isMgr := w.mgrDept[l.OwnerID]; isMgr {
			boundToMgr++
		}
		// Не должен быть у РОПа (отзыв после open запрещён).
		if head := w.deptHead[l.DeptID]; head != "" && l.OwnerID == head {
			t.Errorf("opened %s sitting in ROP bank", l.ID)
		}
	}
	w.mu.Unlock()

	t.Logf("users=%d leads=%d opened=%d bound_to_mgr=%d ops_ok=%d ops_denied=%d",
		len(w.managers)+depts /*+rops*/, leadsN, openedCount, boundToMgr,
		w.opsOK.Load(), w.opsDenied.Load())

	if openedCount == 0 {
		t.Fatal("expected opened leads after stress")
	}
	if boundToMgr == 0 {
		t.Fatal("expected opened leads still on managers")
	}
	if w.opsDenied.Load() < 100 {
		t.Fatalf("expected heavy deny traffic, got %d", w.opsDenied.Load())
	}
}

func TestMultiUserCrossDeptCannotStealOpened(t *testing.T) {
	w := newSimWorld(3, 5, 60)
	// Выдаём всем менеджерам поровну, все открывают.
	i := 0
	for id := range w.leads {
		mgr := w.managers[i%len(w.managers)]
		if err := w.issueFromBank(id, mgr); err != nil {
			t.Fatal(err)
		}
		if err := w.openCard(id, mgr); err != nil {
			t.Fatal(err)
		}
		i++
	}
	// Чужой менеджер другого отдела пытается «забрать» через open + issue.
	thief := w.managers[0]
	victimDept := w.mgrDept[w.managers[len(w.managers)-1]]
	if w.mgrDept[thief] == victimDept {
		thief = w.managers[1]
	}
	for id, l := range w.leads {
		if l.DeptID == victimDept && l.OwnerID != thief {
			if err := w.openCard(id, thief); err == nil {
				t.Fatalf("thief stamped open on foreign lead %s", id)
			}
			if err := w.recallToBank(id, "admin_bank"); err == nil {
				t.Fatalf("opened foreign lead recalled %s", id)
			}
			if err := w.issueFromBank(id, thief); err == nil {
				t.Fatalf("thief issued opened lead from bank %s", id)
			}
		}
	}
	w.assertInvariants(t)
}

func TestMultiUserRopBankRoundTripOnlyUnopened(t *testing.T) {
	w := newSimWorld(2, 4, 80)
	ids := make([]string, 0, 80)
	for id := range w.leads {
		ids = append(ids, id)
	}
	// Выдача → половина open → recall в ROP → issue обратно.
	for i, id := range ids {
		mgr := w.managers[i%len(w.managers)]
		_ = w.issueFromBank(id, mgr)
		if i%2 == 0 {
			_ = w.openCard(id, mgr)
		}
	}
	for _, id := range ids {
		_ = w.recallToBank(id, "rop_bank")
	}
	// В ROP-банке только неоткрытые.
	w.mu.Lock()
	ropBank := 0
	for _, l := range w.leads {
		head := w.deptHead[l.DeptID]
		if head != "" && l.OwnerID == head {
			ropBank++
			if l.OwnerOpened {
				t.Fatalf("opened in rop bank: %s", l.ID)
			}
		}
		if l.OwnerOpened {
			if _, ok := w.mgrDept[l.OwnerID]; !ok {
				t.Fatalf("opened not on manager: %s owner=%s", l.ID, l.OwnerID)
			}
		}
	}
	w.mu.Unlock()
	if ropBank == 0 {
		t.Fatal("expected unopened leads in rop bank")
	}
	// Issue из ROP всем менеджерам отдела.
	for _, id := range ids {
		w.mu.Lock()
		l := w.leads[id]
		head := w.deptHead[l.DeptID]
		inRop := head != "" && l.OwnerID == head
		dept := l.DeptID
		w.mu.Unlock()
		if !inRop {
			continue
		}
		mgrs := w.deptMgrs[dept]
		if err := w.issueRopBank(id, mgrs[0]); err != nil {
			t.Fatalf("rop issue unopened: %v", err)
		}
	}
	w.assertInvariants(t)
}

func TestShouldRecordOwnerOpenedManyRolesMatrix(t *testing.T) {
	roles := []string{"manager", "sales", "admin", "rop", "logist", "observer", ""}
	users := []string{"u1", "u2", "u3", "u4", "u5"}
	allow := 0
	deny := 0
	for _, role := range roles {
		for _, actor := range users {
			for _, owner := range users {
				for _, head := range []bool{false, true} {
					got := shouldRecordOwnerOpened(role, actor, owner, head)
					want := !head &&
						(role == "manager" || role == "sales") &&
						actor != "" && owner != "" && actor == owner
					if got != want {
						t.Fatalf("role=%s actor=%s owner=%s head=%v got=%v want=%v",
							role, actor, owner, head, got, want)
					}
					if got {
						allow++
					} else {
						deny++
					}
				}
			}
		}
	}
	if allow == 0 || deny < allow*3 {
		t.Fatalf("matrix skewed allow=%d deny=%d", allow, deny)
	}
	t.Logf("stamp matrix allow=%d deny=%d", allow, deny)
}

// bank → mgrA → bank → mgrB → bank → mgrC → … пока не открыл.
// После open цепочка рвётся навсегда.
func TestBankHopChainAcrossManyManagers(t *testing.T) {
	const (
		depts       = 4
		mgrsPerDept = 10 // 40 менеджеров
		leadsN      = 50
		hopsPerLead = 25 // каждый лид прыгает по 25 раз через банк
	)
	w := newSimWorld(depts, mgrsPerDept, leadsN)
	mgrs := w.managers
	if len(mgrs) < 3 {
		t.Fatal("need many managers")
	}

	leadIDs := make([]string, 0, leadsN)
	for id := range w.leads {
		leadIDs = append(leadIDs, id)
	}

	type hopLog struct {
		lead, from, to string
	}
	var hops []hopLog
	totalHops := 0

	for li, leadID := range leadIDs {
		// Стартуем из банка, крутим по менеджерам по кругу.
		for hop := 0; hop < hopsPerLead; hop++ {
			mgr := mgrs[(li+hop)%len(mgrs)]
			w.mu.Lock()
			owner := w.leads[leadID].OwnerID
			opened := w.leads[leadID].OwnerOpened
			w.mu.Unlock()

			if opened {
				t.Fatalf("lead %s already opened before hop %d — chain should have stopped", leadID, hop)
			}

			// Если у кого-то — сначала в банк.
			if owner != "" {
				if err := w.recallToBank(leadID, "admin_bank"); err != nil {
					t.Fatalf("hop %d recall %s from %s: %v", hop, leadID, owner, err)
				}
			}
			from := owner
			if err := w.issueFromBank(leadID, mgr); err != nil {
				t.Fatalf("hop %d issue %s → %s: %v", hop, leadID, mgr, err)
			}
			hops = append(hops, hopLog{lead: leadID, from: from, to: mgr})
			totalHops++

			w.mu.Lock()
			got := w.leads[leadID].OwnerID
			w.mu.Unlock()
			if got != mgr {
				t.Fatalf("after hop owner=%s want %s", got, mgr)
			}
		}
	}

	if totalHops != leadsN*hopsPerLead {
		t.Fatalf("hops=%d want %d", totalHops, leadsN*hopsPerLead)
	}

	// Проверяем что реально менялись владельцы (не залипли на одном).
	uniqueOwners := map[string]map[string]struct{}{}
	for _, h := range hops {
		if uniqueOwners[h.lead] == nil {
			uniqueOwners[h.lead] = map[string]struct{}{}
		}
		uniqueOwners[h.lead][h.to] = struct{}{}
	}
	for lead, set := range uniqueOwners {
		if len(set) < 10 {
			t.Fatalf("lead %s visited only %d managers, want ≥10", lead, len(set))
		}
	}
	w.assertInvariants(t)
	t.Logf("unopened chain: leads=%d hops=%d managers=%d", leadsN, totalHops, len(mgrs))

	// Теперь: ещё круг, но на hop=3 менеджер открывает → дальше recall/issue FAIL.
	stuckAt := map[string]string{} // lead → manager who opened
	for li, leadID := range leadIDs {
		for hop := 0; hop < hopsPerLead; hop++ {
			mgr := mgrs[(li+hop+7)%len(mgrs)] // другой сдвиг
			w.mu.Lock()
			opened := w.leads[leadID].OwnerOpened
			owner := w.leads[leadID].OwnerID
			w.mu.Unlock()
			if opened {
				// Любой recall/issue к другому должен отвалиться.
				if err := w.recallToBank(leadID, "admin_bank"); err == nil {
					t.Fatalf("opened %s recalled after stick", leadID)
				}
				if err := w.issueFromBank(leadID, mgr); err == nil {
					t.Fatalf("opened %s re-issued after stick", leadID)
				}
				w.mu.Lock()
				still := w.leads[leadID].OwnerID
				w.mu.Unlock()
				if still != stuckAt[leadID] {
					t.Fatalf("opened %s owner drifted %s → %s", leadID, stuckAt[leadID], still)
				}
				continue
			}
			if owner != "" {
				if err := w.recallToBank(leadID, "admin_bank"); err != nil {
					t.Fatalf("pre-open recall: %v", err)
				}
			}
			if err := w.issueFromBank(leadID, mgr); err != nil {
				t.Fatalf("pre-open issue: %v", err)
			}
			if hop == 3 {
				if err := w.openCard(leadID, mgr); err != nil {
					t.Fatalf("open: %v", err)
				}
				stuckAt[leadID] = mgr
			}
		}
	}
	if len(stuckAt) != leadsN {
		t.Fatalf("stuck=%d want %d", len(stuckAt), leadsN)
	}
	w.assertInvariants(t)

	// Финал: все 50 открытых сидят ровно у того, кто открыл — банк не утащил.
	for lead, mgr := range stuckAt {
		w.mu.Lock()
		l := w.leads[lead]
		w.mu.Unlock()
		if !l.OwnerOpened {
			t.Fatalf("%s not opened", lead)
		}
		if l.OwnerID != mgr {
			t.Fatalf("%s owner=%s want stuck %s", lead, l.OwnerID, mgr)
		}
	}
	t.Logf("after open: all %d leads permanently stuck to opener", len(stuckAt))
}

// Тот же пинг-понг, но через ROP-банк: mgr → rop bank → другой mgr того же отдела.
func TestRopBankHopPingPongLocal(t *testing.T) {
	w := newSimWorld(2, 6, 30)
	dept := "dept-0"
	mgrs := w.deptMgrs[dept]
	if len(mgrs) < 4 {
		t.Fatal("need managers")
	}
	leadIDs := make([]string, 0, 30)
	for id := range w.leads {
		leadIDs = append(leadIDs, id)
	}
	for _, id := range leadIDs {
		if err := w.issueFromBank(id, mgrs[0]); err != nil {
			t.Fatal(err)
		}
	}

	stuck := map[string]string{}
	rounds := 20
	for r := 0; r < rounds; r++ {
		to := mgrs[(r+1)%len(mgrs)]
		for i, id := range leadIDs {
			w.mu.Lock()
			opened := w.leads[id].OwnerOpened
			ownerBefore := w.leads[id].OwnerID
			w.mu.Unlock()

			if opened {
				if err := w.recallToBank(id, "rop_bank"); err == nil {
					t.Fatalf("opened recalled r=%d lead=%s", r, id)
				}
				w.mu.Lock()
				after := w.leads[id].OwnerID
				w.mu.Unlock()
				if after != stuck[id] {
					t.Fatalf("opened drifted %s: %s→%s", id, stuck[id], after)
				}
				continue
			}

			if err := w.recallToBank(id, "rop_bank"); err != nil {
				t.Fatalf("rop recall r=%d: %v", r, err)
			}
			if err := w.issueRopBank(id, to); err != nil {
				t.Fatalf("rop issue r=%d →%s: %v", r, to, err)
			}
			_ = ownerBefore

			// На 10-м круге чётные открывают карточку → дальше залипают.
			if r == 10 && i%2 == 0 {
				if err := w.openCard(id, to); err != nil {
					t.Fatalf("open: %v", err)
				}
				stuck[id] = to
			}
		}
	}
	w.assertInvariants(t)

	openedN, freeN := 0, 0
	w.mu.Lock()
	for _, l := range w.leads {
		if l.OwnerOpened {
			openedN++
			if _, ok := w.mgrDept[l.OwnerID]; !ok {
				t.Errorf("opened not on mgr: %s", l.ID)
			}
		} else {
			freeN++
		}
	}
	w.mu.Unlock()
	t.Logf("rop pingpong: opened=%d still_movable=%d rounds=%d stuck=%d", openedN, freeN, rounds, len(stuck))
	if openedN == 0 || len(stuck) == 0 {
		t.Fatal("expected half opened and stuck")
	}
}
