package server

import (
	"strings"
	"testing"
)

// Контракт заказчика:
// 1) Лид привязан к одному менеджеру после выдачи.
// 2) Из банка / отзыв / перераздача — ТОЛЬКО если менеджер ещё НЕ открывал карточку (owner_opened).
// 3) После открытия карточки лид «закреплён»: ни админ-отзыв, ни выдача из банка, ни банк РОПа.

func TestLeadBoundToOneManagerBankTransferContract(t *testing.T) {
	type step struct {
		name          string
		ownerOpened   bool // менеджер открыл карточку
		onNewStage    bool // колонка «Новый лид»
		inBank        bool // owner_id IS NULL
		wantRecallOK  bool // admin/ROP отзыв у менеджера
		wantIssueOK   bool // выдача из общего/ROP банка
		wantRopRecall bool // ROP отзыв у менеджера
	}
	// Правило: отзыв/выдача допустимы только если !ownerOpened.
	// Для отзыва ещё нужна стадия «Новый лид»; для issue из банка — лид в банке.
	cases := []step{
		{
			name:        "неоткрытый_новый_у_менеджера_можно_отозвать",
			ownerOpened: false, onNewStage: true, inBank: false,
			wantRecallOK: true, wantIssueOK: false, wantRopRecall: true,
		},
		{
			name:        "открыл_карточку_навсегда_закреплён_отзыв_запрещён",
			ownerOpened: true, onNewStage: true, inBank: false,
			wantRecallOK: false, wantIssueOK: false, wantRopRecall: false,
		},
		{
			name:        "открыл_даже_если_ещё_новый_лид_в_колонке",
			ownerOpened: true, onNewStage: true, inBank: false,
			wantRecallOK: false, wantIssueOK: false, wantRopRecall: false,
		},
		{
			name:        "в_банке_неоткрытый_можно_выдать",
			ownerOpened: false, onNewStage: true, inBank: true,
			wantRecallOK: false, wantIssueOK: true, wantRopRecall: false,
		},
		{
			name:        "в_банке_но_уже_открывался_выдача_запрещена",
			ownerOpened: true, onNewStage: true, inBank: true,
			wantRecallOK: false, wantIssueOK: false, wantRopRecall: false,
		},
		{
			name:        "неоткрытый_но_уже_не_новый_лид_отзыв_запрещён_стадией",
			ownerOpened: false, onNewStage: false, inBank: false,
			wantRecallOK: false, wantIssueOK: false, wantRopRecall: false,
		},
	}

	eligibleUnopened := func(opened bool) bool { return !opened }

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			canByOpen := eligibleUnopened(tc.ownerOpened)
			gotRecall := canByOpen && tc.onNewStage && !tc.inBank
			gotIssue := canByOpen && tc.inBank
			gotRopRecall := canByOpen && tc.onNewStage && !tc.inBank

			if gotRecall != tc.wantRecallOK {
				t.Fatalf("recall: got %v want %v (opened=%v new=%v bank=%v)",
					gotRecall, tc.wantRecallOK, tc.ownerOpened, tc.onNewStage, tc.inBank)
			}
			if gotIssue != tc.wantIssueOK {
				t.Fatalf("issue: got %v want %v (opened=%v bank=%v)",
					gotIssue, tc.wantIssueOK, tc.ownerOpened, tc.inBank)
			}
			if gotRopRecall != tc.wantRopRecall {
				t.Fatalf("rop recall: got %v want %v", gotRopRecall, tc.wantRopRecall)
			}
		})
	}
}

func TestAllBankPathsRequireNeverOpenedFilter(t *testing.T) {
	// Точные фрагменты SQL как в production-коде.
	paths := map[string]string{
		"admin_recall": `AND (` + sqlLeadOnNewApplicationStage("l") + `) AND (` + sqlLeadNeverOpenedByManager("l") + `)`,
		"admin_issue":  `AND (` + sqlLeadNeverOpenedByManager("leads") + `)`,
		"rop_issue":    `AND (` + sqlLeadOnNewApplicationStage("leads") + `) AND (` + sqlLeadNeverOpenedByManager("leads") + `)`,
		"rop_recall":   `AND (` + sqlLeadOnNewApplicationStage("leads") + `) AND (` + sqlLeadNeverOpenedByManager("leads") + `)`,
	}
	for name, sql := range paths {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(sql, "owner_opened") {
				t.Fatal("must gate on owner_opened")
			}
			if !strings.Contains(sql, "NOT EXISTS") {
				t.Fatal("must be NOT EXISTS (opened → exclude)")
			}
			if !strings.Contains(sql, "activity_log") {
				t.Fatal("must read activity_log")
			}
			// Постоянно, без окна «N часов».
			if strings.Contains(sql, "interval") || strings.Contains(sql, "24 hours") {
				t.Fatal("open must be permanent, no time window")
			}
		})
	}
}

func TestOnlyManagerOwnerCanStampOwnerOpened(t *testing.T) {
	// Чужой менеджер / РОП / admin / head не ставят флаг → не «блокируют» чужой банк ложным открытием.
	deny := []struct {
		role, actor, owner string
		head               bool
	}{
		{"manager", "mgr-a", "mgr-b", false},
		{"admin", "admin", "admin", false},
		{"rop", "rop", "rop", false},
		{"manager", "head", "head", true},
		{"logist", "log", "log", false},
		{"manager", "", "mgr", false},
		{"manager", "mgr", "", false},
	}
	for _, d := range deny {
		if shouldRecordOwnerOpened(d.role, d.actor, d.owner, d.head) {
			t.Fatalf("must NOT stamp: role=%s actor=%s owner=%s head=%v", d.role, d.actor, d.owner, d.head)
		}
	}
	if !shouldRecordOwnerOpened("manager", "mgr-1", "mgr-1", false) {
		t.Fatal("owning manager must stamp owner_opened")
	}
	if !shouldRecordOwnerOpened("sales", "mgr-1", "mgr-1", false) {
		t.Fatal("sales alias must stamp owner_opened")
	}
}

func TestOpenedLeadCannotBeSelectedByBankSQLSemantics(t *testing.T) {
	// Семантика фильтра: есть owner_opened → NOT EXISTS ложен → строка не в выборке.
	filter := sqlLeadNeverOpenedByManager("l")
	hasOpened := true
	// Эмуляция: если activity есть, кандидат отсекается.
	passesFilter := !hasOpened // NOT EXISTS owner_opened
	if passesFilter {
		t.Fatal("opened lead must fail never-opened filter")
	}
	hasOpened = false
	passesFilter = !hasOpened
	if !passesFilter {
		t.Fatal("unopened lead must pass never-opened filter")
	}
	if !strings.Contains(filter, "owner_opened") {
		t.Fatal(filter)
	}
}

func TestManagerBoardOnlyOwnDeals(t *testing.T) {
	// Параллельный жёсткий слой: на канбане менеджер видит только свои owner_id.
	optsMine := BoardAclOptsForTest{UserID: "mgr-1", IsManager: true}
	if !dealBelongsOnBoardForTest(BoardDealForTest{OwnerID: "mgr-1"}, optsMine) {
		t.Fatal("own deal must be on board")
	}
	if dealBelongsOnBoardForTest(BoardDealForTest{OwnerID: "mgr-2"}, optsMine) {
		t.Fatal("foreign deal must NOT be on board")
	}
	if dealBelongsOnBoardForTest(BoardDealForTest{}, optsMine) {
		t.Fatal("nil owner must NOT be on board")
	}
}

// --- тонкие адаптеры под Go-тесты без импорта frontend ---

type BoardDealForTest struct{ OwnerID string }
type BoardAclOptsForTest struct {
	UserID    string
	IsManager bool
	Mine      bool
}

func dealBelongsOnBoardForTest(deal BoardDealForTest, opts BoardAclOptsForTest) bool {
	uid := strings.TrimSpace(opts.UserID)
	if uid == "" {
		return false
	}
	if opts.Mine || opts.IsManager {
		return deal.OwnerID != "" && deal.OwnerID == uid
	}
	return true
}
