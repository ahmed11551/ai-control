package server

import "testing"

func TestVoxRecordingKey(t *testing.T) {
	url := "https://cdn.voximplant.com/records/foo.mp3"
	key := voxRecordingKey(url)
	if key != "vox:"+url {
		t.Fatalf("key=%q", key)
	}
	if parseVoxRecordingKey(key) != url {
		t.Fatalf("parse=%q", parseVoxRecordingKey(key))
	}
	if parseVoxRecordingKey("exolve:123") != "" {
		t.Fatal("exolve must not parse as vox")
	}
}

func TestRecordingURLFromHook(t *testing.T) {
	raw := map[string]any{
		"event":         "recording",
		"recording_url": "https://cdn.voximplant.com/x.mp3",
		"from":          "79001112233",
		"to":            "79014173381",
	}
	if got := recordingURLFromHook(raw); got != "https://cdn.voximplant.com/x.mp3" {
		t.Fatalf("got %q", got)
	}
	if !hookLooksLikeVoxRecording(raw) {
		t.Fatal("expected recording hook")
	}
	peers := hookPeerPhones(raw)
	if len(peers) < 1 {
		t.Fatalf("peers=%v", peers)
	}
}
