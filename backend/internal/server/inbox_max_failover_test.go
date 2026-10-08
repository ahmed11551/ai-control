package server

import "testing"

func TestPickFreeWorkingMaxSkipsSecondProfile(t *testing.T) {
	profiles := []wappiProfile{
		{ProfileID: "aaaaaaaa-aaaa-bound", Phone: "79994447821", Authorized: true, AppStatus: "open", Platform: "max"},
		{ProfileID: "bbbbbbbb-bbbb-free2", Phone: "79994447821", Authorized: true, AppStatus: "open", Platform: "max"},
		{ProfileID: "cccccccc-cccc-ok", Phone: "79994448125", Authorized: true, AppStatus: "open", Platform: "max"},
		{ProfileID: "dddddddd-dddd-off", Phone: "79994447863", Authorized: false, AppStatus: "open", Platform: "max"},
	}
	got, ok := pickFreeWorkingMax(profiles, []string{"aaaaaaaa-aaaa"}, []string{"79994447821"})
	if !ok {
		t.Fatal("expected a free working profile")
	}
	if got.ProfileID != "cccccccc-cccc-ok" {
		t.Fatalf("got %s, want the unbound phone", got.ProfileID)
	}
}

func TestPickFreeWorkingMaxPhoneBoundWithoutMatchingRef(t *testing.T) {
	profiles := []wappiProfile{
		{ProfileID: "11111111-1111-second", Phone: "79014681493", Authorized: true, AppStatus: "open", Platform: "max"},
	}
	_, ok := pickFreeWorkingMax(profiles, nil, []string{"MAX · 79014681493"})
	if ok {
		t.Fatal("second profile of a bound phone must not be issued")
	}
}

func TestMaxSendFailureClass(t *testing.T) {
	if !maxAccountSendDead("Нет доступа к профилю мессенджера — проверьте токен линии") {
		t.Fatal("auth failure is the line")
	}
	if !maxClientSendMiss("Сообщение не доставлено в MAX: клиент не найден у провайдера") {
		t.Fatal("missing client is not the line")
	}
	if maxAccountSendDead("Сообщение не доставлено в MAX: клиент не найден у провайдера") && !maxClientSendMiss("Сообщение не доставлено в MAX: клиент не найден у провайдера") {
		t.Fatal("client miss must win")
	}
	if !maxClientSendMiss("Контакт не зарегистрирован в MAX.") {
		t.Fatal("unregistered contact is not a dead line")
	}
	if maxColdNotice("79014681493") == "" {
		t.Fatal("cold notice")
	}
	if maxDownNotice("79014681493", "", "79994448125", true) == "" {
		t.Fatal("notice")
	}
	if phoneTail4("79014681493") != "1493" {
		t.Fatal("tail")
	}
}
