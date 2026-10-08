package server

import "testing"

func TestKeepMaxThreadLine(t *testing.T) {
	if !keepMaxThreadLine("line-a", "line-b") {
		t.Fatal("another account must not rewrite the open dialog")
	}
	if keepMaxThreadLine("line-a", "line-a") {
		t.Fatal("the same account stays on this dialog")
	}
	if keepMaxThreadLine("", "line-b") {
		t.Fatal("a dialog without an account can take the resolved line")
	}
	if keepMaxThreadLine("line-a", "") {
		t.Fatal("empty resolution does not count as a switch")
	}
	if !keepMaxThreadLine("  line-a ", "line-b") {
		t.Fatal("spaces around the thread line still protect it")
	}
}
