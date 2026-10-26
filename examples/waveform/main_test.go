package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/chart/plot"
)

// The demo's last two channels are the same wave in millivolts, on the
// right axis.
func TestSampleIsMillivoltsOnTheLastChannels(t *testing.T) {
	for i := 0; i < 1_000; i += 97 {
		if got, want := sample(i, 2), wave(i, 2)*1000; got != want {
			t.Fatalf("channel 3 sample %d = %g, want %g", i, got, want)
		}
		if got, want := sample(i, 0), wave(i, 0); got != want {
			t.Fatalf("channel 1 sample %d = %g, want %g", i, got, want)
		}
	}
}

// Save PNG writes a picture of the chart where it is told to.
func TestSaveWritesAPNG(t *testing.T) {
	ch := &plot.Chart{Height: 200,
		Series: []*plot.Series{{Y: []float32{0, 1, 0, 1}, X0: 0, Dx: 1}}}
	dir := t.TempDir()
	msg := save(ch, dir)
	if !strings.HasPrefix(msg, "saved ") {
		t.Fatalf("save said %q", msg)
	}
	b, err := os.ReadFile(filepath.Join(dir, "waveform.png"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 1600 || img.Bounds().Dy() != 900 {
		t.Fatalf("the picture is %v, want 1600x900", img.Bounds())
	}
}
