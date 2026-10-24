//go:build android && cgo

package backend

// The system speech recognizer over platform.SpeechRecognizer.
//
// The recognizer streams: partial transcripts replace the whole text until a
// final result or an error ends the session. Those arrive on the Java UI
// thread, and the Go side polls for them (internal/android's collectSpeechResults)
// because the bridge is one-way; the wait is a person speaking, so a poll every
// 100ms costs nothing.
//
// A device without the service reports ErrUnsupported, which the app renders as
// "voice input is not available here" rather than as a failure.

import (
	"errors"
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// errSpeechFailed is a recognizer that started and then failed for a reason it
// could only report as text, such as the service being busy.
var errSpeechFailed = errors.New("mygo: Android speech recognition failed")

// Speech returns the phone's speech recognizer.
func (b *Backend) Speech() platform.SpeechRecognizer { return androidSpeech{b: b} }

type androidSpeech struct{ b *Backend }

// speechEmits maps a recognition's token to the callback its results go to.
var speechEmits sync.Map // token → func(platform.SpeechResult)

func (androidSpeech) Start(o platform.SpeechOptions, emit func(platform.SpeechResult)) (platform.SpeechRecording, error) {
	bridge := platformBridge()
	if bridge == nil {
		return nil, ErrPlatformBridge
	}
	token := nextToken()
	speechEmits.Store(token, emit)
	if err := bridge.SpeechStart(token, o.Language, o.OnDevice, o.ContextualStrings); err != nil {
		speechEmits.Delete(token)
		return nil, err
	}
	return &androidSpeechRecording{token: token}, nil
}

// androidSpeechRecording ends one recognition, once: Stop asks for the final
// result, Cancel discards it.
type androidSpeechRecording struct {
	token uint64
	once  sync.Once
}

func (r *androidSpeechRecording) Stop() {
	r.once.Do(func() {
		if bridge := platformBridge(); bridge != nil {
			_ = bridge.SpeechStop(r.token)
		}
	})
}

func (r *androidSpeechRecording) Cancel() {
	r.once.Do(func() {
		speechEmits.Delete(r.token)
		if bridge := platformBridge(); bridge != nil {
			_ = bridge.SpeechCancel(r.token)
		}
	})
}

// SpeechTimedOut drops a recognition that never ended. The session has already
// stopped by its own duration, so there is nothing to report; this only frees
// the callback.
func SpeechTimedOut(token uint64) { speechEmits.Delete(token) }

// SpeechResult delivers one recognizer answer: code 0 a partial transcript that
// replaces the previous one, 1 a final transcript, 2 an error whose text is in
// the payload. internal/android calls it from the goroutine collecting answers.
func SpeechResult(token uint64, code int, text string) {
	v, ok := speechEmits.Load(token)
	if !ok {
		// Canceled while the recognizer was still talking: the rest is dropped,
		// which is what cancel means.
		return
	}
	emit := v.(func(platform.SpeechResult))
	switch code {
	case 0:
		emit(platform.SpeechResult{Text: text})
	case 1:
		speechEmits.Delete(token)
		emit(platform.SpeechResult{Text: text, Final: true})
	default:
		speechEmits.Delete(token)
		emit(platform.SpeechResult{Text: text, Final: true, Err: errSpeechFailed})
	}
}
