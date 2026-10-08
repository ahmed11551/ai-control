package server

import (
	"strings"
	"testing"
)

func TestBankRoom(t *testing.T) {
	if bankRoom(10, 10) != 0 {
		t.Fatal("full")
	}
	if bankRoom(110, 10) != 100 {
		t.Fatal("raised")
	}
	if bankRoom(0, 0) != 0 {
		t.Fatal("unlimited must not dump the whole bank")
	}
}

func TestClampBankIssue(t *testing.T) {
	if clampBankIssue(100, 100, 200) != 100 {
		t.Fatal("room")
	}
	if clampBankIssue(100, 50, 200) != 50 {
		t.Fatal("want")
	}
	if clampBankIssue(100, 80, 30) != 30 {
		t.Fatal("bank")
	}
	if clampBankIssue(0, 10, 10) != 0 {
		t.Fatal("no room")
	}
}

func TestHireNewApplicationStageKey(t *testing.T) {
	if hireNewApplicationStage != "local:hire:1" {
		t.Fatalf("stage=%q", hireNewApplicationStage)
	}
	if adaptationStages[0].key != "1" {
		t.Fatal("first adaptation stage must stay «Новая заявка»")
	}
	sql := sqlLeadOnNewApplicationStage("l")
	if !strings.Contains(sql, hireNewApplicationStage) {
		t.Fatal("filter must pin new-application stage")
	}
	if !strings.Contains(sql, "pipeline_stages") {
		t.Fatal("filter must join deal stage")
	}
	// Недозвоны / документы / в работе — другие ключи, в фильтр не попадают.
	for _, bad := range []string{":2", ":3", ":docs", ":bilet"} {
		if strings.Contains(sql, "local:hire"+bad) {
			t.Fatalf("filter must not include %s", bad)
		}
	}
}

func TestBankOpsDisabledMsg(t *testing.T) {
	if !strings.Contains(distBankOpsDisabledMsg, "отключены") {
		t.Fatalf("%q", distBankOpsDisabledMsg)
	}
}
