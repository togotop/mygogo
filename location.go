package mygo

import "time"

// Position is where the device is, as the system reports it.
type Position struct {
	Latitude  float64
	Longitude float64
	// Accuracy is the radius the system gives, in metres.
	Accuracy float64
	// Time is when the fix was taken; the zero time means unknown.
	Time time.Time
}

// GeolocationModule reads the device's position. Ask for the geolocation
// permission first: without it there is no fix.
type GeolocationModule struct{}

// Geolocation is the device's position.
var Geolocation = &GeolocationModule{}

// Current returns the last position the system recorded. It does not start a
// location request, so the answer can be stale; Time says when it was taken.
// ErrUnsupported means there is no fix at all, or the app may not read one.
func (m *GeolocationModule) Current() (Position, error) {
	needsApp("Geolocation.Current")
	var (
		pos Position
		err error
	)
	onMain(func() {
		p, e := backend().Mobile().Location()
		if e != nil {
			err = e
			return
		}
		pos = Position{
			Latitude:  p.Latitude,
			Longitude: p.Longitude,
			Accuracy:  p.Accuracy,
			Time:      p.Time,
		}
	})
	return pos, err
}
