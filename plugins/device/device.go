// Package device tells what the app runs on: the maker and model of the
// hardware, and the system's name and version. An app that prints a bug
// report, or adapts to a maker's quirks, asks it.
//
//	info, err := device.Current(ctx)
//	log.Printf("%s %s, %s %s", info.Manufacturer, info.Model, info.System, info.SystemVersion)
//
// Elsewhere its functions report errors.ErrUnsupported.
package device

import (
	"context"

	"github.com/egoist/mygo"
)

const pluginID = "device"

// Info is what the app runs on.
type Info struct {
	// Manufacturer is who made the hardware, "Google", "Samsung"; a desktop
	// may leave it empty.
	Manufacturer string `json:"manufacturer"`
	// Model is what the hardware is called, "Pixel 8", "iPhone15,2".
	Model string `json:"model"`
	// System is the OS's name, "Android", "iOS"; a desktop says which one
	// it is, as "linux".
	System string `json:"system"`
	// SystemVersion is the OS's version, "14", "17.5".
	SystemVersion string `json:"systemVersion"`
	// SDK is the API level, on Android: 26 for Android 8, 35 for Android
	// 15. Other platforms leave it 0.
	SDK int `json:"sdk"`
}

// Current tells what the app runs on.
func Current(ctx context.Context) (Info, error) {
	var r Info
	if err := mygo.Native(pluginID).Call(ctx, "info", nil, &r); err != nil {
		return Info{}, err
	}
	return r, nil
}
