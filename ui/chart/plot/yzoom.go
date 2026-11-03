package plot

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// fitY returns the axes to fitting their series, keeping the ranges the app
// fixed: only the ones a wheel zoomed are cleared.
func (ch *Chart) fitY() {
	for k := range ch.yFit {
		if !ch.yFit[k] {
			continue
		}
		ch.yFit[k] = false
		if k < len(ch.Axes) {
			ch.Axes[k].Range = Range{}
		}
	}
}

// ensureAxis makes sure the chart has an axis at index k to write to.
func (ch *Chart) ensureAxis(k int) {
	for len(ch.Axes) <= k {
		ch.Axes = append(ch.Axes, Axis{})
	}
}

// zoomY scales the y axis under the pointer around the value there, as Alt
// and the wheel do: the left half zooms the first axis, the right the
// second. The axis becomes fixed until the view is fitted again.
func (ch *Chart) zoomY(ev ui.InputEvent, localY float64) bool {
	if ch.r.H <= 0 || !ch.vx.Valid() || ch.yHi[0] <= ch.yLo[0] {
		return false
	}
	k := 0
	if ch.axisCount() > 1 && float64(ev.X) > float64(ch.r.W)/2 {
		k = 1
	}
	lo, hi := ch.yLo[k], ch.yHi[k]
	step := float64(ev.DY)
	if ev.Precise {
		step /= 4
	}
	f := math.Pow(1.25, step/100)
	var nlo, nhi float64
	if ch.yLog[k] { // decades: the same room per decade, zoomed in decades
		l0, l1 := math.Log10(lo), math.Log10(hi)
		a := l0 + (1-localY/float64(ch.r.H))*(l1-l0)
		nlo, nhi = math.Pow(10, a-(a-l0)*f), math.Pow(10, a+(l1-a)*f)
	} else {
		a := lo + (1-localY/float64(ch.r.H))*(hi-lo)
		nlo, nhi = a-(a-lo)*f, a+(hi-a)*f
	}
	if nhi-nlo <= 0 || math.IsInf(nhi-nlo, 0) || math.IsNaN(nlo) {
		return true
	}
	ch.ensureAxis(k)
	ch.Axes[k].Range = Range{nlo, nhi}
	ch.yFit[k] = true
	return true
}
