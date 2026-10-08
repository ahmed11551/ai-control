package server

import "testing"

func TestInboxMessageHasMediaSkipsText(t *testing.T) {
	if inboxMessageHasMedia("", "") || inboxMessageHasMedia("  ", "  ") {
		t.Fatal("текст не должен считаться вложением")
	}
	if mediaWappiID(inboxEv{MediaMsgID: "msg-1", Body: "привет"}) != "" {
		t.Fatal("у текста нельзя сохранять id для media/download")
	}
	if !inboxMessageHasMedia("image", "") {
		t.Fatal("картинка должна скачиваться")
	}
	if mediaWappiID(inboxEv{MediaKind: "image", MediaMsgID: "msg-2"}) != "msg-2" {
		t.Fatal("у картинки id для скачивания должен сохраниться")
	}
	if !inboxMessageHasMedia("", "https://example/a.jpg") {
		t.Fatal("сообщение со ссылкой на файл должно скачиваться")
	}
}

func TestMediaFetchGateSingleFlightAndCooldown(t *testing.T) {
	var g mediaFetchGate

	if !g.begin("m1") {
		t.Fatal("первая закачка должна стартовать")
	}
	if g.begin("m1") {
		t.Fatal("повтор той же закачки не должен стартовать, пока первая идёт")
	}
	if !g.begin("m2") {
		t.Fatal("другое сообщение не должно ждать первое")
	}

	g.end("m1", false)
	if !g.cooling("m1") {
		t.Fatal("после неудачи нужна пауза, чтобы опрос чата не качал файл снова")
	}
	g.end("m2", true)
	if g.cooling("m2") {
		t.Fatal("успешная закачка не должна ставить паузу")
	}

	// Новый вебхук всё ещё может начать закачку: пауза проверяется только в kick.
	if !g.begin("m1") {
		t.Fatal("явный старт после паузы должен проходить")
	}
	g.end("m1", true)
	if g.cooling("m1") {
		t.Fatal("успех снимает паузу")
	}
}
