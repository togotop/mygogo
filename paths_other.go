//go:build !android || !cgo

package mygo

func platformDirs() (data, cache string, ok bool) { return "", "", false }
