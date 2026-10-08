package server

import (
	"os"
	"testing"
)

func TestAutodialPausedByDefault(t *testing.T) {
	_ = os.Unsetenv("AUTODIAL_ENABLED")
	if autodialFeatureEnabled() {
		t.Fatal("autodial must be paused by default")
	}
}

func TestAutodialEnabledViaEnv(t *testing.T) {
	t.Setenv("AUTODIAL_ENABLED", "1")
	if !autodialFeatureEnabled() {
		t.Fatal("AUTODIAL_ENABLED=1 must turn on")
	}
	t.Setenv("AUTODIAL_ENABLED", "true")
	if !autodialFeatureEnabled() {
		t.Fatal("true")
	}
	t.Setenv("AUTODIAL_ENABLED", "0")
	if autodialFeatureEnabled() {
		t.Fatal("0 must stay off")
	}
}

func TestAutodialPausedMessage(t *testing.T) {
	if autodialPausedMsg == "" {
		t.Fatal("empty msg")
	}
}
