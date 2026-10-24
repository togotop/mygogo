package backend

import (
	"errors"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestExportDialogMultiFileUnsupported(t *testing.T) {
	f := &fakeBridge{}
	install(t, f)
	var got bool
	var gotErr error
	androidDialogs{}.ShowExportDialog(nil, &platform.ExportDialogOptions{
		Title: "x", Files: []string{"/a", "/b"},
	}, func(ok bool, err error) { got, gotErr = ok, err })
	if got || !errors.Is(gotErr, platform.ErrUnsupported) {
		t.Fatalf("ShowExportDialog(two) = %v, %v; want false, ErrUnsupported", got, gotErr)
	}
	if f.exportToken != 0 {
		t.Fatalf("export started despite multiple files (token %d)", f.exportToken)
	}
}

func TestExportDialogSingleFileStarts(t *testing.T) {
	f := &fakeBridge{}
	install(t, f)
	androidDialogs{}.ShowExportDialog(nil, &platform.ExportDialogOptions{
		Title: "x", Files: []string{"/a"},
	}, func(bool, error) {})
	if f.exportToken == 0 || f.exportPath != "/a" || f.exportTitle != "x" {
		t.Fatalf("export = token %d path %q title %q", f.exportToken, f.exportPath, f.exportTitle)
	}
	ExportResult(f.exportToken, true)
}

func TestPhotoDialogCancelIsNilNil(t *testing.T) {
	f := &fakeBridge{}
	install(t, f)
	var paths []string
	var gotErr error
	called := false
	androidDialogs{}.ShowPhotoDialog(nil, &platform.PhotoDialogOptions{Multiple: true},
		func(p []string, e error) { paths, gotErr, called = p, e, true })
	if f.photoToken == 0 || !f.photoMultiple {
		t.Fatalf("photo = token %d multiple %v", f.photoToken, f.photoMultiple)
	}
	if called {
		t.Fatal("done ran before a result arrived")
	}
	PhotoResult(f.photoToken, nil)
	if !called || paths != nil || gotErr != nil {
		t.Fatalf("cancel = %v, %v; want nil, nil", paths, gotErr)
	}
}
