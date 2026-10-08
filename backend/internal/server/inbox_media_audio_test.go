package server

import "testing"

func TestIsInboxAudioKind(t *testing.T) {
	for _, k := range []string{"audio", "voice", "ptt", "VOICE"} {
		if !isInboxAudioKind(k) {
			t.Fatalf("%q must be audio", k)
		}
	}
	for _, k := range []string{"image", "document", "file", "video", "sticker", "passport_text", "", "application/pdf"} {
		if isInboxAudioKind(k) {
			t.Fatalf("%q must not be audio", k)
		}
	}
}

func TestInboxFileIsAudio(t *testing.T) {
	ogg := map[string]any{"kind": "audio", "filename": "voice.ogg", "mime": "audio/ogg"}
	if !inboxFileIsAudio(ogg, "audio") {
		t.Fatal("ogg voice must play")
	}
	if inboxFileIsAudio(ogg, "image") {
		t.Fatal("image message must not play as audio")
	}
	pdf := map[string]any{"kind": "audio", "filename": "scan.pdf", "mime": "application/pdf"}
	if inboxFileIsAudio(pdf, "audio") {
		t.Fatal("pdf mislabeled as audio must not play")
	}
	jpg := map[string]any{"kind": "image", "filename": "a.jpg", "mime": "image/jpeg"}
	if inboxFileIsAudio(jpg, "audio") {
		t.Fatal("jpeg must not play")
	}
	bare := map[string]any{"filename": "b5aa1d66.ogg", "mime": ""}
	if !inboxFileIsAudio(bare, "voice") {
		t.Fatal("ogg filename with voice kind must play")
	}
}
