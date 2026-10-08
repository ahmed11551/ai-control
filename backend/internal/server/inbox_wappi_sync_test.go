package server

import (
	"testing"
	"time"
)

func resetWappiLabelSync() {
	wappiLabelSyncMu.Lock()
	wappiLabelSyncBusy = false
	wappiLabelSyncAt = time.Time{}
	wappiLabelSyncMu.Unlock()
}

func TestWappiLabelSyncRunsOncePerMinute(t *testing.T) {
	resetWappiLabelSync()
	t.Cleanup(resetWappiLabelSync)

	now := time.Date(2026, 10, 6, 11, 0, 0, 0, time.UTC)
	if !wappiLabelSyncStart(now) {
		t.Fatal("first run must start")
	}
	if wappiLabelSyncStart(now.Add(2 * time.Minute)) {
		t.Fatal("second run must wait while the first is busy")
	}
	wappiLabelSyncDone()
	if wappiLabelSyncStart(now.Add(30 * time.Second)) {
		t.Fatal("run inside the minute must be skipped")
	}
	if !wappiLabelSyncStart(now.Add(wappiLabelSyncEvery)) {
		t.Fatal("run after the minute must start")
	}
	wappiLabelSyncDone()
}

func TestWappiLineLabelSame(t *testing.T) {
	const pid, phone, label = "abc-123", "+7 901 417-12-63", "MAX · +7 901 417-12-63"
	if !wappiLineLabelSame(pid, phone, label, true, pid, phone, label) {
		t.Fatal("unchanged active line must be skipped")
	}
	for name, c := range map[string]struct {
		ref, op, name string
		active        bool
	}{
		"inactive":  {pid, phone, label, false},
		"other ref": {"old", phone, label, true},
		"other op":  {pid, "", label, true},
		"old name":  {pid, phone, "MAX · 79014171263", true},
	} {
		if wappiLineLabelSame(c.ref, c.op, c.name, c.active, pid, phone, label) {
			t.Fatalf("%s: changed line must be written", name)
		}
	}
}

