package platform

import (
	"math"
	"sync/atomic"
)

// fontScale is the user's text size preference: Android's fontScale, 1 by
// default. Text sizes multiply by it while lengths, paddings and borders do
// not, the same line Android draws between sp and dp. Other platforms leave it
// at 1.
var fontScale atomic.Uint32

// SetFontScale records the user's text size preference. A value of 1 or less,
// or one that is not a number, is ignored.
func SetFontScale(scale float32) {
	if scale <= 0 || math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) {
		return
	}
	fontScale.Store(math.Float32bits(scale))
}

// FontScale is the text size multiplier, 1 by default.
func FontScale() float32 {
	bits := fontScale.Load()
	if bits == 0 {
		return 1
	}
	return math.Float32frombits(bits)
}
