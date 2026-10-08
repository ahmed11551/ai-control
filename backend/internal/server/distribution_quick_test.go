package server

import "testing"

func TestShortLoginBase(t *testing.T) {
	if got := shortLoginBase("Корнеев Виктор"); got != "korneev.v" {
		t.Fatalf("got %q", got)
	}
	if got := shortLoginBase("Denis Nikulin Igorevich"); got != "denis.n" {
		t.Fatalf("got %q", got)
	}
	long := shortLoginBase("Оченьдлиннаяфамилиячеловек Пётр")
	if len([]rune(long)) > 14 {
		t.Fatalf("too long: %q", long)
	}
}

func TestGenShortPassword(t *testing.T) {
	p := genShortPassword(8)
	if len(p) != 8 {
		t.Fatalf("len %d", len(p))
	}
}

func TestLooksLikeName(t *testing.T) {
	if !looksLikeName("Михаил РОП") {
		t.Fatal("ok name")
	}
	if looksLikeName("1") || looksLikeName("") {
		t.Fatal("bad name")
	}
}
