package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A plugin's native code goes into the host's sources with a registry, and
// what it asks of the app adds up.
func TestMobilePlugins(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("ios/A.swift", "class APlugin {}")
	write("android/A.kt", "class APlugin")
	ps := mobilePlugins{
		{ID: "a", dir: dir,
			IOS:     &pluginIOS{Sources: []string{"ios/*.swift"}, Class: "APlugin", Frameworks: []string{"CoreLocation"}, InfoPlist: map[string]any{"UIBackgroundModes": []any{"audio"}, "K": "v"}},
			Android: &pluginAndroid{Sources: []string{"android/*.kt"}, Class: "APlugin", Permissions: []string{"CAMERA"}, Dependencies: []string{"x:y:1"}}},
		{ID: "b", dir: dir,
			IOS:     &pluginIOS{Class: "BPlugin", Frameworks: []string{"CoreLocation", "Photos"}, InfoPlist: map[string]any{"UIBackgroundModes": []any{"fetch"}}},
			Android: &pluginAndroid{Class: "BPlugin", Dependencies: []string{"x:y:1", "z:w:2"}, Application: []string{"<service />"}}},
	}
	swift := t.TempDir()
	if err := ps.writeSwift(swift); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(swift, "Plugin_a_A.swift")); err != nil {
		t.Error(err)
	}
	reg, _ := os.ReadFile(filepath.Join(swift, "PluginRegistry.swift"))
	if !strings.Contains(string(reg), `PluginHost.register("a", APlugin())`) || !strings.Contains(string(reg), `PluginHost.register("b", BPlugin())`) {
		t.Errorf("registry: %s", reg)
	}
	if got := ps.frameworks(); len(got) != 2 {
		t.Errorf("frameworks: %v", got)
	}
	if got := ps.infoPlist("dev.mygo.x")["UIBackgroundModes"].([]any); len(got) != 2 {
		t.Errorf("background modes: %v", got)
	}
	kt := t.TempDir()
	if err := os.WriteFile(filepath.Join(kt, "Plugin_gone_X.kt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ps.writeKotlin(kt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(kt, "Plugin_gone_X.kt")); err == nil {
		t.Error("a plugin no longer imported stays")
	}
	if _, err := os.Stat(filepath.Join(kt, "Plugin_a_A.kt")); err != nil {
		t.Error(err)
	}
	if got := ps.dependencies(); len(got) != 2 {
		t.Errorf("dependencies: %v", got)
	}
	m := string(androidManifest(&Config{}, ps))
	if !strings.Contains(m, "android.permission.CAMERA") || !strings.Contains(m, "<service />") {
		t.Errorf("manifest: %s", m)
	}
	// A package may not reach outside itself.
	bad := &mobilePlugin{ID: "c", dir: dir}
	if _, err := bad.sourcesOf([]string{"../x.swift"}); err == nil {
		t.Error("a source outside the package")
	}
}

// What decides whether Gradle runs changes with what an APK holds, not with the
// app's Go code.
func TestSwiftStamp(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.swift")
	if err := os.WriteFile(src, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	obj := filepath.Join(t.TempDir(), "host.o")
	if swiftCompiled(dir, "arm64", obj) {
		t.Error("nothing compiled yet")
	}
	if err := os.WriteFile(obj, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	noteSwiftCompiled(dir, "arm64", obj)
	if !swiftCompiled(dir, "arm64", obj) {
		t.Error("just compiled")
	}
	if swiftCompiled(dir, "x86_64", obj) {
		t.Error("another target")
	}
	if err := os.WriteFile(src, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if swiftCompiled(dir, "arm64", obj) {
		t.Error("a source changed")
	}
}

// A plugin's C compiles once for a target, from the sources it names and the
// archive's, into the place phones load it from.
func TestPluginNative(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake compiler is a shell script")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "native"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "native", "a.c")
	if err := os.WriteFile(src, []byte("int a;"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The compiler says it ran, and writes what -o names.
	runs := filepath.Join(dir, "runs")
	cc := filepath.Join(dir, "cc")
	script := "#!/bin/sh\necho \"$@\" >> " + runs + "\nwhile [ $# -gt 1 ]; do [ \"$1\" = -o ] && echo built > \"$2\"; shift; done\n"
	if err := os.WriteFile(cc, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	p := &mobilePlugin{ID: "a", dir: dir}
	n := &pluginNative{Name: "mygo-a", Sources: []string{"native/*.c"}, Cflags: []string{"-DA=1"}}
	target := nativeTarget{key: "t", compiler: []string{cc}, args: []string{"-shared"}, output: func(name string) string { return "lib" + name + ".so" }}
	compile := func() string {
		t.Helper()
		out, err := n.compile(context.Background(), p, target)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	count := func() int {
		data, err := os.ReadFile(runs)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Count(string(data), "\n")
	}
	first := compile()
	if filepath.Base(first) != "libmygo-a.so" || !fileExists(first) {
		t.Fatalf("built %s", first)
	}
	line, _ := os.ReadFile(runs)
	for _, want := range []string{"-shared", "-DA=1", src, "-fPIC"} {
		if !strings.Contains(string(line), want) {
			t.Errorf("compiled with %s, without %s", line, want)
		}
	}
	if compile(); count() != 1 {
		t.Errorf("compiled %d times without a change", count())
	}
	// A change of its source, of its flags or of the target is another build.
	if err := os.WriteFile(src, []byte("int b;"), 0o644); err != nil {
		t.Fatal(err)
	}
	if compile(); count() != 2 {
		t.Errorf("a changed source compiled %d times", count())
	}
	target.key = "other"
	if compile(); count() != 3 {
		t.Errorf("another target compiled %d times", count())
	}
	// Patterns stay inside the package.
	n.Sources = []string{"../x.c"}
	if _, err := n.compile(context.Background(), p, target); err == nil {
		t.Error("a source outside the package compiled")
	}
}

// Desktops' downloads don't apply where the plugin compiles for phones.
func TestBuiltForPhones(t *testing.T) {
	data := []byte(`{"libraries": [{"name": "x"}, {"name": "y"}], "mobile": {"id": "a", "native": {"name": "x"}}}`)
	if !builtForPhones(data, "x") || builtForPhones(data, "y") || builtForPhones([]byte(`{"libraries": []}`), "x") {
		t.Error("builtForPhones")
	}
}

// A plugin's resources are the res files a widget or a tile draws: they land
// under the project's res with their paths, and two plugins may not bring one
// file.
func TestMobilePluginResources(t *testing.T) {
	write := func(root, name, s string) {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a, b := t.TempDir(), t.TempDir()
	write(a, "android/res/layout/widget.xml", "<LinearLayout />")
	write(a, "android/res/xml/widget_info.xml", "<info />")
	ps := mobilePlugins{
		{ID: "widget", dir: a, Android: &pluginAndroid{Resources: []string{"android/res"}}},
	}
	out := t.TempDir()
	if err := ps.writeResources(out); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"layout/widget.xml", "xml/widget_info.xml"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	write(b, "res/layout/widget.xml", "<FrameLayout />")
	ps = append(ps, &mobilePlugin{ID: "other", dir: b, Android: &pluginAndroid{Resources: []string{"res"}}})
	if err := ps.writeResources(t.TempDir()); err == nil {
		t.Error("two plugins may not bring one resource")
	}

	ps = mobilePlugins{{ID: "missing", dir: a, Android: &pluginAndroid{Resources: []string{"android/none"}}}}
	if err := ps.writeResources(t.TempDir()); err == nil {
		t.Error("a resources directory that is not there is an error")
	}
}
