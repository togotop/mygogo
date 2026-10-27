//go:build android

package mygo

// linkMenuIcons is a no-op on Android: there is no menu bar to draw icons in,
// and the menus the app shows are its own. Android satisfies the linux tags
// that menuicon_other.go excludes, so it needs this file of its own.
func linkMenuIcons() {}
