package server

import "testing"

func TestNightBriefEditsStayOnTheHandoff(t *testing.T) {
	if nightBriefNameOverride("  Евгений ", "Евгений") != "" {
		t.Fatal("same name must stay linked to the card")
	}
	if nightBriefNameOverride("Евгений Петрович", "Евгений") != "Евгений Петрович" {
		t.Fatal("a corrected name is kept on the handoff")
	}
	for _, raw := range []string{"поезд", "rail", "жд"} {
		got, ok := nightBriefTransport(raw)
		if !ok || got != "поезд" {
			t.Fatalf("%s -> %s ok=%v", raw, got, ok)
		}
	}
	if _, ok := nightBriefTransport("вертолёт"); ok {
		t.Fatal("unknown transport")
	}
	if v, err := nightBriefTime(""); err != nil || v != nil {
		t.Fatal("empty time clears")
	}
}

func TestNightNoteWriter(t *testing.T) {
	const owner = "manager-1"
	const head = "rop-1"
	if !nightNoteWriter("night_logist", "n1", owner, head) {
		t.Fatal("night logist adds a note")
	}
	if !nightNoteWriter("logist", "l1", owner, head) {
		t.Fatal("logist adds a note")
	}
	if !nightNoteWriter("admin", "a1", owner, head) {
		t.Fatal("admin adds a note")
	}
	if !nightNoteWriter("manager", owner, owner, head) {
		t.Fatal("card manager adds a note")
	}
	if !nightNoteWriter("sales", owner, owner, head) {
		t.Fatal("card owner adds a note")
	}
	if !nightNoteWriter("rop", head, owner, head) {
		t.Fatal("this manager's ROP adds a note")
	}
	if !nightNoteWriter("manager", head, owner, head) {
		t.Fatal("department head adds a note even without the rop role")
	}
	if nightNoteWriter("rop", "other-rop", owner, head) {
		t.Fatal("another ROP must not write")
	}
	if nightNoteWriter("manager", "colleague", owner, head) {
		t.Fatal("another manager must not write")
	}
	if nightNoteWriter("manager", "", owner, head) {
		t.Fatal("empty actor must not write")
	}
	if nightNoteWriter("rop", head, owner, "") {
		t.Fatal("missing department head is not a writer")
	}
}
