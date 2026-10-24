package mygo

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// NFCRecord is one record on a tag. Type says what the rest holds: "text"
// fills Text and Language, "uri" and "external" fill URI, "mime" fills MIME,
// and Raw always carries the payload bytes.
type NFCRecord struct {
	Type     string
	Text     string
	URI      string
	MIME     string
	Language string
	Raw      []byte
}

// NFCTag is a tag held against the phone. ID is the serial number in hex, Tech
// lists the technologies it answers to, and Records is its NDEF message, which
// is empty for a tag that holds none.
type NFCTag struct {
	ID       string
	Tech     []string
	Writable bool
	Records  []NFCRecord
}

// NFCModule reads tags held against the phone and writes text to them, which
// is how a tag becomes a button: "movie night", "good night".
//
// Reading is Android's foreground reader mode: the phone must be in the app,
// and the reader stops when the caller stops it or the activity goes away.
// Desktop backends have no reader and answer platform.ErrUnsupported.
type NFCModule struct{}

// NFC is the phone's NFC reader.
var NFC = &NFCModule{}

var (
	// ErrNFCTagReadOnly is a tag that cannot be written.
	ErrNFCTagReadOnly = errors.New("mygo: the NFC tag is read-only")
	// ErrNFCWrite is a write that failed: most often the tag left the field
	// between the read and the write.
	ErrNFCWrite = errors.New("mygo: the NFC tag could not be written")
	// ErrNFCTransceive is a raw command the tag did not answer.
	ErrNFCTransceive = errors.New("mygo: the NFC command failed")
	// ErrHCECouldNotStart is card emulation the system refused.
	ErrHCECouldNotStart = errors.New("mygo: the card emulation could not start")
)

// nfcPoll is how often Scan asks the platform for a tag: the Java side
// collects them, and this side drains them.
const nfcPoll = 120 * time.Millisecond

// hcePoll is how often the emulation asks for a waiting APDU: a reader waits
// about five seconds for an answer, so a few milliseconds of lag are free.
const hcePoll = 50 * time.Millisecond

// nfcEvent is one queued tag.
type nfcEvent struct {
	Kind     string   `json:"kind"`
	ID       string   `json:"id"`
	Tech     []string `json:"tech"`
	Writable bool     `json:"writable"`
	Records  []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		URI      string `json:"uri"`
		MIME     string `json:"mime"`
		Language string `json:"language"`
		Raw      string `json:"raw"`
	} `json:"records"`
}

// tag turns the event into the public type.
func (e nfcEvent) tag() NFCTag {
	tag := NFCTag{
		ID:       e.ID,
		Tech:     append([]string(nil), e.Tech...),
		Writable: e.Writable,
		Records:  make([]NFCRecord, 0, len(e.Records)),
	}
	for _, r := range e.Records {
		raw, err := base64.StdEncoding.DecodeString(r.Raw)
		if err != nil {
			raw = nil
		}
		tag.Records = append(tag.Records, NFCRecord{
			Type:     r.Type,
			Text:     r.Text,
			URI:      r.URI,
			MIME:     r.MIME,
			Language: r.Language,
			Raw:      raw,
		})
	}
	return tag
}

// reading guards the single foreground reader, emulating the single card
// emulation.
var (
	nfcReading struct {
		mu     sync.Mutex
		active bool
	}
	nfcEmulating struct {
		mu     sync.Mutex
		active bool
	}
)

// Available reports whether the device has an NFC adapter, which is not the
// same as it being switched on.
func (n *NFCModule) Available() bool {
	needsApp("NFC.Available")
	available := false
	onMain(func() {
		if reader, ok := backend().Mobile().(platform.NFCReader); ok {
			available = reader.NFCAvailable()
		}
	})
	return available
}

// Scan reads tags until stop runs. fn is called on a background goroutine for
// each tag held against the phone, so it may block.
//
// The reader runs only while the app is in front: the system turns it off with
// the activity, and stop is how the app says it is done with it.
func (n *NFCModule) Scan(fn func(NFCTag)) (stop func(), err error) {
	needsApp("NFC.Scan")
	if fn == nil {
		return nil, errors.New("mygo: NFC.Scan needs a callback")
	}
	nfcReading.mu.Lock()
	if nfcReading.active {
		nfcReading.mu.Unlock()
		return nil, errors.New("mygo: the NFC reader is already open")
	}
	nfcReading.active = true
	nfcReading.mu.Unlock()

	var startErr error
	onMain(func() {
		reader, ok := backend().Mobile().(platform.NFCReader)
		if !ok {
			startErr = platform.ErrUnsupported
			return
		}
		switch rc := reader.NFCScanStart(); rc {
		case 0:
		case 2:
			startErr = platform.ErrUnsupported
		default:
			startErr = errors.New("mygo: the NFC reader could not start")
		}
	})
	if startErr != nil {
		nfcReading.mu.Lock()
		nfcReading.active = false
		nfcReading.mu.Unlock()
		return nil, startErr
	}

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			var raw string
			var ok bool
			onMain(func() {
				if reader, isReader := backend().Mobile().(platform.NFCReader); isReader {
					raw, ok = reader.TakeNFCEvent()
				}
			})
			if ok {
				var event nfcEvent
				if json.Unmarshal([]byte(raw), &event) == nil && event.Kind == "tag" {
					fn(event.tag())
				}
			}
			pauseMain(nfcPoll)
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			nfcReading.mu.Lock()
			nfcReading.active = false
			nfcReading.mu.Unlock()
			onMain(func() {
				if reader, ok := backend().Mobile().(platform.NFCReader); ok {
					reader.NFCScanStop()
				}
			})
		})
	}, nil
}

// Transceive sends one raw command to the tag in the field and returns its
// answer, for tags that speak something other than NDEF: a MIFARE command, an
// ISO-DEP APDU, a Felica request, a Type 2 page read.
//
// tech names the technology to speak through — "NfcA", "NfcB", "NfcF", "NfcV",
// "IsoDep", "MifareUltralight" or "MifareClassic" — and empty means "NfcA". It
// has to be one the tag answered with (see NFCTag.Tech). The tag is the one
// Scan last read, so call this from inside its callback or before the tag
// leaves the field.
func (n *NFCModule) Transceive(tech string, data []byte) ([]byte, error) {
	needsApp("NFC.Transceive")
	if tech == "" {
		tech = "NfcA"
	}
	if len(data) == 0 {
		return nil, errors.New("mygo: NFC.Transceive needs data")
	}
	payload := jsonPayload(map[string]any{"tech": tech, "data": hex.EncodeToString(data)})
	var answer []byte
	var err error
	// Not on the main thread: a tag that does not answer holds the call for
	// the technology's timeout, and that must not freeze the UI.
	if reader, ok := backend().Mobile().(platform.NFCReader); ok {
		raw, got := reader.Transceive(payload)
		switch {
		case !got:
			err = ErrNFCTransceive
		default:
			var event struct {
				Data string `json:"data"`
			}
			if json.Unmarshal([]byte(raw), &event) != nil {
				err = ErrNFCTransceive
			} else if answer, err = hex.DecodeString(event.Data); err != nil {
				err = ErrNFCTransceive
			}
		}
	} else {
		err = platform.ErrUnsupported
	}
	return answer, err
}

// HCEOptions configures card emulation.
type HCEOptions struct {
	// AIDs are the application identifiers this app answers for, in hex,
	// "F04D59474F3031". Empty leaves the ones declared in apduservice.xml,
	// which answer nothing until Emulate runs.
	AIDs []string
	// Timeout bounds how long a reader waits for the handler: zero means
	// five seconds, which is about what a payment terminal tolerates.
	Timeout time.Duration
}

// Emulate makes the phone answer APDUs as a card would, until stop runs: the
// other half of NFC, for a door, a locker, or a toy that expects a card rather
// than a tag.
//
// handle runs on a background goroutine for each command and returns the
// response bytes, normally data followed by a status word such as 0x90 0x00.
// Returning nil answers 6F00, "no precise diagnosis". A response that takes
// longer than the timeout does the same.
//
// Android routes to the app only while it is in front (Emulate sets it as the
// preferred service), and only for the AIDs it registered.
func (n *NFCModule) Emulate(o HCEOptions, handle func(apdu []byte) []byte) (stop func(), err error) {
	needsApp("NFC.Emulate")
	if handle == nil {
		return nil, errors.New("mygo: NFC.Emulate needs a handler")
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	aids := make([]string, 0, len(o.AIDs))
	for _, aid := range o.AIDs {
		aid = strings.ToUpper(strings.TrimSpace(aid))
		if aid == "" {
			continue
		}
		if _, err := hex.DecodeString(aid); err != nil {
			return nil, fmt.Errorf("mygo: the AID %q is not hex", aid)
		}
		aids = append(aids, aid)
	}
	nfcEmulating.mu.Lock()
	if nfcEmulating.active {
		nfcEmulating.mu.Unlock()
		return nil, errors.New("mygo: the card emulation is already open")
	}
	nfcEmulating.active = true
	nfcEmulating.mu.Unlock()

	var startErr error
	var reader platform.NFCReader
	onMain(func() {
		found, ok := backend().Mobile().(platform.NFCReader)
		if !ok {
			startErr = platform.ErrUnsupported
			return
		}
		reader = found
	})
	if startErr != nil {
		nfcEmulating.mu.Lock()
		nfcEmulating.active = false
		nfcEmulating.mu.Unlock()
		return nil, startErr
	}
	payload := jsonPayload(map[string]any{"aids": aids, "timeout": timeout.Milliseconds()})
	switch rc := reader.HCEStart(payload); rc {
	case 0:
	case 2:
		startErr = platform.ErrUnsupported
	default:
		startErr = ErrHCECouldNotStart
	}
	if startErr != nil {
		nfcEmulating.mu.Lock()
		nfcEmulating.active = false
		nfcEmulating.mu.Unlock()
		return nil, startErr
	}

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			raw, ok := reader.TakeHCERequest()
			if !ok {
				pauseMain(hcePoll)
				continue
			}
			var request struct {
				ID   int64  `json:"id"`
				APDU string `json:"apdu"`
			}
			if json.Unmarshal([]byte(raw), &request) != nil {
				continue
			}
			apdu, err := hex.DecodeString(request.APDU)
			if err != nil {
				continue
			}
			var answer []byte
			if response := handle(apdu); response != nil {
				answer = response
			}
			_ = reader.HCERespond(jsonPayload(map[string]any{
				"id":   request.ID,
				"data": hex.EncodeToString(answer),
			}))
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			nfcEmulating.mu.Lock()
			nfcEmulating.active = false
			nfcEmulating.mu.Unlock()
			reader.HCEStop()
		})
	}, nil
}

// WriteText writes one text record to the tag in the field. It has to happen
// while the tag is still there, so an app writes from inside Scan's callback
// after deciding what the tag should say.
func (n *NFCModule) WriteText(text string) error {
	needsApp("NFC.WriteText")
	if strings.TrimSpace(text) == "" {
		return errors.New("mygo: NFC.WriteText needs text")
	}
	payload := jsonPayload(map[string]any{"text": text})
	var err error
	onMain(func() {
		reader, ok := backend().Mobile().(platform.NFCReader)
		if !ok {
			err = platform.ErrUnsupported
			return
		}
		switch rc := reader.NFCWriteText(payload); rc {
		case 0:
			return
		case 2:
			err = platform.ErrUnsupported
		case 4:
			err = ErrNFCTagReadOnly
		default:
			err = ErrNFCWrite
		}
	})
	return err
}
