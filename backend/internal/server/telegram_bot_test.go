package server

import "testing"

func TestEscapeTelegramHTML(t *testing.T) {
	got := escapeTelegramHTML(`A <b> & "x"`)
	if got != `A &lt;b&gt; &amp; "x"` {
		t.Fatalf("got %q", got)
	}
}

func TestTelegramReadyRequiresToken(t *testing.T) {
	a := &App{}
	if a.telegramReady() {
		t.Fatal("empty token must be not ready")
	}
}
