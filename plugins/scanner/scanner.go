// Package scanner reads a barcode from a photo the app's user takes with the
// system's own camera, which needs no permission of the app: a QR code on a
// box, a ticket's barcode, a pairing code on another screen.
//
//	text, err := scanner.Scan(ctx, scanner.Options{Formats: []string{"qr"}})
//	switch {
//	case errors.Is(err, scanner.ErrCanceled): // the user went back
//	case errors.Is(err, scanner.ErrNoCode):   // the photo holds no code
//	}
//
// Elsewhere its functions report errors.ErrUnsupported.
package scanner

import (
	"context"
	"errors"

	"github.com/egoist/mygo"
)

const pluginID = "scanner"

// ErrCanceled is what Scan reports when the user backs out of the camera.
var ErrCanceled = errors.New("scanner: canceled")

// ErrNoCode is what Scan reports when the photo holds no barcode it reads.
var ErrNoCode = errors.New("scanner: no code in the photo")

// Options are what to read.
type Options struct {
	// Formats are the barcodes to look for: "qr", "ean13", "ean8", "upca",
	// "upce", "code128", "code39", "codabar", "itf", "datamatrix",
	// "pdf417" or "aztec". Empty looks for all of them, which takes the
	// longest.
	Formats []string
}

// Available reports whether the device has a camera app to take a photo with.
func Available(ctx context.Context) (bool, error) {
	var r struct {
		Available bool `json:"available"`
	}
	if err := mygo.Native(pluginID).Call(ctx, "available", nil, &r); err != nil {
		return false, err
	}
	return r.Available, nil
}

// Scan has the user take a photo with the system's camera and returns what
// the barcode in it says. Call it off the UI thread, as it waits for the
// camera.
func Scan(ctx context.Context, o Options) (string, error) {
	args := map[string]any{}
	if len(o.Formats) > 0 {
		args["Formats"] = o.Formats
	}
	var r struct {
		Text string `json:"text"`
	}
	if err := mygo.Native(pluginID).Call(ctx, "scan", args, &r); err != nil {
		var n *mygo.NativeError
		if errors.As(err, &n) {
			switch n.Code {
			case "Canceled":
				return "", ErrCanceled
			case "NoCode":
				return "", ErrNoCode
			}
		}
		return "", err
	}
	return r.Text, nil
}
