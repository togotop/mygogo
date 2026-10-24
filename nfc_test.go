//go:build !js

package mygo

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// TestNFCEventDecode checks the tag the platform hands over: serial, tech
// list, and NDEF records with their payloads intact.
func TestNFCEventDecode(t *testing.T) {
	const raw = `{"kind":"tag","id":"04a2b3c4","tech":["Ndef","NfcA"],"writable":true,` +
		`"records":[` +
		`{"type":"text","text":"movie night","language":"en","raw":"aGk="},` +
		`{"type":"uri","uri":"https://example.com/scene","raw":""}]}`
	var event nfcEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatalf("decode: %v", err)
	}
	tag := event.tag()
	if tag.ID != "04a2b3c4" {
		t.Errorf("ID = %q", tag.ID)
	}
	if len(tag.Tech) != 2 || tag.Tech[1] != "NfcA" {
		t.Errorf("Tech = %v", tag.Tech)
	}
	if !tag.Writable {
		t.Error("Writable = false")
	}
	if len(tag.Records) != 2 {
		t.Fatalf("records = %+v", tag.Records)
	}
	if tag.Records[0].Type != "text" || tag.Records[0].Text != "movie night" {
		t.Errorf("text record = %+v", tag.Records[0])
	}
	if string(tag.Records[0].Raw) != "hi" {
		t.Errorf("raw = %q", tag.Records[0].Raw)
	}
	if tag.Records[1].URI != "https://example.com/scene" {
		t.Errorf("uri record = %+v", tag.Records[1])
	}
}

// TestNFCTransceiveArguments checks the arguments that never reach the
// platform, and the answer when the platform has no reader.
func TestNFCTransceiveArguments(t *testing.T) {
	if _, err := NFC.Transceive("NfcA", nil); err == nil {
		t.Error("Transceive accepted an empty command")
	}
	if _, err := NFC.Transceive("NfcA", []byte{0x30, 0x00}); !errors.Is(err, platform.ErrUnsupported) {
		t.Errorf("Transceive = %v, want ErrUnsupported", err)
	}
}

// TestNFCEmulateArguments checks the handler and AID validation, and that a
// platform without a reader says so rather than hanging.
func TestNFCEmulateArguments(t *testing.T) {
	if _, err := NFC.Emulate(HCEOptions{}, nil); err == nil {
		t.Error("Emulate accepted a nil handler")
	}
	if _, err := NFC.Emulate(HCEOptions{AIDs: []string{"F04D"}}, func([]byte) []byte { return nil }); !errors.Is(err, platform.ErrUnsupported) {
		t.Errorf("Emulate = %v, want ErrUnsupported", err)
	}
	if _, err := NFC.Emulate(HCEOptions{AIDs: []string{"not-hex"}}, func([]byte) []byte { return nil }); err == nil {
		t.Error("Emulate accepted an AID that is not hex")
	}
}

// TestNFCWithoutAReader checks the desktop answer, and that a failed start
// leaves the reader free for the next attempt.
func TestNFCWithoutAReader(t *testing.T) {
	if NFC.Available() {
		t.Error("Available on a platform without a reader")
	}
	for i := 0; i < 2; i++ {
		if _, err := NFC.Scan(func(NFCTag) {}); !errors.Is(err, platform.ErrUnsupported) {
			t.Fatalf("Scan #%d = %v, want ErrUnsupported", i+1, err)
		}
	}
	if err := NFC.WriteText("movie night"); !errors.Is(err, platform.ErrUnsupported) {
		t.Errorf("WriteText = %v, want ErrUnsupported", err)
	}
	if err := NFC.WriteText("  "); err == nil {
		t.Error("WriteText accepted empty text")
	}
}
