// Package nfc reads the NFC tags a phone is held against, and writes them:
// Android's reader mode, with the system's own sound and vibration.
//
//	stop, err := nfc.Scan(ctx, func(t nfc.Tag) { ... })
//	defer stop()
//	err = nfc.WriteText(ctx, "https://example.com") // to the tag last seen
//
// Elsewhere its functions report errors.ErrUnsupported.
package nfc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/egoist/mygo"
)

const pluginID = "nfc"

// Record is one record of a tag's NDEF message. Kind is "text", "uri", "mime"
// or "raw": Text carries a text record's text, URI its URI, MIME a record's
// type and Data its bytes.
type Record struct {
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
	URI  string `json:"uri,omitempty"`
	MIME string `json:"mime,omitempty"`
	Data []byte `json:"data,omitempty"`
}

// Tag is a tag the phone was held against: its ID, the technologies it
// answers on, and what it holds.
type Tag struct {
	ID      string
	Techs   []string
	Records []Record
}

// ErrNoTag is WriteText or Transceive with no tag seen yet: hold the phone
// against one first.
var ErrNoTag = errors.New("nfc: no tag seen yet")

// Available reports whether the device has an NFC radio that is on.
func Available(ctx context.Context) (bool, error) {
	var r struct {
		Available bool `json:"available"`
	}
	if err := mygo.Native(pluginID).Call(ctx, "available", nil, &r); err != nil {
		return false, err
	}
	return r.Available, nil
}

// Scanner hears the tags the phone is held against until Stop.
type Scanner struct {
	once sync.Once
	stop func()
}

// Stop ends the scan.
func (s *Scanner) Stop() {
	if s != nil {
		s.once.Do(func() {
			if s.stop != nil {
				s.stop()
			}
		})
	}
}

// Scan reports each tag the phone is held against to fn, until Stop. The
// callback runs on the goroutine that called Scan.
func Scan(ctx context.Context, fn func(Tag)) (*Scanner, error) {
	events := make(chan Tag, 8)
	off := mygo.Native(pluginID).On("event", func(data []byte) {
		var e struct {
			Kind    string   `json:"kind"`
			ID      string   `json:"id"`
			Techs   []string `json:"techs"`
			Records []Record `json:"records"`
		}
		if json.Unmarshal(data, &e) != nil || e.Kind != "tag" {
			return
		}
		select {
		case events <- Tag{ID: e.ID, Techs: e.Techs, Records: e.Records}:
		default:
			// A tag waits for the callback to be done with the one before it.
		}
	})
	if err := mygo.Native(pluginID).Call(ctx, "scan", nil, nil); err != nil {
		off()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case t := <-events:
				fn(t)
			case <-done:
				return
			}
		}
	}()
	return &Scanner{stop: func() {
		close(done)
		off()
		_ = mygo.Native(pluginID).Call(context.Background(), "stopScan", nil, nil)
	}}, nil
}

// WriteText writes text as the only record of the tag the phone was last held
// against: a text record, in the device's language. A tag that is not
// formatted yet is formatted. Call it while holding the phone against the tag.
func WriteText(ctx context.Context, text string) error {
	err := mygo.Native(pluginID).Call(ctx, "writeText", map[string]string{"text": text}, nil)
	if err != nil {
		var n *mygo.NativeError
		if errors.As(err, &n) && n.Code == "NoTag" {
			return ErrNoTag
		}
	}
	return err
}

// Transceive sends data to the tag last seen through one of its technologies
// ("IsoDep", "NfcA", "NfcF", "NfcV" or "NfcB"), and returns its answer: the
// exchange an app that speaks a tag's own protocol needs, as MIFARE's.
func Transceive(ctx context.Context, tech string, data []byte) ([]byte, error) {
	var r struct {
		Data string `json:"data"`
	}
	args := map[string]string{
		"tech": tech,
		"data": base64.StdEncoding.EncodeToString(data),
	}
	if err := mygo.Native(pluginID).Call(ctx, "transceive", args, &r); err != nil {
		var n *mygo.NativeError
		if errors.As(err, &n) && n.Code == "NoTag" {
			return nil, ErrNoTag
		}
		return nil, err
	}
	return base64.StdEncoding.DecodeString(strings.TrimSpace(r.Data))
}
