package server

import "testing"

func TestSuggestLoginFromName(t *testing.T) {
	if got := suggestLoginFromName("Иванов Иван"); got != "ivanov.i" {
		t.Fatalf("got %q want ivanov.i", got)
	}
	if got := sanitizeLogin("Ivanov!!"); got != "ivanov" {
		t.Fatalf("got %q want ivanov", got)
	}
}
