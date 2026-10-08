package server

import (
	"sync"
	"time"
)

const wappiLabelSyncEvery = time.Minute

var (
	wappiLabelSyncMu   sync.Mutex
	wappiLabelSyncBusy bool
	wappiLabelSyncAt   time.Time
)

func wappiLabelSyncStart(now time.Time) bool {
	wappiLabelSyncMu.Lock()
	defer wappiLabelSyncMu.Unlock()
	if wappiLabelSyncBusy || (!wappiLabelSyncAt.IsZero() && now.Sub(wappiLabelSyncAt) < wappiLabelSyncEvery) {
		return false
	}
	wappiLabelSyncBusy = true
	wappiLabelSyncAt = now
	return true
}

func wappiLabelSyncDone() {
	wappiLabelSyncMu.Lock()
	defer wappiLabelSyncMu.Unlock()
	wappiLabelSyncBusy = false
}

func wappiLineLabelSame(ref, phone, label string, active bool, wantedRef, wantedPhone, wantedLabel string) bool {
	return active && ref == wantedRef && phone == wantedPhone && label == wantedLabel
}
