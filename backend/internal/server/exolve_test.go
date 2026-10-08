package server

import "testing"

func TestNormalizeExolveDID(t *testing.T) {
	cases := map[string]string{
		"+7 (900) 123-45-67": "79001234567",
		"89001234567":        "79001234567",
		"79001234567":        "79001234567",
		"883140776911964":    "883140776911964",
		"":                   "",
	}
	for in, want := range cases {
		if got := normalizeExolveDID(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}
