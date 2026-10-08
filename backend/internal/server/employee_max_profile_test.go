package server

import "testing"

func TestChooseMaxProfileOnePerPhone(t *testing.T) {
	// Уже включённая линия важнее второго входа того же номера.
	if got := chooseMaxProfile("dup", "live", false, true, true, true); got != "live" {
		t.Fatalf("active line wins, got %s", got)
	}
	if got := chooseMaxProfile("live", "dup", true, false, true, true); got != "live" {
		t.Fatalf("must keep active line, got %s", got)
	}
	// Оба свободны — остаётся онлайн.
	if got := chooseMaxProfile("off", "on", false, false, false, true); got != "on" {
		t.Fatalf("working profile wins, got %s", got)
	}
	// Оба онлайн и свободны — один стабильный id.
	if got := chooseMaxProfile("b", "a", false, false, true, true); got != "a" {
		t.Fatalf("stable id, got %s", got)
	}
}
