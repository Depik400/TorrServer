package core

import (
	"strings"
	"testing"
)

func TestParseTorrentLinkMagnet(t *testing.T) {
	const infohash = "26785c97a6cbe9e5d18924e52574ece0fafdff9c"
	link := "magnet:?xt=urn:btih:" + infohash + "&dn=TestFile"

	spec, err := parseTorrentLink(link)
	if err != nil {
		t.Fatalf("parseTorrentLink failed: %v", err)
	}
	if spec.InfoHash.HexString() != infohash {
		t.Errorf("InfoHash: got %q, want %q", spec.InfoHash.HexString(), infohash)
	}
	if spec.DisplayName != "TestFile" {
		t.Errorf("DisplayName: got %q, want TestFile", spec.DisplayName)
	}
}

func TestParseTorrentLinkInfohash(t *testing.T) {
	const infohash = "26785c97a6cbe9e5d18924e52574ece0fafdff9c"

	spec, err := parseTorrentLink(infohash)
	if err != nil {
		t.Fatalf("parseTorrentLink failed: %v", err)
	}
	if spec.InfoHash.HexString() != infohash {
		t.Errorf("InfoHash: got %q, want %q", spec.InfoHash.HexString(), infohash)
	}
}

func TestParseTorrentLinkUrnBtih(t *testing.T) {
	const infohash = "26785c97a6cbe9e5d18924e52574ece0fafdff9c"
	link := "urn:btih:" + infohash

	spec, err := parseTorrentLink(link)
	if err != nil {
		t.Fatalf("parseTorrentLink failed: %v", err)
	}
	if spec.InfoHash.HexString() != infohash {
		t.Errorf("InfoHash: got %q, want %q", spec.InfoHash.HexString(), infohash)
	}
}

func TestParseTorrentLinkWithTrackers(t *testing.T) {
	const infohash = "26785c97a6cbe9e5d18924e52574ece0fafdff9c"
	link := "magnet:?xt=urn:btih:" + infohash + "&tr=udp://tracker.example.com:80"

	spec, err := parseTorrentLink(link)
	if err != nil {
		t.Fatalf("parseTorrentLink failed: %v", err)
	}
	if spec.InfoHash.HexString() != infohash {
		t.Errorf("InfoHash: got %q, want %q", spec.InfoHash.HexString(), infohash)
	}
	if len(spec.Trackers) == 0 {
		t.Error("expected trackers in spec")
	}
}

func TestParseTorrentLinkInvalid(t *testing.T) {
	_, err := parseTorrentLink("not a valid link at all")
	if err == nil {
		t.Fatal("expected error for invalid link")
	}
}

func TestParseTorrentLinkEmpty(t *testing.T) {
	_, err := parseTorrentLink("")
	if err == nil {
		t.Fatal("expected error for empty link")
	}
}

func TestParseTorrentLinkShortInfohash(t *testing.T) {
	_, err := parseTorrentLink("26785c97a6cbe9e5d18924e52574ece0")
	if err == nil {
		t.Fatal("expected error for short infohash (< 40 chars)")
	}
}

func TestParseTorrentLinkWhitespaceTrim(t *testing.T) {
	const infohash = "26785c97a6cbe9e5d18924e52574ece0fafdff9c"
	link := "\t " + infohash + "\n "

	spec, err := parseTorrentLink(link)
	if err != nil {
		t.Fatalf("parseTorrentLink with whitespace failed: %v", err)
	}
	if spec.InfoHash.HexString() != infohash {
		t.Errorf("InfoHash: got %q, want %q", spec.InfoHash.HexString(), infohash)
	}
}

func TestEngineStateConstants(t *testing.T) {
	states := []EngineState{
		EngineStopped,
		EngineStarting,
		EngineRunning,
		EngineStopping,
		EngineFailed,
	}

	for _, s := range states {
		if strings.TrimSpace(string(s)) == "" {
			t.Errorf("empty EngineState constant found")
		}
	}

	if EngineStopped != "stopped" {
		t.Errorf("EngineStopped: got %q", EngineStopped)
	}
	if EngineRunning != "running" {
		t.Errorf("EngineRunning: got %q", EngineRunning)
	}
}

func TestSessionStateConstants(t *testing.T) {
	states := []SessionState{
		SessionPreparing,
		SessionReady,
		SessionStreaming,
		SessionGrace,
		SessionCompleted,
		SessionCancelled,
		SessionFailed,
	}

	for _, s := range states {
		if strings.TrimSpace(string(s)) == "" {
			t.Errorf("empty SessionState constant found")
		}
	}

	if SessionCancelled != "cancelled" {
		t.Errorf("SessionCancelled: got %q", SessionCancelled)
	}
	if SessionCompleted != "completed" {
		t.Errorf("SessionCompleted: got %q", SessionCompleted)
	}
}
