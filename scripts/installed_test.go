package scripts_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// inst sources lib.sh and installed.sh, runs the snippet and returns stdout.
func (e *env) inst(snippet string) string {
	e.t.Helper()
	return e.output(e.command("-c", ". lib.sh; . installed.sh; "+snippet))
}

func lines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func contains(list []string, want string) bool {
	for _, l := range list {
		if l == want {
			return true
		}
	}
	return false
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return lines(string(b))
}

func TestInventoryFiles(t *testing.T) {
	e := newEnv(t)
	exampleApp := filepath.Join(e.Root, "Opt", "Example.app")
	notesApp := filepath.Join(e.Root, "System/Applications/Notes.app")
	for _, d := range []string{exampleApp, notesApp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.registry(
		regRecord{Path: exampleApp, ID: "com.Example.App", Team: "ABCDE12345", Name: "Example"},
		regRecord{Path: "/gone/Old.app", ID: "com.example.old", Name: "Old App"},
		regRecord{Path: "/Volumes/Ext/Ext.app", ID: "com.example.ext", Name: "Ext", Unmounted: true},
		regRecord{Path: notesApp, ID: "com.apple.Notes", Name: "Notes"},
	)
	e.app(filepath.Join(e.Root, "Applications/Suite.localized/Suite Center.app"), "com.suite.center", "Suite Centre")
	e.write(filepath.Join(e.Root, "System/Library/LaunchAgents/com.apple.accountsd.plist"), 1)

	dir := t.TempDir()
	e.inst("mc_inventory " + dir)

	ids := readLines(t, filepath.Join(dir, "ids"))
	for _, want := range []string{"com.example.app", "com.example.ext", "com.suite.center"} {
		if !contains(ids, want) {
			t.Errorf("ids lack %q: %q", want, ids)
		}
	}
	if contains(ids, "com.example.old") {
		t.Errorf("ids contain the stale record: %q", ids)
	}
	if got := readLines(t, filepath.Join(dir, "teams")); len(got) != 1 || got[0] != "ABCDE12345" {
		t.Errorf("teams = %q", got)
	}
	names := readLines(t, filepath.Join(dir, "names"))
	for _, want := range []string{"example", "suite center", "suite", "suite centre"} {
		if !contains(names, want) {
			t.Errorf("names lack %q: %q", want, names)
		}
	}
	if contains(names, "old app") {
		t.Errorf("names contain the stale record: %q", names)
	}
	vendors := readLines(t, filepath.Join(dir, "vendors"))
	for _, want := range []string{"example", "suite"} {
		if !contains(vendors, want) {
			t.Errorf("vendors lack %q: %q", want, vendors)
		}
	}
	apple := readLines(t, filepath.Join(dir, "apple"))
	for _, want := range []string{"notes", "accountsd"} {
		if !contains(apple, want) {
			t.Errorf("apple lacks %q: %q", want, apple)
		}
	}
	if contains(apple, "example") {
		t.Errorf("apple contains a non-Apple name: %q", apple)
	}
	if got := readLines(t, filepath.Join(dir, "stale")); len(got) != 1 || got[0] != "com.example.old\tOld App" {
		t.Errorf("stale = %q", got)
	}
}

func TestInventoryRegistryFails(t *testing.T) {
	e := newEnv(t)
	e.registryFails()
	var stdout, stderr bytes.Buffer
	cmd := e.command("-c", ". lib.sh; . installed.sh; mc_inventory "+t.TempDir())
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err == nil {
		t.Fatal("mc_inventory succeeded, want a failure")
	}
	if stderr.Len() == 0 {
		t.Error("no message on stderr")
	}
}

func TestInventoryRegistryMissing(t *testing.T) {
	e := newEnv(t)
	cmd := e.command("-c", ". lib.sh; . installed.sh; mc_inventory "+t.TempDir())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil || stderr.Len() == 0 {
		t.Fatalf("err = %v, stderr = %q; want a failure with a message", err, stderr.String())
	}
}

func TestUnclaimedRules(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ids"), []byte("com.example.app\ncom.vendor.thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := "com.Example.App\tx\ncom.example.app.helper\tx\ncom.vendor\tx\ncom.example.application\tx y\ncom.other.app\tz\n"
	cmd := e.command("-c", ". lib.sh; . installed.sh; mc_unclaimed "+dir)
	cmd.Stdin = strings.NewReader(in)
	got := e.output(cmd)
	if want := "com.example.application\tx y\ncom.other.app\tz\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestIsBundleID(t *testing.T) {
	e := newEnv(t)
	for name, want := range map[string]string{
		"com.a.b": "yes", "com.a": "no", "Foo Bar": "no", "Old Browser 0.8": "no",
	} {
		got := strings.TrimSpace(e.inst("if mc_is_bundle_id '" + name + "'; then echo yes; else echo no; fi"))
		if got != want {
			t.Errorf("mc_is_bundle_id %q = %s, want %s", name, got, want)
		}
	}
}

func (e *env) quiet(days int, paths ...string) string {
	e.t.Helper()
	args := ""
	for _, p := range paths {
		args += " '" + p + "'"
	}
	return strings.TrimSpace(e.inst("mc_quiet " + strconv.Itoa(days) + args + "; echo $?"))
}

func TestQuiet(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(t.TempDir(), "d")
	file := filepath.Join(dir, "sub", "f")
	e.write(file, 1)
	e.age(dir, 400)
	if got := e.quiet(365, dir); got != "0" {
		t.Errorf("old file: status %s, want 0", got)
	}
	if got := e.quiet(365, file); got != "0" {
		t.Errorf("old file as PATH: status %s, want 0", got)
	}
	if e.atime(file).After(time.Now().AddDate(0, 0, -399)) {
		t.Error("mc_quiet changed the access time")
	}

	old := time.Now().AddDate(0, 0, -400)
	now := time.Now()
	if err := os.Chtimes(file, now, old); err != nil {
		t.Fatal(err)
	}
	if got := e.quiet(365, dir); got != "1" {
		t.Errorf("fresh atime: status %s, want 1", got)
	}
	if err := os.Chtimes(file, old, now); err != nil {
		t.Fatal(err)
	}
	if got := e.quiet(365, dir); got != "1" {
		t.Errorf("fresh mtime: status %s, want 1", got)
	}

	// Directory times are no evidence.
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Dir(file), now, now); err != nil {
		t.Fatal(err)
	}
	if got := e.quiet(365, dir); got != "0" {
		t.Errorf("fresh dir times: status %s, want 0", got)
	}

	// Never follows symlinks.
	target := filepath.Join(t.TempDir(), "elsewhere")
	e.write(filepath.Join(target, "fresh"), 1)
	e.link(filepath.Join(dir, "link"), target)
	if got := e.quiet(365, dir); got != "0" {
		t.Errorf("symlink: status %s, want 0", got)
	}

	// Unreadable subfolder.
	locked := filepath.Join(dir, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if got := e.quiet(365, dir); got != "2" {
		t.Errorf("unreadable: status %s, want 2", got)
	}
}

func TestNewestMtime(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(t.TempDir(), "d")
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "x", "b")
	e.write(a, 1)
	e.write(b, 1)
	e.age(a, 400)
	e.age(b, 500)
	fi, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(e.inst("mc_newest_mtime '" + dir + "'"))
	if want := strconv.FormatInt(fi.ModTime().Unix(), 10); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := e.inst("mc_newest_mtime '" + t.TempDir() + "'"); got != "" {
		t.Errorf("empty dir printed %q", got)
	}
}

func TestWarnBundles(t *testing.T) {
	e := newEnv(t)
	e.bundles("com.example.app")
	got := e.inst("mc_warn_bundles Example com.EXAMPLE.app")
	if want := "warn\tExample is running: quit it first\n"; got != want {
		t.Errorf("running: got %q, want %q", got, want)
	}
	if got := e.inst("mc_warn_bundles Example com.example.other"); got != "" {
		t.Errorf("not running: got %q", got)
	}
	e.bundles()
	if got := e.inst("mc_warn_bundles Example com.example.app"); got != "" {
		t.Errorf("no bundles: got %q", got)
	}
	e.tool("lsappinfo", "exit 1")
	got = e.inst("mc_warn_bundles Example com.example.app")
	if want := "warn\tCould not check whether Example is running\n"; got != want {
		t.Errorf("failing: got %q, want %q", got, want)
	}
}

// inventoryErr runs mc_inventory and returns its status and stderr.
func (e *env) inventoryErr(dir string) (bool, string) {
	e.t.Helper()
	var stderr bytes.Buffer
	cmd := e.command("-c", ". lib.sh; . installed.sh; mc_inventory "+dir)
	cmd.Stderr = &stderr
	return cmd.Run() == nil, stderr.String()
}

func TestInventoryEmptyDump(t *testing.T) {
	e := newEnv(t)
	e.exe(filepath.Join(e.Root, lsregisterRel), "exit 0")
	if ok, msg := e.inventoryErr(t.TempDir()); ok || msg == "" {
		t.Errorf("ok = %v, stderr = %q; want a failure with a message", ok, msg)
	}
}

func TestUnclaimedNoIDs(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	cmd := e.command("-c", ". lib.sh; . installed.sh; mc_unclaimed "+dir)
	cmd.Stdin = strings.NewReader("com.other.app\tz\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err == nil || stderr.Len() == 0 || stdout.Len() != 0 {
		t.Errorf("err = %v, stdout = %q, stderr = %q", err, stdout.String(), stderr.String())
	}
	if err := os.WriteFile(filepath.Join(dir, "ids"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = e.command("-c", ". lib.sh; . installed.sh; mc_unclaimed "+dir)
	cmd.Stdin = strings.NewReader("com.other.app\tz\n")
	if got := e.output(cmd); got != "com.other.app\tz\n" {
		t.Errorf("empty ids: got %q", got)
	}
}

func TestInventoryUnreadableAppFolder(t *testing.T) {
	e := newEnv(t)
	e.registry(regRecord{Path: "/gone/Old.app", ID: "com.example.old", Name: "Old"})
	locked := filepath.Join(e.Root, "Applications", "Locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if ok, msg := e.inventoryErr(t.TempDir()); ok || msg == "" {
		t.Errorf("ok = %v, stderr = %q; want a failure with a message", ok, msg)
	}
}

func TestInventoryBrokenPlist(t *testing.T) {
	e := newEnv(t)
	e.registry(regRecord{Path: "/gone/Old.app", ID: "com.example.old", Name: "Old"})
	e.write(filepath.Join(e.Root, "Applications/Bad.app/Contents/Info.plist"), 1)
	if ok, msg := e.inventoryErr(t.TempDir()); ok || !strings.Contains(msg, "Bad.app") {
		t.Errorf("ok = %v, stderr = %q; want a failure naming the app", ok, msg)
	}
}

func TestInventoryPlistWithoutID(t *testing.T) {
	e := newEnv(t)
	otherApp(e, regRecord{Path: "/gone/Old.app", ID: "com.example.old", Name: "Old"})
	e.app(filepath.Join(e.Root, "Applications/Good.app"), "com.example.good", "Good")
	noID := filepath.Join(e.Root, "Applications/NoID.app/Contents/Info.plist")
	e.mkparent(noID)
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>CFBundleName</key><string>NoID</string></dict></plist>
`
	if err := os.WriteFile(noID, []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	e.mkparent(filepath.Join(e.Root, "Applications/Bare.app/x"))
	dir := t.TempDir()
	if ok, msg := e.inventoryErr(dir); !ok {
		t.Fatalf("failed: %s", msg)
	}
	if ids := readLines(t, filepath.Join(dir, "ids")); !contains(ids, "com.example.good") {
		t.Errorf("ids = %q", ids)
	}
}

func TestInventoryNestedBundles(t *testing.T) {
	e := newEnv(t)
	// One app found by the Info.plist scan, one only by the registry.
	scanned := filepath.Join(e.Root, "Applications/Example.app")
	e.app(scanned, "com.example.app", "Example")
	e.app(filepath.Join(scanned, "Contents/XPCServices/Renderer.xpc"), "com.example.renderer", "Renderer")
	e.app(filepath.Join(scanned, "Contents/Frameworks/Core.framework/Versions/A/XPCServices/Deep.xpc"), "com.example.deep", "Deep")
	e.app(filepath.Join(scanned, "Contents/PlugIns/Share.appex"), "com.example.share", "Share")
	e.app(filepath.Join(scanned, "Contents/Library/LoginItems/Helper.app"), "com.example.helper", "Helper")
	// No Info.plist, and an Info.plist without an id: skipped.
	e.mkparent(filepath.Join(scanned, "Contents/XPCServices/Bare.xpc/x"))
	noID := filepath.Join(scanned, "Contents/XPCServices/NoID.xpc/Contents/Info.plist")
	e.mkparent(noID)
	if err := os.WriteFile(noID, []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleName</key><string>NoID</string></dict></plist>`), 0o644); err != nil {
		t.Fatal(err)
	}
	registered := filepath.Join(e.Root, "Opt/Registered.app")
	e.app(registered, "com.example.registered", "Registered")
	e.app(filepath.Join(registered, "Contents/XPCServices/Agent.xpc"), "com.example.agent", "Agent")
	// A stale record's folder is not an installed app.
	gone := filepath.Join(e.Root, "Opt/Gone.app")
	e.app(filepath.Join(gone, "Contents/XPCServices/Old.xpc"), "com.example.oldxpc", "Old")
	e.registry(
		regRecord{Path: scanned, ID: "com.example.app", Name: "Example"},
		regRecord{Path: registered, ID: "com.example.registered", Name: "Registered"},
		regRecord{Path: gone + ".missing", ID: "com.example.gone", Name: "Gone"},
	)
	dir := t.TempDir()
	if ok, msg := e.inventoryErr(dir); !ok {
		t.Fatalf("failed: %s", msg)
	}
	ids := readLines(t, filepath.Join(dir, "ids"))
	for _, want := range []string{"com.example.renderer", "com.example.deep", "com.example.share", "com.example.helper", "com.example.agent"} {
		if !contains(ids, want) {
			t.Errorf("ids lack %q: %q", want, ids)
		}
	}
	if contains(ids, "com.example.oldxpc") || contains(ids, "com.example.gone") {
		t.Errorf("ids contain a stale app's bundle: %q", ids)
	}
}

func TestInventoryBrokenNestedPlist(t *testing.T) {
	e := newEnv(t)
	app := filepath.Join(e.Root, "Applications/Example.app")
	e.app(app, "com.example.app", "Example")
	e.write(filepath.Join(app, "Contents/XPCServices/Bad.xpc/Contents/Info.plist"), 1)
	e.registry(regRecord{Path: app, ID: "com.example.app", Name: "Example"})
	if ok, msg := e.inventoryErr(t.TempDir()); ok || !strings.Contains(msg, "Bad.xpc") {
		t.Errorf("ok = %v, stderr = %q; want a failure naming the bundle", ok, msg)
	}
}

func TestInventoryNoInstalledRecords(t *testing.T) {
	e := newEnv(t)
	// Only stale records.
	e.registry(regRecord{Path: "/gone/Old.app", ID: "com.example.old", Name: "Old"})
	if ok, msg := e.inventoryErr(t.TempDir()); ok || msg == "" {
		t.Errorf("stale only: ok = %v, stderr = %q; want a failure with a message", ok, msg)
	}
	// Records with an identifier but no path.
	e.registry(regRecord{ID: "com.example.nopath", Name: "NoPath"})
	if ok, msg := e.inventoryErr(t.TempDir()); ok || msg == "" {
		t.Errorf("no path: ok = %v, stderr = %q; want a failure with a message", ok, msg)
	}
}

func TestInventoryUnreadableRegistryApp(t *testing.T) {
	e := newEnv(t)
	other := filepath.Join(e.Root, "Applications/Other.app")
	e.app(other, "com.example.other", "Other")
	// An app known only to the registry, with a folder find can't read.
	reg := filepath.Join(e.Root, "Opt/Registered.app")
	e.app(reg, "com.example.registered", "Registered")
	locked := filepath.Join(reg, "Contents/XPCServices/Locked.xpc")
	e.app(filepath.Join(reg, "Contents/XPCServices/Fine.xpc"), "com.example.fine", "Fine")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	e.registry(
		regRecord{Path: other, ID: "com.example.other", Name: "Other"},
		regRecord{Path: reg, ID: "com.example.registered", Name: "Registered"},
	)
	dir := t.TempDir()
	if ok, msg := e.inventoryErr(dir); !ok {
		t.Fatalf("failed: %s", msg)
	}
	if ids := readLines(t, filepath.Join(dir, "ids")); !contains(ids, "com.example.fine") {
		t.Errorf("ids = %q", ids)
	}
}
