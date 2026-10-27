//go:build !js

package mygo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// The installer of a staged update keeps the bundle it replaced, beside the
// app, when SetKeepPreviousBundles asks for it: the app's background helpers
// outlive the GUI and still need their resources.
func TestStagedUpdateKeepsThePreviousBundle(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Fixture.app")
	unpacked := filepath.Join(dir, "unpacked")
	work := filepath.Join(dir, ".Fixture.app.update-test")
	for path, text := range map[string]string{
		filepath.Join(target, "Contents", "resource"):                  "old helper resource",
		filepath.Join(unpacked, "Fixture.app", "Contents", "resource"): "new resource",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	// A process that has exited stands for the app the installer waits for.
	app := exec.Command("/bin/sleep", "60")
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	pid := app.Process.Pid
	app.Process.Kill()
	app.Wait()
	keep := filepath.Join(dir, fmt.Sprintf(".Fixture.app.old-%d", pid))
	cmd := exec.Command("/bin/sh", "-c", installAfterExit, "mygo-update",
		strconv.Itoa(pid), target, filepath.Join(unpacked, "Fixture.app"), work, target)
	cmd.Env = append(os.Environ(), "MYGO_KEEP_BUNDLE="+keep)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("installer: %v: %s", err, out)
	}
	if data, err := os.ReadFile(filepath.Join(keep, "Contents", "resource")); err != nil || string(data) != "old helper resource" {
		t.Fatalf("background helper bundle removed: %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(target, "Contents", "resource")); err != nil || string(data) != "new resource" {
		t.Fatalf("new bundle not activated: %q, %v", data, err)
	}
}
