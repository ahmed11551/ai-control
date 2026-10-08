package server

import "testing"

func TestOwnedSessionWorkingIgnoresDeadLine(t *testing.T) {
	lines := []maxBoardLine{{Ref: "aaaaaaaa-aaaa", Phone: "79014681493"}}
	dead := []wappiProfile{{ProfileID: "aaaaaaaa-aaaa-bound", Phone: "79014681493", Authorized: false, AppStatus: "close", Platform: "max"}}
	if ownedSessionWorking(lines, dead) {
		t.Fatal("dead session is not a working number")
	}
	live := []wappiProfile{{ProfileID: "aaaaaaaa-aaaa-bound", Phone: "79014681493", Authorized: true, AppStatus: "open", Platform: "max"}}
	if !ownedSessionWorking(lines, live) {
		t.Fatal("open session counts as working")
	}
}

func TestBuildMaxPhoneRowsSecondProfileStaysBound(t *testing.T) {
	profiles := []wappiProfile{
		{ProfileID: "aaaaaaaa-aaaa-bound", Phone: "79014681493", Authorized: true, AppStatus: "open", Platform: "max"},
		{ProfileID: "bbbbbbbb-bbbb-free2", Phone: "79014681493", Authorized: true, AppStatus: "open", Platform: "max"},
		{ProfileID: "cccccccc-cccc-ok", Phone: "79994448125", Authorized: true, AppStatus: "open", Platform: "max"},
	}
	lines := []maxBoardLine{{
		Ref: "aaaaaaaa-aaaa", Phone: "79014681493", OwnerID: "user-1", OwnerName: "Русин",
	}}
	rows := buildMaxPhoneRows(profiles, lines, nil)
	if len(rows) != 2 {
		t.Fatalf("rows %d", len(rows))
	}
	var bound, free maxPhoneRow
	for _, row := range rows {
		if row.Phone == "79014681493" {
			bound = row
		}
		if row.Phone == "79994448125" {
			free = row
		}
	}
	if !bound.Bound || bound.Bucket != "bound_ok" || !bound.ExtraProfile || bound.Profiles != 2 {
		t.Fatalf("bound row %+v", bound)
	}
	if free.Bound || free.Bucket != "free" {
		t.Fatalf("free row %+v", free)
	}
}

func TestBuildMaxPhoneRowsColdAndDown(t *testing.T) {
	profiles := []wappiProfile{
		{ProfileID: "11111111-1111-live", Phone: "79014683030", Authorized: true, AppStatus: "open", Platform: "max"},
		{ProfileID: "22222222-2222-off", Phone: "79014692527", Authorized: false, AppStatus: "close", Platform: "max"},
	}
	lines := []maxBoardLine{
		{Ref: "11111111-1111", Phone: "79014683030", OwnerID: "u", OwnerName: "А", Cold: true},
		{Ref: "22222222-2222", Phone: "79014692527", OwnerID: "u", OwnerName: "А", SendDown: true},
	}
	rows := buildMaxPhoneRows(profiles, lines, nil)
	by := map[string]maxPhoneRow{}
	for _, row := range rows {
		by[row.Phone] = row
	}
	if by["79014683030"].Bucket != "cold" || by["79014683030"].CanNew {
		t.Fatalf("cold %+v", by["79014683030"])
	}
	if by["79014692527"].Bucket != "down" {
		t.Fatalf("down %+v", by["79014692527"])
	}
	nums, ok := maxPersonNumbers(rows, "u")
	if ok || len(nums) != 2 {
		t.Fatalf("person nums=%v ok=%v", nums, ok)
	}
}

func TestClassifyMaxContactCheck(t *testing.T) {
	if classifyMaxContactCheck("", map[string]any{"on_max": true}) != "ok" {
		t.Fatal("on_max true")
	}
	if classifyMaxContactCheck("", map[string]any{"on_max": false}) != "no" {
		t.Fatal("on_max false")
	}
	if classifyMaxContactCheck("Wappi 401: unauthorized", nil) != "unauthorized" {
		t.Fatal("unauthorized")
	}
	if classifyMaxContactCheck("CHECK_PHONE_FLOODWAIT", nil) != "flood" {
		t.Fatal("flood")
	}
	if classifyMaxContactCheck("", map[string]any{"on_max": false, "detail": "не авторизован"}) != "unauthorized" {
		t.Fatal("detail unauthorized")
	}
}

func TestPhoneGroupCheckPrefersBoundProfile(t *testing.T) {
	profiles := []wappiProfile{
		{ProfileID: "aaaaaaaa-aaaa-bound", Phone: "79014681493", Authorized: true, AppStatus: "open", Platform: "max"},
		{ProfileID: "bbbbbbbb-bbbb-free2", Phone: "79014681493", Authorized: true, AppStatus: "open", Platform: "max"},
		{ProfileID: "cccccccc-cccc-ok", Phone: "79994448125", Authorized: true, AppStatus: "open", Platform: "max"},
	}
	lines := []maxBoardLine{{
		Ref: "aaaaaaaa-aaaa", Phone: "79014681493", OwnerID: "user-1", OwnerName: "Русин",
	}}
	checks := map[string]string{
		"aaaaaaaa-aaaa": "no",
		"bbbbbbbb-bbbb": "ok",
		"cccccccc-cccc": "ok",
	}
	rows := buildMaxPhoneRows(profiles, lines, checks)
	by := map[string]maxPhoneRow{}
	for _, row := range rows {
		by[row.Phone] = row
	}
	if by["79014681493"].Check != "no" || by["79014681493"].Bucket != "cold" {
		t.Fatalf("bound uses its own check %+v", by["79014681493"])
	}
	if by["79994448125"].Check != "ok" || by["79994448125"].Bucket != "free" {
		t.Fatalf("free ok %+v", by["79994448125"])
	}
}
