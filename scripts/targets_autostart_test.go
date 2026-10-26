package scripts_test

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"mac-cleanup/internal/protocol"
)

const launchdProgram = "/Applications/Example App.app/Contents/MacOS/helper"

const launchdTarget = "autostart-launchd"

func uidStr() string { return strconv.Itoa(os.Getuid()) }

func guiDomain() string { return "gui/" + uidStr() }

// launchdEnv is an Mac with the registry, a process table with launchd only,
// a launchctl without jobs and one BTM record that belongs to no plist.
func launchdEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	otherApp(e)
	e.ps(psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"})
	e.launchd()
	e.btm(btmRecord{UID: os.Getuid(), Type: "spotlight", Disposition: "enabled, allowed, notified", Name: "Example Importer", Identifier: "3.com.example.importer"})
	return e
}

// plistIn writes a plist for label in dir whose program exists under MC_ROOT.
func plistIn(e *env, dir, label string) string {
	e.t.Helper()
	p := filepath.Join(dir, label+".plist")
	e.plist(p, label, launchdProgram)
	e.exe(filepath.Join(e.Root, launchdProgram), "")
	return p
}

func userAgent(e *env, label string) string {
	return plistIn(e, filepath.Join(e.Home, "Library/LaunchAgents"), label)
}

func systemAgent(e *env, label string) string {
	return plistIn(e, filepath.Join(e.Root, "Library/LaunchAgents"), label)
}

func daemon(e *env, label string) string {
	return plistIn(e, filepath.Join(e.Root, "Library/LaunchDaemons"), label)
}

// btmURL is the file:// URL of a plist as BTM prints it: MC_ROOT removed
// (it stands for /), the path percent-encoded.
func btmURL(e *env, plist string) string {
	p := strings.TrimPrefix(plist, e.Root)
	return "file://" + (&url.URL{Path: p}).EscapedPath()
}

func legacyRecord(e *env, uid int, typ, plist, parent string) btmRecord {
	return btmRecord{
		UID: uid, Type: typ, Flags: "legacy, curated", Disposition: "enabled, allowed, notified",
		Name: "Example Updater", Identifier: "9.com.example.updater", URL: btmURL(e, plist),
		ExePath: launchdProgram, ParentID: parent,
	}
}

func appParent(bundleID, team string) btmRecord {
	return btmRecord{
		UID: os.Getuid(), Type: "app", Disposition: "enabled, allowed, notified", Name: "Example App",
		Developer: "Example Corp", TeamID: team, Identifier: "2." + bundleID,
		URL: "file:///Applications/Example%20App.app/", BundleID: bundleID,
	}
}

func developerParent(name string) btmRecord {
	return btmRecord{UID: os.Getuid(), Type: "developer", Disposition: "enabled, allowed, notified", Name: name, Identifier: "Example Corp"}
}

func autostartItem(t *testing.T, items []protocol.Item, id string) protocol.Item {
	t.Helper()
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("no item %q in %+v", id, items)
	return protocol.Item{}
}

func onlyItem(t *testing.T, items []protocol.Item) protocol.Item {
	t.Helper()
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1: %+v", len(items), items)
	}
	return items[0]
}

func TestAutostartLaunchdAbsent(t *testing.T) {
	e := launchdEnv(t)
	// No plist, no other tool is called: they would leave a mark and fail.
	mark := filepath.Join(e.Home, "called")
	for _, tool := range []string{"ps", "sfltool", "launchctl"} {
		e.tool(tool, "echo "+tool+" >> "+mark+"; exit 1")
	}
	// Symlinks and other files do not count.
	e.write(filepath.Join(e.Home, "Library/LaunchAgents/readme.txt"), 1)
	e.link(filepath.Join(e.Home, "Library/LaunchAgents/linked.plist"), filepath.Join(e.Root, "nowhere.plist"))
	if items := e.run(launchdTarget); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
	if b, err := os.ReadFile(mark); err == nil {
		t.Errorf("tools were called: %s", b)
	}
}

func TestLaunchdRunningAgent(t *testing.T) {
	e := launchdEnv(t)
	p := userAgent(e, "com.example.agent")
	e.ps(
		psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"},
		psLine{PID: 42, PPID: 1, RSSKB: 1000, Comm: launchdProgram},
		psLine{PID: 43, PPID: 42, RSSKB: 500, Comm: launchdProgram},
		psLine{PID: 44, PPID: 1, RSSKB: 9999, Comm: "/usr/libexec/other"},
	)
	e.launchd(launchdJob{Target: guiDomain() + "/com.example.agent", PID: 42})
	it := onlyItem(t, e.run(launchdTarget))
	target := "'" + guiDomain() + "/com.example.agent'"
	if it.ID != guiDomain()+"/com.example.agent" || it.Name != "com.example.agent" || it.Category != "Agents and daemons" || !it.Autostart() {
		t.Errorf("unexpected identity %+v", it)
	}
	if it.State != "running" || it.Memory != 1500*1024 || it.Enabled != "on" || it.Admin || it.Default != protocol.Disable {
		t.Errorf("unexpected state %+v", it)
	}
	if len(it.Paths) != 1 || it.Paths[0] != p {
		t.Errorf("paths %v", it.Paths)
	}
	want := map[protocol.Action]string{
		protocol.Stop:    "launchctl bootout " + target,
		protocol.Disable: "launchctl bootout " + target + " && launchctl disable " + target,
		protocol.Trash:   "launchctl bootout " + target + " && /usr/bin/trash '" + p + "'",
	}
	for a, c := range want {
		if it.Commands[a] != c {
			t.Errorf("%s command %q, want %q", a, it.Commands[a], c)
		}
	}
	if len(it.Commands) != 3 {
		t.Errorf("commands %v", it.Commands)
	}
	wantExplain := "Launch agent of an unknown owner, started at every login. Program: " + launchdProgram + ". Disable stops it and keeps it from starting again; Trash also removes its plist."
	if it.Explain != wantExplain {
		t.Errorf("explain %q", it.Explain)
	}
}

func TestLaunchdLoadedNotRunning(t *testing.T) {
	e := launchdEnv(t)
	userAgent(e, "com.example.agent")
	e.launchd(launchdJob{Target: guiDomain() + "/com.example.agent", State: "waiting"})
	it := onlyItem(t, e.run(launchdTarget))
	if it.State != "stopped" || it.Memory != 0 {
		t.Errorf("state %q memory %d", it.State, it.Memory)
	}
	if _, ok := it.Commands[protocol.Stop]; !ok {
		t.Errorf("a loaded job has Stop: %v", it.Commands)
	}
}

func TestLaunchdNotLoaded(t *testing.T) {
	e := launchdEnv(t)
	p := userAgent(e, "com.example.agent")
	it := onlyItem(t, e.run(launchdTarget))
	target := "'" + guiDomain() + "/com.example.agent'"
	if it.State != "stopped" || it.Enabled != "on" || it.Default != protocol.Disable {
		t.Errorf("unexpected %+v", it)
	}
	if _, ok := it.Commands[protocol.Stop]; ok {
		t.Errorf("not loaded, but Stop: %v", it.Commands)
	}
	if it.Commands[protocol.Disable] != "launchctl disable "+target {
		t.Errorf("disable %q", it.Commands[protocol.Disable])
	}
	if it.Commands[protocol.Trash] != "/usr/bin/trash '"+p+"'" {
		t.Errorf("trash %q", it.Commands[protocol.Trash])
	}
}

func TestLaunchdStoppedDisabled(t *testing.T) {
	e := launchdEnv(t)
	userAgent(e, "com.example.agent")
	e.disabled(guiDomain(), "com.example.agent")
	it := onlyItem(t, e.run(launchdTarget))
	if it.State != "stopped" || it.Memory != 0 || it.Enabled != "off" || it.Default != protocol.Trash {
		t.Errorf("unexpected %+v", it)
	}
	if len(it.Commands) != 1 || it.Commands[protocol.Trash] == "" {
		t.Errorf("commands %v, want only trash", it.Commands)
	}
	if strings.Contains(it.Explain, "Disable stops") {
		t.Errorf("explain offers Disable: %q", it.Explain)
	}
}

func TestLaunchdDisabledButLoaded(t *testing.T) {
	e := launchdEnv(t)
	userAgent(e, "com.example.agent")
	e.disabled(guiDomain(), "com.example.agent")
	e.launchd(launchdJob{Target: guiDomain() + "/com.example.agent", PID: 42})
	e.ps(psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"}, psLine{PID: 42, PPID: 1, RSSKB: 10, Comm: launchdProgram})
	it := onlyItem(t, e.run(launchdTarget))
	if it.Enabled != "off" || it.Default != protocol.Disable {
		t.Errorf("unexpected %+v", it)
	}
	if !strings.Contains(it.Commands[protocol.Disable], "bootout") {
		t.Errorf("a loaded job is stopped by Disable: %q", it.Commands[protocol.Disable])
	}
}

func TestLaunchdDisabledByBTM(t *testing.T) {
	e := launchdEnv(t)
	p := userAgent(e, "com.example.agent")
	rec := legacyRecord(e, os.Getuid(), "legacy agent", p, "Example Corp")
	rec.Disposition = "disabled, allowed, notified"
	e.btm(rec, developerParent("Example Corp"))
	it := onlyItem(t, e.run(launchdTarget))
	if it.Enabled != "off" {
		t.Errorf("enabled %q, want off", it.Enabled)
	}
}

func TestLaunchdDaemon(t *testing.T) {
	e := launchdEnv(t)
	p := daemon(e, "com.example.daemon")
	// A root-owned folder: the plist moves to the Trash with sudo.
	dir := filepath.Dir(p)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	e.ps(psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"}, psLine{PID: 50, PPID: 1, RSSKB: 2000, Comm: launchdProgram})
	e.launchd(launchdJob{Target: "system/com.example.daemon", PID: 50})
	it := onlyItem(t, e.run(launchdTarget))
	if it.ID != "system/com.example.daemon" || !it.Admin || it.State != "running" || it.Memory != 2000*1024 {
		t.Errorf("unexpected %+v", it)
	}
	if it.Commands[protocol.Stop] != "sudo launchctl bootout 'system/com.example.daemon'" {
		t.Errorf("stop %q", it.Commands[protocol.Stop])
	}
	if it.Commands[protocol.Disable] != "sudo launchctl bootout 'system/com.example.daemon' && sudo launchctl disable 'system/com.example.daemon'" {
		t.Errorf("disable %q", it.Commands[protocol.Disable])
	}
	if !strings.HasPrefix(it.Commands[protocol.Trash], "sudo launchctl bootout 'system/com.example.daemon' && [ ! -e '") ||
		!strings.Contains(it.Commands[protocol.Trash], " && sudo mv -- '"+p+"' '") {
		t.Errorf("trash %q", it.Commands[protocol.Trash])
	}
	if !strings.Contains(it.Explain, "started at boot as root") || !strings.HasPrefix(it.Explain, "Launch daemon") {
		t.Errorf("explain %q", it.Explain)
	}
}

func TestLaunchdDaemonDisabledList(t *testing.T) {
	e := launchdEnv(t)
	daemon(e, "com.example.daemon")
	// The disabled list of the system domain, not the user's.
	e.disabled("system", "com.example.daemon")
	it := onlyItem(t, e.run(launchdTarget))
	if it.Enabled != "off" || !it.Admin {
		t.Errorf("unexpected %+v", it)
	}
}

func TestLaunchdSystemAgent(t *testing.T) {
	e := launchdEnv(t)
	p := systemAgent(e, "com.example.sysagent")
	dir := filepath.Dir(p)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	e.launchd(launchdJob{Target: guiDomain() + "/com.example.sysagent", PID: 7})
	e.ps(psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"}, psLine{PID: 7, PPID: 1, RSSKB: 10, Comm: launchdProgram})
	it := onlyItem(t, e.run(launchdTarget))
	target := "'" + guiDomain() + "/com.example.sysagent'"
	// It loads into the user's domain, so launchctl needs no sudo; moving the
	// plist out of /Library does.
	if it.ID != guiDomain()+"/com.example.sysagent" || !it.Admin {
		t.Errorf("unexpected %+v", it)
	}
	if it.Commands[protocol.Stop] != "launchctl bootout "+target {
		t.Errorf("stop %q", it.Commands[protocol.Stop])
	}
	if it.Commands[protocol.Disable] != "launchctl bootout "+target+" && launchctl disable "+target {
		t.Errorf("disable %q", it.Commands[protocol.Disable])
	}
	if !strings.HasPrefix(it.Commands[protocol.Trash], "launchctl bootout "+target+" && [ ! -e '") ||
		!strings.Contains(it.Commands[protocol.Trash], " && sudo mv -- '"+p+"' '") {
		t.Errorf("trash %q", it.Commands[protocol.Trash])
	}
	if !strings.Contains(it.Explain, "Launch agent") {
		t.Errorf("explain %q", it.Explain)
	}
}

func TestLaunchdProgramMissing(t *testing.T) {
	e := launchdEnv(t)
	p := filepath.Join(e.Home, "Library/LaunchAgents/com.example.agent.plist")
	e.plist(p, "com.example.agent", "/opt/example/gone")
	it := onlyItem(t, e.run(launchdTarget))
	if it.Orphan != "program missing" || it.Default != protocol.Trash {
		t.Errorf("orphan %q default %q", it.Orphan, it.Default)
	}
	if it.Commands[protocol.Disable] == "" {
		t.Errorf("Disable stays available: %v", it.Commands)
	}
}

func TestLaunchdProgramKey(t *testing.T) {
	e := launchdEnv(t)
	p := filepath.Join(e.Home, "Library/LaunchAgents/com.example.agent.plist")
	// Program wins over ProgramArguments.
	e.mkparent(p)
	body := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>Label</key><string>com.example.agent</string>
<key>Program</key><string>/opt/example/real</string>
<key>ProgramArguments</key><array><string>/opt/example/other</string></array>
</dict></plist>
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	e.exe(filepath.Join(e.Root, "opt/example/real"), "")
	it := onlyItem(t, e.run(launchdTarget))
	if it.Orphan != "" || !strings.Contains(it.Explain, "Program: /opt/example/real.") {
		t.Errorf("orphan %q explain %q", it.Orphan, it.Explain)
	}
}

func TestLaunchdRelativeProgram(t *testing.T) {
	e := launchdEnv(t)
	e.plist(filepath.Join(e.Home, "Library/LaunchAgents/com.example.agent.plist"), "com.example.agent", "example-tool", "--run")
	it := onlyItem(t, e.run(launchdTarget))
	if it.Orphan != "" {
		t.Errorf("a relative program is not checked, orphan %q", it.Orphan)
	}
}

func TestLaunchdProgramBehindClosedFolder(t *testing.T) {
	e := launchdEnv(t)
	dir := filepath.Join(e.Root, "opt/closed")
	e.exe(filepath.Join(dir, "tool"), "")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	e.plist(filepath.Join(e.Home, "Library/LaunchAgents/com.example.agent.plist"), "com.example.agent", "/opt/closed/tool")
	it := onlyItem(t, e.run(launchdTarget))
	if it.Orphan != "" {
		t.Errorf("a program that cannot be looked up is not missing, orphan %q", it.Orphan)
	}
}

// programApart moves the program of an agent out of the example bundle and
// removes the bundle, as when the app of the parent record is deleted.
func programApart(e *env, plist string) {
	e.t.Helper()
	e.plist(plist, strings.TrimSuffix(filepath.Base(plist), ".plist"), "/opt/example/tool")
	e.exe(filepath.Join(e.Root, "opt/example/tool"), "")
	if err := os.RemoveAll(filepath.Join(e.Root, exampleBundle)); err != nil {
		e.t.Fatal(err)
	}
}

func TestLaunchdAppDeleted(t *testing.T) {
	cases := []struct {
		name   string
		parent btmRecord
		reg    []regRecord
		orphan string
	}{
		{"gone", appParent("com.example.gone", ""), nil, "app deleted"},
		{"gone with unknown team", appParent("com.example.gone", "ZZZZZ99999"), nil, "app deleted"},
		{"installed", appParent("com.example.other", ""), nil, ""},
		{"vendor sibling installed", appParent("com.example.other.helper", ""), nil, ""},
		{"developer parent", developerParent("Example Corp"), nil, ""},
		{"team installed", appParent("com.example.gone", "ABCDE12345"), []regRecord{{Path: "/Applications/Example Two.app", ID: "net.example.two", Team: "ABCDE12345", Name: "Example Two", Unmounted: true}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := launchdEnv(t)
			if c.reg != nil {
				otherApp(e, c.reg...)
			}
			p := userAgent(e, "com.example.agent")
			if c.orphan == "app deleted" {
				programApart(e, p)
			}
			parentID := c.parent.Identifier
			e.btm(legacyRecord(e, os.Getuid(), "legacy agent", p, parentID), c.parent)
			it := onlyItem(t, e.run(launchdTarget))
			if it.Orphan != c.orphan {
				t.Errorf("orphan %q, want %q", it.Orphan, c.orphan)
			}
			if c.orphan != "" && it.Default != protocol.Disable {
				t.Errorf("an app-deleted item keeps default %q", it.Default)
			}
		})
	}
}

func TestLaunchdOwnerUnknown(t *testing.T) {
	e := launchdEnv(t)
	userAgent(e, "com.example.agent")
	it := onlyItem(t, e.run(launchdTarget))
	if it.Owner != "" || it.Orphan != "" {
		t.Errorf("owner %q orphan %q", it.Owner, it.Orphan)
	}
	if !strings.Contains(it.Explain, "unknown owner") {
		t.Errorf("explain %q", it.Explain)
	}
}

func TestLaunchdOwnerFromBTM(t *testing.T) {
	e := launchdEnv(t)
	// A home folder with a space: the BTM URL is percent-encoded.
	e.Home = filepath.Join(filepath.Dir(e.Home), "my home")
	if err := os.MkdirAll(e.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	p := userAgent(e, "com.example.agent")
	d := daemon(e, "com.example.daemon")
	if !strings.Contains(btmURL(e, p), "my%20home") {
		t.Fatalf("url %q", btmURL(e, p))
	}
	e.btm(
		legacyRecord(e, os.Getuid(), "legacy agent", p, "Example Corp"),
		developerParent("Example Corp"),
		// Legacy daemons sit in the section of another user.
		legacyRecord(e, 0, "legacy daemon", d, "Example Corp"),
		btmRecord{UID: 0, Type: "developer", Disposition: "enabled, allowed, notified", Name: "Example Daemon Corp", Identifier: "Example Corp"},
	)
	items := e.run(launchdTarget)
	if len(items) != 2 {
		t.Fatalf("got %+v", items)
	}
	if got := autostartItem(t, items, guiDomain()+"/com.example.agent").Owner; got != "Example Corp" {
		t.Errorf("agent owner %q", got)
	}
	// The parent in the record's own section wins over a same-named one.
	if got := autostartItem(t, items, "system/com.example.daemon").Owner; got != "Example Daemon Corp" {
		t.Errorf("daemon owner %q", got)
	}
	if ex := autostartItem(t, items, guiDomain()+"/com.example.agent").Explain; !strings.HasPrefix(ex, "Launch agent of Example Corp, started at every login.") {
		t.Errorf("explain %q", ex)
	}
}

func TestLaunchdOwnerFallbacks(t *testing.T) {
	cases := []struct {
		name   string
		parent btmRecord
		want   string
	}{
		{"developer name", btmRecord{UID: os.Getuid(), Type: "app", Disposition: "enabled, allowed, notified", Developer: "Example Corp", Identifier: "2.com.example.other", BundleID: "com.example.other"}, "Example Corp"},
		{"parent id", developerParent(""), "Example Corp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := launchdEnv(t)
			p := userAgent(e, "com.example.agent")
			parent := c.parent
			e.btm(legacyRecord(e, os.Getuid(), "legacy agent", p, parent.Identifier), parent)
			if got := onlyItem(t, e.run(launchdTarget)).Owner; got != c.want {
				t.Errorf("owner %q, want %q", got, c.want)
			}
		})
	}
}

func TestLaunchdBTMOtherTypeNoMatch(t *testing.T) {
	e := launchdEnv(t)
	p := userAgent(e, "com.example.agent")
	// Only legacy records describe a plist in these folders.
	rec := legacyRecord(e, os.Getuid(), "agent", p, "Example Corp")
	e.btm(rec, developerParent("Example Corp"))
	if got := onlyItem(t, e.run(launchdTarget)).Owner; got != "" {
		t.Errorf("owner %q", got)
	}
}

func TestLaunchdAppleSkipped(t *testing.T) {
	e := launchdEnv(t)
	userAgent(e, "com.apple.x")
	userAgent(e, "COM.APPLE.Y")
	daemon(e, "Com.Apple.Z")
	if items := e.run(launchdTarget); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
}

func TestLaunchdLabelFromPlist(t *testing.T) {
	e := launchdEnv(t)
	e.plist(filepath.Join(e.Home, "Library/LaunchAgents/a.plist"), "com.example.b", launchdProgram)
	e.exe(filepath.Join(e.Root, launchdProgram), "")
	it := onlyItem(t, e.run(launchdTarget))
	if it.Name != "com.example.b" || it.ID != guiDomain()+"/com.example.b" {
		t.Errorf("unexpected %+v", it)
	}
}

func TestLaunchdUnreadablePlist(t *testing.T) {
	e := launchdEnv(t)
	p := userAgent(e, "com.example.agent")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	it := onlyItem(t, e.run(launchdTarget))
	if it.Name != "com.example.agent" || it.Orphan != "" || !strings.Contains(it.Explain, "Program: unknown.") {
		t.Errorf("unexpected %+v", it)
	}
}

func TestLaunchdUnreadableAppleName(t *testing.T) {
	e := launchdEnv(t)
	p := userAgent(e, "com.apple.thing")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	if items := e.run(launchdTarget); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
}

// An unreadable plist whose real label is loaded from it: the file name is
// only a guess at the label, so nothing is run against that guess.
func TestLaunchdUnreadablePlistGuessedLabel(t *testing.T) {
	e := launchdEnv(t)
	p := filepath.Join(e.Root, "Library/LaunchDaemons/a.plist")
	e.plist(p, "com.example.real", launchdProgram)
	e.exe(filepath.Join(e.Root, launchdProgram), "")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	e.ps(psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"}, psLine{PID: 50, PPID: 1, RSSKB: 2000, Comm: launchdProgram})
	e.launchd(launchdJob{Target: "system/com.example.real", PID: 50, Path: "/Library/LaunchDaemons/a.plist"})
	it := onlyItem(t, e.run(launchdTarget))
	if it.Name != "a" || it.State != "stopped" || it.Memory != 0 || it.Default != protocol.Trash {
		t.Errorf("unexpected %+v", it)
	}
	if len(it.Commands) != 1 || it.Commands[protocol.Trash] == "" || strings.Contains(it.Commands[protocol.Trash], "bootout") {
		t.Errorf("commands %v, want only a Trash without bootout", it.Commands)
	}
	if !strings.Contains(it.Explain, "can't be read") || !strings.Contains(it.Explain, "under another name") {
		t.Errorf("explain %q", it.Explain)
	}
}

// An unreadable plist whose guessed label is loaded from it keeps its commands.
func TestLaunchdUnreadablePlistLoaded(t *testing.T) {
	e := launchdEnv(t)
	p := userAgent(e, "com.example.agent")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	e.ps(psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"}, psLine{PID: 42, PPID: 1, RSSKB: 10, Comm: launchdProgram})
	e.launchd(launchdJob{Target: guiDomain() + "/com.example.agent", PID: 42, Path: p})
	it := onlyItem(t, e.run(launchdTarget))
	if it.State != "running" || it.Commands[protocol.Stop] == "" || it.Commands[protocol.Disable] == "" || it.Default != protocol.Disable {
		t.Errorf("unexpected %+v", it)
	}
}

// One label in two folders: launchd's path says which plist the loaded job
// came from. The other plist is not loaded.
func TestLaunchdSameLabelLoadedFromOne(t *testing.T) {
	for _, loadedHome := range []bool{true, false} {
		t.Run(strconv.FormatBool(loadedHome), func(t *testing.T) {
			e := launchdEnv(t)
			home := userAgent(e, "com.example.agent")
			sys := systemAgent(e, "com.example.agent")
			loadedFrom, other := home, sys
			jobPath := home
			if !loadedHome {
				loadedFrom, other = sys, home
				jobPath = strings.TrimPrefix(sys, e.Root)
			}
			e.ps(psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"}, psLine{PID: 42, PPID: 1, RSSKB: 1000, Comm: launchdProgram})
			e.launchd(launchdJob{Target: guiDomain() + "/com.example.agent", PID: 42, Path: jobPath})
			items := e.run(launchdTarget)
			if len(items) != 2 {
				t.Fatalf("got %+v", items)
			}
			byPath := map[string]protocol.Item{}
			for _, it := range items {
				byPath[it.Paths[0]] = it
			}
			l, o := byPath[loadedFrom], byPath[other]
			if l.State != "running" || l.Memory != 1000*1024 || l.Commands[protocol.Stop] == "" || !strings.Contains(l.Commands[protocol.Trash], "bootout") {
				t.Errorf("loaded plist: %+v", l)
			}
			if o.State != "stopped" || o.Memory != 0 {
				t.Errorf("other plist: %+v", o)
			}
			// Disabling the label would keep the loaded plist's job from
			// starting as well: Trash only.
			if len(o.Commands) != 1 || o.Commands[protocol.Trash] == "" || strings.Contains(o.Commands[protocol.Trash], "bootout") || o.Default != protocol.Trash {
				t.Errorf("other plist commands %v default %q, want only a Trash without bootout", o.Commands, o.Default)
			}
			if !strings.Contains(o.Explain, "launchd runs this label from another plist, so Trash removes only this file.") {
				t.Errorf("other plist explain %q", o.Explain)
			}
		})
	}
}

// A legacy agent switched off in System Settings is disallowed, not disabled.
func TestLaunchdDisallowedByBTM(t *testing.T) {
	e := launchdEnv(t)
	p := userAgent(e, "com.example.agent")
	rec := legacyRecord(e, os.Getuid(), "legacy agent", p, "Example Corp")
	rec.Disposition = "enabled, disallowed, notified"
	e.btm(rec, developerParent("Example Corp"))
	if it := onlyItem(t, e.run(launchdTarget)); it.Enabled != "off" {
		t.Errorf("enabled %q, want off", it.Enabled)
	}
}

// The user's own record of a plist wins over another user's listed before it.
func TestLaunchdBTMRecordOfThisUser(t *testing.T) {
	e := launchdEnv(t)
	p := userAgent(e, "com.example.agent")
	root := legacyRecord(e, 0, "legacy agent", p, "Example Corp")
	root.Disposition = "disabled, allowed, notified"
	mine := legacyRecord(e, os.Getuid(), "legacy agent", p, "Example Corp")
	e.btm(root, mine, developerParent("Example Corp"))
	it := onlyItem(t, e.run(launchdTarget))
	if it.Enabled != "on" || it.Commands[protocol.Disable] == "" || it.Default != protocol.Disable {
		t.Errorf("unexpected %+v", it)
	}
	// Another user's record still counts when the user has none.
	e.btm(root, developerParent("Example Corp"))
	if it := onlyItem(t, e.run(launchdTarget)); it.Enabled != "off" {
		t.Errorf("fallback: enabled %q, want off", it.Enabled)
	}
}

// A program that is a link to a volume that may not be mounted is not missing.
func TestLaunchdProgramDanglingLink(t *testing.T) {
	e := launchdEnv(t)
	e.link(filepath.Join(e.Root, "opt/example/tool"), "/Volumes/Example Disk/example-tool")
	e.plist(filepath.Join(e.Home, "Library/LaunchAgents/com.example.agent.plist"), "com.example.agent", "/opt/example/tool")
	it := onlyItem(t, e.run(launchdTarget))
	if it.Orphan != "" || it.Default != protocol.Disable {
		t.Errorf("orphan %q default %q", it.Orphan, it.Default)
	}
}

func TestLaunchdQuoting(t *testing.T) {
	e := launchdEnv(t)
	label := "com.example.it's"
	p := userAgent(e, label)
	e.launchd(launchdJob{Target: guiDomain() + "/" + label, PID: 9})
	e.ps(psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"}, psLine{PID: 9, PPID: 1, RSSKB: 10, Comm: launchdProgram})
	it := onlyItem(t, e.run(launchdTarget))
	target := `'` + guiDomain() + `/com.example.it'\''s'`
	if it.Commands[protocol.Stop] != "launchctl bootout "+target {
		t.Errorf("stop %q", it.Commands[protocol.Stop])
	}
	if it.Commands[protocol.Disable] != "launchctl bootout "+target+" && launchctl disable "+target {
		t.Errorf("disable %q", it.Commands[protocol.Disable])
	}
	if it.Commands[protocol.Trash] != "launchctl bootout "+target+" && /usr/bin/trash '"+strings.ReplaceAll(p, "'", `'\''`)+"'" {
		t.Errorf("trash %q", it.Commands[protocol.Trash])
	}
	if it.Name != label {
		t.Errorf("name %q", it.Name)
	}
}

func TestLaunchdPathQuoting(t *testing.T) {
	e := launchdEnv(t)
	e.Home = filepath.Join(filepath.Dir(e.Home), "it's home")
	if err := os.MkdirAll(e.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	p := userAgent(e, "com.example.agent")
	it := onlyItem(t, e.run(launchdTarget))
	want := "/usr/bin/trash '" + strings.ReplaceAll(p, "'", `'\''`) + "'"
	if it.Commands[protocol.Trash] != want {
		t.Errorf("trash %q, want %q", it.Commands[protocol.Trash], want)
	}
}

func TestLaunchdMemoryCountedOnce(t *testing.T) {
	e := launchdEnv(t)
	userAgent(e, "com.example.a")
	userAgent(e, "com.example.b")
	// b runs under a: its memory belongs to a alone.
	e.ps(
		psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"},
		psLine{PID: 10, PPID: 1, RSSKB: 1000, Comm: launchdProgram},
		psLine{PID: 11, PPID: 10, RSSKB: 500, Comm: launchdProgram},
	)
	e.launchd(
		launchdJob{Target: guiDomain() + "/com.example.a", PID: 10},
		launchdJob{Target: guiDomain() + "/com.example.b", PID: 11},
	)
	items := e.run(launchdTarget)
	a := autostartItem(t, items, guiDomain()+"/com.example.a")
	b := autostartItem(t, items, guiDomain()+"/com.example.b")
	if a.Memory+b.Memory != 1500*1024 {
		t.Errorf("a %d + b %d, want 1536000", a.Memory, b.Memory)
	}
	if b.State != "running" {
		t.Errorf("b state %q", b.State)
	}
}

func TestLaunchdSkipsSymlinksAndOtherFiles(t *testing.T) {
	e := launchdEnv(t)
	real := userAgent(e, "com.example.real")
	e.link(filepath.Join(e.Home, "Library/LaunchAgents/com.example.linked.plist"), real)
	e.write(filepath.Join(e.Home, "Library/LaunchAgents/notes.txt"), 1)
	if err := os.MkdirAll(filepath.Join(e.Home, "Library/LaunchAgents/dir.plist"), 0o755); err != nil {
		t.Fatal(err)
	}
	onlyItem(t, e.run(launchdTarget))
}

func TestLaunchdSameLabelTwice(t *testing.T) {
	e := launchdEnv(t)
	userAgent(e, "com.example.agent")
	systemAgent(e, "com.example.agent")
	items := e.run(launchdTarget)
	if len(items) != 2 || items[0].ID == items[1].ID {
		t.Fatalf("ids must differ: %+v", items)
	}
}

func TestLaunchdToolFails(t *testing.T) {
	cases := map[string]func(e *env){
		"ps":         func(e *env) { e.psFails() },
		"btm":        func(e *env) { e.btmFails() },
		"launchctl":  func(e *env) { e.launchctlFails() },
		"print-only": func(e *env) { e.tool("launchctl", `[ "$1" = print ] && exit 1; echo '"x" => enabled'`) },
		"disabled-only": func(e *env) {
			e.tool("launchctl", `[ "$1" = print-disabled ] && { echo failed >&2; exit 1; }; exit 0`)
		},
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			e := launchdEnv(t)
			userAgent(e, "com.example.agent")
			breakIt(e)
			out, stderr := e.runFail(launchdTarget)
			if out != "" || stderr == "" {
				t.Errorf("stdout %q, stderr %q", out, stderr)
			}
		})
	}
}

// The installed apps are read only for a parent app whose bundle is not on
// disk: without the registry the scan works while the bundle is there and
// fails when it is gone.
func TestLaunchdRegistryOnlyWhenNeeded(t *testing.T) {
	e := launchdEnv(t)
	p := userAgent(e, "com.example.agent")
	gone := appParent("com.example.gone", "")
	e.btm(legacyRecord(e, os.Getuid(), "legacy agent", p, gone.Identifier), gone)
	bundleAt(e, exampleBundle)
	e.registryFails()
	if got := onlyItem(t, e.run(launchdTarget)).Orphan; got != "" {
		t.Errorf("bundle present, orphan %q", got)
	}
	programApart(e, p)
	if out, stderr := e.runFail(launchdTarget); out != "" || stderr == "" {
		t.Errorf("bundle gone: stdout %q, stderr %q", out, stderr)
	}
}

func TestLaunchdDaemonDomainFails(t *testing.T) {
	e := launchdEnv(t)
	daemon(e, "com.example.daemon")
	// The user's domain answers, the system domain does not.
	e.tool("launchctl", `case "$2" in system) exit 1;; esac; exit 0`)
	out, stderr := e.runFail(launchdTarget)
	if out != "" || stderr == "" {
		t.Errorf("stdout %q, stderr %q", out, stderr)
	}
}

const loginTarget = "autostart-login"

const exampleBundle = "/Applications/Example App.app"

// parentApp is the BTM record of an app whose own login item is off: the
// parent record of the agents and login items it registered.
func parentApp(bundleID, team string) btmRecord {
	r := appParent(bundleID, team)
	r.Disposition = "disabled, allowed, notified"
	return r
}

// bundleAt creates an app bundle directory (with its main executable) for the
// logical path under MC_ROOT.
func bundleAt(e *env, logical string) string {
	e.t.Helper()
	dir := filepath.Join(e.Root, logical)
	e.exe(filepath.Join(dir, "Contents/MacOS/main"), "")
	return dir
}

// oldStyle is the BTM record of an app that opens at login.
func oldStyle(bundleID, bundle string) btmRecord {
	return btmRecord{
		UID: os.Getuid(), Type: "app", Flags: "", Disposition: "enabled, allowed, notified", Name: "Example App",
		Developer: "Example Corp", Identifier: "2." + bundleID, URL: "file://" + (&url.URL{Path: bundle}).EscapedPath() + "/", BundleID: bundleID,
	}
}

// registered is the BTM record of an agent that the app registers itself.
func registered(parent btmRecord, typ, id, rel, exe string) btmRecord {
	return btmRecord{
		UID: os.Getuid(), Type: typ, Flags: "sandboxed", Disposition: "enabled, allowed, notified", Name: "Example Helper",
		Developer: "Example Corp", Identifier: id, URL: (&url.URL{Path: rel}).EscapedPath(), ExePath: exe, ParentID: parent.Identifier,
	}
}

// agentInApp writes the plist and executable of an SMAppService agent in the
// example bundle and returns the BTM records (agent, parent).
func agentInApp(e *env, label string) (btmRecord, btmRecord) {
	e.t.Helper()
	bundle := bundleAt(e, exampleBundle)
	rel := "Contents/Library/LaunchAgents/" + label + ".plist"
	e.plist(filepath.Join(bundle, rel), label, "x")
	e.exe(filepath.Join(bundle, "Contents/MacOS/example-agent"), "")
	parent := parentApp("com.example.other", "")
	return registered(parent, "agent", "8."+label, rel, "Contents/MacOS/example-agent"), parent
}

func TestAutostartLoginAbsent(t *testing.T) {
	e := launchdEnv(t)
	disabledApp := oldStyle("com.example.app", exampleBundle)
	disabledApp.Disposition = "disabled, allowed, notified"
	e.btm(disabledApp, btmRecord{UID: os.Getuid(), Type: "spotlight", Disposition: "enabled, allowed, notified", Name: "Example Importer", Identifier: "3.com.example.importer"})
	// Nothing to look at: ps and launchctl are not called.
	mark := filepath.Join(e.Home, "called")
	for _, tool := range []string{"ps", "launchctl"} {
		e.tool(tool, "echo "+tool+" >> "+mark+"; exit 1")
	}
	if items := e.run(loginTarget); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
	if b, err := os.ReadFile(mark); err == nil {
		t.Errorf("tools were called: %s", b)
	}
}

func TestLoginAppRunning(t *testing.T) {
	e := launchdEnv(t)
	bundleAt(e, exampleBundle)
	e.btm(oldStyle("com.example.app", exampleBundle))
	e.ps(
		psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"},
		psLine{PID: 42, PPID: 1, RSSKB: 1000, Comm: exampleBundle + "/Contents/MacOS/Example App"},
		psLine{PID: 43, PPID: 1, RSSKB: 500, Comm: exampleBundle + "/Contents/Frameworks/Helper.app/Contents/MacOS/Helper"},
		// A different bundle whose name extends this one.
		psLine{PID: 44, PPID: 1, RSSKB: 9999, Comm: "/Applications/Example App Helper.app/Contents/MacOS/Helper"},
		// A copy of the bundle under another root.
		psLine{PID: 45, PPID: 1, RSSKB: 8888, Comm: "/Volumes/Backup" + exampleBundle + "/Contents/MacOS/Example App"},
	)
	it := onlyItem(t, e.run(loginTarget))
	if it.ID != "login/com.example.app" || it.Name != "Example App" || it.Category != "Login items" || !it.Autostart() {
		t.Errorf("unexpected identity %+v", it)
	}
	if it.State != "running" || it.Memory != 1500*1024 || it.Enabled != "on" || it.Admin || it.Default != protocol.Disable || it.Orphan != "" {
		t.Errorf("unexpected state %+v", it)
	}
	if len(it.Paths) != 1 || it.Paths[0] != exampleBundle {
		t.Errorf("paths %v", it.Paths)
	}
	if it.Owner != "Example Corp" {
		t.Errorf("owner %q", it.Owner)
	}
	want := map[protocol.Action]string{
		protocol.Stop:    `osascript -e 'tell application id "com.example.app" to quit'`,
		protocol.Disable: `osascript -e 'tell application "System Events" to delete (every login item whose path is "/Applications/Example App.app")'`,
	}
	for a, c := range want {
		if it.Commands[a] != c {
			t.Errorf("%s command %q, want %q", a, it.Commands[a], c)
		}
	}
	if len(it.Commands) != 2 {
		t.Errorf("commands %v", it.Commands)
	}
}

func TestLoginAppStopped(t *testing.T) {
	e := launchdEnv(t)
	bundleAt(e, exampleBundle)
	e.btm(oldStyle("com.example.app", exampleBundle))
	it := onlyItem(t, e.run(loginTarget))
	if it.State != "stopped" || it.Memory != 0 || it.Default != protocol.Disable {
		t.Errorf("unexpected %+v", it)
	}
	if _, ok := it.Commands[protocol.Stop]; ok || it.Commands[protocol.Disable] == "" {
		t.Errorf("commands %v", it.Commands)
	}
}

func TestLoginAppQuoting(t *testing.T) {
	cases := []struct{ bundle, want string }{
		{`/Applications/Say "hi".app`, `osascript -e 'tell application "System Events" to delete (every login item whose path is "/Applications/Say \"hi\".app")'`},
		{`/Applications/It's.app`, `osascript -e 'tell application "System Events" to delete (every login item whose path is "/Applications/It'\''s.app")'`},
		{`/Applications/Back\slash.app`, `osascript -e 'tell application "System Events" to delete (every login item whose path is "/Applications/Back\\slash.app")'`},
	}
	for _, c := range cases {
		t.Run(c.bundle, func(t *testing.T) {
			e := launchdEnv(t)
			bundleAt(e, c.bundle)
			rec := oldStyle(`com.example.say"x`, c.bundle)
			e.btm(rec)
			e.ps(psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"}, psLine{PID: 5, PPID: 1, RSSKB: 10, Comm: c.bundle + "/Contents/MacOS/main"})
			it := onlyItem(t, e.run(loginTarget))
			if it.Commands[protocol.Disable] != c.want {
				t.Errorf("disable %q, want %q", it.Commands[protocol.Disable], c.want)
			}
			// The bundle id is quoted for AppleScript and then for bash.
			if it.Commands[protocol.Stop] != `osascript -e 'tell application id "com.example.say\"x" to quit'` {
				t.Errorf("stop %q", it.Commands[protocol.Stop])
			}
			if len(it.Paths) != 1 || it.Paths[0] != c.bundle {
				t.Errorf("paths %v", it.Paths)
			}
		})
	}
}

func TestLoginAppWithoutBundleID(t *testing.T) {
	e := launchdEnv(t)
	bundleAt(e, exampleBundle)
	rec := oldStyle("", exampleBundle)
	rec.Identifier = "2.com.example.fromid"
	e.btm(rec)
	e.ps(psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"}, psLine{PID: 5, PPID: 1, RSSKB: 10, Comm: exampleBundle + "/Contents/MacOS/main"})
	it := onlyItem(t, e.run(loginTarget))
	if it.ID != "login"+exampleBundle {
		t.Errorf("id %q", it.ID)
	}
	if _, ok := it.Commands[protocol.Stop]; ok || it.State != "running" || it.Default != protocol.Disable {
		t.Errorf("no bundle id, no Stop: %+v", it)
	}
}

func TestLoginDisabledAppSkipped(t *testing.T) {
	e := launchdEnv(t)
	bundleAt(e, exampleBundle)
	rec := oldStyle("com.example.app", exampleBundle)
	rec.Disposition = "disabled, allowed, notified"
	e.btm(rec)
	if items := e.run(loginTarget); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
}

func TestLoginAppDisallowed(t *testing.T) {
	for _, running := range []bool{true, false} {
		t.Run(strconv.FormatBool(running), func(t *testing.T) {
			e := launchdEnv(t)
			bundleAt(e, exampleBundle)
			rec := oldStyle("com.example.app", exampleBundle)
			rec.Disposition = "enabled, disallowed, notified"
			e.btm(rec)
			if running {
				e.ps(psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"}, psLine{PID: 5, PPID: 1, RSSKB: 10, Comm: exampleBundle + "/Contents/MacOS/main"})
			}
			it := onlyItem(t, e.run(loginTarget))
			want := protocol.Disable
			if running {
				want = protocol.Stop
			}
			if it.Enabled != "off" || it.Default != want {
				t.Errorf("enabled %q default %q, want off %q", it.Enabled, it.Default, want)
			}
		})
	}
}

func TestLoginAppBundleMissing(t *testing.T) {
	e := launchdEnv(t)
	e.btm(oldStyle("com.example.app", exampleBundle))
	it := onlyItem(t, e.run(loginTarget))
	if it.Orphan != "program missing" || it.Default != protocol.Disable {
		t.Errorf("orphan %q default %q", it.Orphan, it.Default)
	}
}

func TestLoginAppBundleBehindDeadLink(t *testing.T) {
	e := launchdEnv(t)
	e.link(filepath.Join(e.Root, "Applications/Drive"), filepath.Join(e.Root, "gone-drive"))
	e.btm(oldStyle("com.example.app", "/Applications/Drive/Example App.app"))
	if got := onlyItem(t, e.run(loginTarget)).Orphan; got != "" {
		t.Errorf("a link to nowhere is not a missing program, orphan %q", got)
	}
}

func TestLoginAppUnderVolumes(t *testing.T) {
	e := launchdEnv(t)
	e.btm(oldStyle("com.example.app", "/Volumes/Disk/Example App.app"))
	if got := onlyItem(t, e.run(loginTarget)).Orphan; got != "" {
		t.Errorf("an unmounted volume is not a missing program, orphan %q", got)
	}
}

func TestLoginAppInHome(t *testing.T) {
	e := launchdEnv(t)
	bundle := filepath.Join(e.Home, "Applications/Home App.app")
	e.exe(filepath.Join(bundle, "Contents/MacOS/main"), "")
	e.btm(oldStyle("com.example.app", bundle))
	it := onlyItem(t, e.run(loginTarget))
	if it.Orphan != "" || len(it.Paths) != 1 || it.Paths[0] != bundle {
		t.Errorf("unexpected %+v", it)
	}
	if !strings.Contains(it.Commands[protocol.Disable], `whose path is "`+bundle+`"`) {
		t.Errorf("disable %q", it.Commands[protocol.Disable])
	}
}

func TestLoginAgentLabelFromPlist(t *testing.T) {
	e := launchdEnv(t)
	agent, parent := agentInApp(e, "com.example.b")
	// The plist is called a.plist, its label is another.
	bundle := filepath.Join(e.Root, exampleBundle)
	if err := os.Rename(filepath.Join(bundle, "Contents/Library/LaunchAgents/com.example.b.plist"), filepath.Join(bundle, "Contents/Library/LaunchAgents/a.plist")); err != nil {
		t.Fatal(err)
	}
	agent.URL = "Contents/Library/LaunchAgents/a.plist"
	e.btm(parent, agent)
	it := onlyItem(t, e.run(loginTarget))
	if it.ID != guiDomain()+"/com.example.b" {
		t.Errorf("id %q", it.ID)
	}
	if it.Orphan != "" {
		t.Errorf("orphan %q", it.Orphan)
	}
}

func TestLoginAgentLabelFromFileName(t *testing.T) {
	cases := []struct{ name, url, want string }{
		{"unreadable plist", "Contents/Library/LaunchAgents/com.example.file.plist", "com.example.file"},
		{"no url", "", "agent.id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := launchdEnv(t)
			bundleAt(e, exampleBundle)
			parent := parentApp("com.example.other", "")
			agent := registered(parent, "agent", "8.agent.id", c.url, "")
			agent.URL = c.url
			e.btm(parent, agent)
			it := onlyItem(t, e.run(loginTarget))
			if it.ID != guiDomain()+"/"+c.want {
				t.Errorf("id %q, want %s", it.ID, c.want)
			}
			if it.Orphan != "" {
				t.Errorf("without a readable program nothing is missing, orphan %q", it.Orphan)
			}
		})
	}
}

func TestLoginAgent(t *testing.T) {
	e := launchdEnv(t)
	agent, parent := agentInApp(e, "com.example.agent")
	e.btm(parent, agent)
	e.ps(
		psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"},
		psLine{PID: 42, PPID: 1, RSSKB: 1000, Comm: exampleBundle + "/Contents/MacOS/example-agent"},
		psLine{PID: 43, PPID: 42, RSSKB: 500, Comm: "/usr/libexec/child"},
	)
	e.launchd(launchdJob{Target: guiDomain() + "/com.example.agent", PID: 42})
	it := onlyItem(t, e.run(loginTarget))
	target := "'" + guiDomain() + "/com.example.agent'"
	if it.ID != guiDomain()+"/com.example.agent" || it.Name != "com.example.agent" || it.Category != "Login items" || !it.Autostart() {
		t.Errorf("unexpected identity %+v", it)
	}
	if it.State != "running" || it.Memory != 1500*1024 || it.Enabled != "on" || it.Admin || it.Default != protocol.Disable || it.Orphan != "" {
		t.Errorf("unexpected state %+v", it)
	}
	if it.Owner != "Example App" {
		t.Errorf("owner %q, want the parent's name", it.Owner)
	}
	if len(it.Paths) != 1 || it.Paths[0] != exampleBundle {
		t.Errorf("paths %v", it.Paths)
	}
	want := map[protocol.Action]string{
		protocol.Stop:    "launchctl bootout " + target,
		protocol.Disable: "launchctl bootout " + target + " && launchctl disable " + target,
	}
	for a, c := range want {
		if it.Commands[a] != c {
			t.Errorf("%s command %q, want %q", a, it.Commands[a], c)
		}
	}
	if len(it.Commands) != 2 {
		t.Errorf("commands %v", it.Commands)
	}
	if !strings.HasPrefix(it.Explain, "Background agent of Example App, started at every login.") || !strings.HasSuffix(it.Explain, "System Settings may still show it as allowed.") {
		t.Errorf("explain %q", it.Explain)
	}
}

func TestLoginAgentNotLoaded(t *testing.T) {
	e := launchdEnv(t)
	agent, parent := agentInApp(e, "com.example.agent")
	e.btm(parent, agent)
	it := onlyItem(t, e.run(loginTarget))
	target := "'" + guiDomain() + "/com.example.agent'"
	if it.State != "stopped" || it.Default != protocol.Disable || it.Commands[protocol.Disable] != "launchctl disable "+target {
		t.Errorf("unexpected %+v", it)
	}
	if _, ok := it.Commands[protocol.Stop]; ok || len(it.Commands) != 1 {
		t.Errorf("commands %v", it.Commands)
	}
}

func TestLoginAgentOff(t *testing.T) {
	cases := map[string]func(e *env, agent *btmRecord){
		"disallowed": func(e *env, agent *btmRecord) { agent.Disposition = "enabled, disallowed, notified" },
		"disabled":   func(e *env, agent *btmRecord) { agent.Disposition = "disabled, allowed, notified" },
		"launchctl":  func(e *env, agent *btmRecord) { e.disabled(guiDomain(), "com.example.agent") },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			e := launchdEnv(t)
			agent, parent := agentInApp(e, "com.example.agent")
			setup(e, &agent)
			e.btm(parent, agent)
			it := onlyItem(t, e.run(loginTarget))
			// Off and not loaded: nothing to run.
			if it.Enabled != "off" || len(it.Commands) != 0 || it.Default != "" {
				t.Errorf("unexpected %+v", it)
			}
		})
	}
}

func TestLoginAgentOffButLoaded(t *testing.T) {
	e := launchdEnv(t)
	agent, parent := agentInApp(e, "com.example.agent")
	agent.Disposition = "enabled, disallowed, notified"
	e.btm(parent, agent)
	e.launchd(launchdJob{Target: guiDomain() + "/com.example.agent", PID: 42})
	e.ps(psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"}, psLine{PID: 42, PPID: 1, RSSKB: 10, Comm: "/usr/libexec/x"})
	it := onlyItem(t, e.run(loginTarget))
	if it.Enabled != "off" || it.Default != protocol.Disable || !strings.Contains(it.Commands[protocol.Disable], "bootout") {
		t.Errorf("unexpected %+v", it)
	}
}

func TestLoginItemHelper(t *testing.T) {
	e := launchdEnv(t)
	bundle := bundleAt(e, exampleBundle)
	helper := "Contents/Library/LoginItems/Example Helper.app"
	e.exe(filepath.Join(bundle, helper, "Contents/MacOS/main"), "")
	parent := parentApp("com.example.other", "")
	li := registered(parent, "login item", "4.com.example.helper", helper, "")
	li.BundleID = "com.example.helper"
	e.btm(parent, li)
	e.launchd(launchdJob{Target: guiDomain() + "/com.example.helper", PID: 7})
	e.ps(psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"}, psLine{PID: 7, PPID: 1, RSSKB: 20, Comm: exampleBundle + "/" + helper + "/Contents/MacOS/main"})
	it := onlyItem(t, e.run(loginTarget))
	if it.ID != guiDomain()+"/com.example.helper" || it.Name != "Example Helper" || it.State != "running" || it.Memory != 20*1024 {
		t.Errorf("unexpected %+v", it)
	}
	if it.Orphan != "" || len(it.Paths) != 1 || it.Paths[0] != exampleBundle || it.Owner != "Example App" {
		t.Errorf("unexpected %+v", it)
	}
	if !strings.Contains(it.Explain, "Background login item of Example App") {
		t.Errorf("explain %q", it.Explain)
	}
	// The same item without the helper bundle on disk.
	if err := os.RemoveAll(filepath.Join(bundle, helper)); err != nil {
		t.Fatal(err)
	}
	if got := onlyItem(t, e.run(loginTarget)).Orphan; got != "program missing" {
		t.Errorf("orphan %q", got)
	}
}

// An app that opens at login and registers an agent and a login item in its
// own bundle: each helper's process counts for its own row, the app row gets
// the rest, whatever the order of the BTM records.
func TestLoginMemoryHelpersFirst(t *testing.T) {
	e := launchdEnv(t)
	agent, _ := agentInApp(e, "com.example.agent")
	app := oldStyle("com.example.other", exampleBundle)
	agent.ParentID = app.Identifier
	helper := "Contents/Library/LoginItems/Example Helper.app"
	e.exe(filepath.Join(e.Root, exampleBundle, helper, "Contents/MacOS/main"), "")
	li := registered(app, "login item", "4.com.example.helper", helper, "")
	li.BundleID = "com.example.helper"
	e.btm(app, agent, li)
	e.ps(
		psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"},
		psLine{PID: 10, PPID: 1, RSSKB: 10000, Comm: exampleBundle + "/Contents/MacOS/Example App"},
		psLine{PID: 20, PPID: 1, RSSKB: 3000, Comm: exampleBundle + "/Contents/MacOS/example-agent"},
		psLine{PID: 30, PPID: 1, RSSKB: 2000, Comm: exampleBundle + "/" + helper + "/Contents/MacOS/main"},
	)
	e.launchd(
		launchdJob{Target: guiDomain() + "/com.example.agent", PID: 20},
		launchdJob{Target: guiDomain() + "/com.example.helper", PID: 30},
	)
	items := e.run(loginTarget)
	if len(items) != 3 {
		t.Fatalf("got %+v", items)
	}
	for id, kb := range map[string]int64{
		guiDomain() + "/com.example.agent":  3000,
		guiDomain() + "/com.example.helper": 2000,
		"login/com.example.other":           10000,
	} {
		if it := autostartItem(t, items, id); it.State != "running" || it.Memory != kb*1024 {
			t.Errorf("%s: state %q memory %d, want running %d", id, it.State, it.Memory, kb*1024)
		}
	}
}

func TestLoginAgentProgramMissing(t *testing.T) {
	e := launchdEnv(t)
	agent, parent := agentInApp(e, "com.example.agent")
	if err := os.Remove(filepath.Join(e.Root, exampleBundle, "Contents/MacOS/example-agent")); err != nil {
		t.Fatal(err)
	}
	e.btm(parent, agent)
	it := onlyItem(t, e.run(loginTarget))
	if it.Orphan != "program missing" || it.Default != protocol.Disable {
		t.Errorf("orphan %q default %q", it.Orphan, it.Default)
	}
}

func TestLoginAgentAppDeleted(t *testing.T) {
	cases := []struct {
		name   string
		parent btmRecord
		reg    []regRecord
		orphan string
	}{
		{"gone", parentApp("com.example.gone", ""), nil, "app deleted"},
		{"gone with unknown team", parentApp("com.example.gone", "ZZZZZ99999"), nil, "app deleted"},
		{"installed", parentApp("com.example.other", ""), nil, ""},
		{"vendor sibling installed", parentApp("com.example.other.helper", ""), nil, ""},
		{"developer parent", developerParent("Example Corp"), nil, ""},
		{"team installed", parentApp("com.example.gone", "ABCDE12345"), []regRecord{{Path: "/Applications/Example Two.app", ID: "net.example.two", Team: "ABCDE12345", Name: "Example Two", Unmounted: true}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := launchdEnv(t)
			if c.reg != nil {
				otherApp(e, c.reg...)
			}
			agent, _ := agentInApp(e, "com.example.agent")
			agent.ParentID = c.parent.Identifier
			// The bundle is gone when the app is deleted.
			if c.orphan == "app deleted" {
				if err := os.RemoveAll(filepath.Join(e.Root, exampleBundle)); err != nil {
					t.Fatal(err)
				}
			}
			e.btm(c.parent, agent)
			it := onlyItem(t, e.run(loginTarget))
			if it.Orphan != c.orphan {
				t.Errorf("orphan %q, want %q", it.Orphan, c.orphan)
			}
			if it.Default != protocol.Disable {
				t.Errorf("default %q", it.Default)
			}
		})
	}
}

func TestLoginAgentOwnerUnknown(t *testing.T) {
	cases := []struct {
		name, owner, explain string
		dev                  string
	}{
		{"developer name", "Example Corp", "Background agent of Example Corp,", "Example Corp"},
		{"unknown", "", "Background agent of an unknown owner,", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := launchdEnv(t)
			agent := registered(parentApp("com.example.gone", ""), "agent", "8.com.example.agent", "Contents/Library/LaunchAgents/com.example.agent.plist", "Contents/MacOS/x")
			agent.Developer = c.dev
			agent.ParentID = "2.com.example.missing"
			e.btm(agent)
			it := onlyItem(t, e.run(loginTarget))
			// Without a parent record there is no bundle: no path, no orphan.
			if it.Owner != c.owner || it.Orphan != "" || len(it.Paths) != 0 {
				t.Errorf("unexpected %+v", it)
			}
			if it.ID != guiDomain()+"/com.example.agent" || !strings.HasPrefix(it.Explain, c.explain) {
				t.Errorf("id %q explain %q", it.ID, it.Explain)
			}
		})
	}
}

func TestLoginParentFromSameUID(t *testing.T) {
	e := launchdEnv(t)
	agent, parent := agentInApp(e, "com.example.agent")
	other := parentApp("com.example.other", "")
	other.UID = 0
	other.Name = "Wrong Owner"
	e.btm(other, parent, agent)
	if got := onlyItem(t, e.run(loginTarget)).Owner; got != "Example App" {
		t.Errorf("owner %q", got)
	}
}

func TestLoginOtherUIDSkipped(t *testing.T) {
	e := launchdEnv(t)
	bundleAt(e, exampleBundle)
	agent, parent := agentInApp(e, "com.example.agent")
	app := oldStyle("com.example.app", exampleBundle)
	for _, r := range []*btmRecord{&agent, &parent, &app} {
		r.UID = 502
	}
	e.btm(parent, agent, app)
	if items := e.run(loginTarget); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
}

func TestLoginLegacyFlagSkipped(t *testing.T) {
	e := launchdEnv(t)
	agent, parent := agentInApp(e, "com.example.agent")
	agent.Flags = "legacy, curated"
	e.btm(parent, agent)
	if items := e.run(loginTarget); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
}

func TestLoginAppleSkipped(t *testing.T) {
	e := launchdEnv(t)
	bundleAt(e, exampleBundle)
	bundleAt(e, "/System/Applications/Thing.app")
	apple := oldStyle("COM.Apple.thing", exampleBundle)
	system := oldStyle("com.example.sys", "/System/Applications/Thing.app")
	byID := oldStyle("", exampleBundle)
	byID.Identifier = "2.com.apple.byid"
	parent := parentApp("com.example.other", "")
	label := registered(parent, "agent", "8.com.example.agent", "Contents/Library/LaunchAgents/com.apple.label.plist", "Contents/MacOS/x")
	li := registered(parent, "login item", "4.Com.Apple.helper", "Contents/Library/LoginItems/H.app", "")
	appleParent := parentApp("com.apple.parent", "")
	child := registered(appleParent, "login item", "4.com.example.child", "Contents/Library/LoginItems/C.app", "")
	child.Identifier = "4.com.example.child"
	// The control is the only item left.
	ok := registered(parent, "login item", "4.com.example.ok", "Contents/Library/LoginItems/Ok.app", "")
	e.btm(apple, system, byID, parent, label, li, appleParent, child, ok)
	e.plist(filepath.Join(e.Root, exampleBundle, "Contents/Library/LaunchAgents/com.apple.label.plist"), "COM.APPLE.label", "x")
	if it := onlyItem(t, e.run(loginTarget)); it.ID != guiDomain()+"/com.example.ok" {
		t.Errorf("got %+v", it)
	}
}

func TestLoginIDsUnique(t *testing.T) {
	e := launchdEnv(t)
	agent, parent := agentInApp(e, "com.example.agent")
	// Three records of one agent, with the same label and the same url.
	a2, a3 := agent, agent
	a2.Identifier = "8.again"
	a3.Identifier = "8.third"
	bundleAt(e, "/Applications/Example App 2.app")
	// Three apps with one bundle id; two of them at one path.
	app1 := oldStyle("com.example.app", exampleBundle)
	app2 := oldStyle("com.example.app", "/Applications/Example App 2.app")
	app3 := app1
	app4 := app1
	app3.Identifier = "2.again"
	app4.Identifier = "2.third"
	e.btm(parent, agent, a2, a3, app1, app2, app3, app4)
	items := e.run(loginTarget)
	if len(items) != 7 {
		t.Fatalf("got %d items: %+v", len(items), items)
	}
	seen := map[string]bool{}
	for _, it := range items {
		if seen[it.ID] {
			t.Errorf("id %q twice", it.ID)
		}
		seen[it.ID] = true
		if strings.HasPrefix(it.ID, "login//") {
			t.Errorf("double slash in %q", it.ID)
		}
	}
}

func TestLoginItemAbsoluteOrEmptyURL(t *testing.T) {
	e := launchdEnv(t)
	bundleAt(e, exampleBundle)
	parent := parentApp("com.example.other", "")
	e.exe(filepath.Join(e.Root, "opt/example/helper"), "")
	abs := registered(parent, "login item", "4.com.example.abs", "/opt/example/helper", "")
	abs.URL = "/opt/example/helper"
	gone := registered(parent, "login item", "4.com.example.gone", "/opt/example/gone", "")
	gone.URL = "/opt/example/gone"
	empty := registered(parent, "login item", "4.com.example.empty", "", "")
	empty.URL = ""
	e.btm(parent, abs, gone, empty)
	items := e.run(loginTarget)
	if len(items) != 3 {
		t.Fatalf("got %+v", items)
	}
	if got := autostartItem(t, items, guiDomain()+"/com.example.abs").Orphan; got != "" {
		t.Errorf("absolute url, orphan %q", got)
	}
	if got := autostartItem(t, items, guiDomain()+"/com.example.gone").Orphan; got != "program missing" {
		t.Errorf("absolute url to nothing, orphan %q", got)
	}
	// Nothing to look up: not a missing program, and not the bundle itself.
	if it := autostartItem(t, items, guiDomain()+"/com.example.empty"); it.Orphan != "" || !strings.Contains(it.Explain, "Program: unknown.") {
		t.Errorf("empty url: %+v", it)
	}
}

func TestLoginAgentAppDeletedNeedsBundleGone(t *testing.T) {
	e := launchdEnv(t)
	agent, _ := agentInApp(e, "com.example.agent")
	gone := parentApp("com.example.gone", "")
	agent.ParentID = gone.Identifier
	// The registry does not know the app, but its bundle is on disk.
	e.btm(gone, agent)
	if got := onlyItem(t, e.run(loginTarget)).Orphan; got != "" {
		t.Errorf("bundle present, orphan %q", got)
	}
	if err := os.RemoveAll(filepath.Join(e.Root, exampleBundle)); err != nil {
		t.Fatal(err)
	}
	if got := onlyItem(t, e.run(loginTarget)).Orphan; got != "app deleted" {
		t.Errorf("bundle gone, orphan %q", got)
	}
	// A bundle on a volume that may not be mounted is not known to be gone.
	gone.URL = "file:///Volumes/Disk/Example%20App.app/"
	e.btm(gone, agent)
	if got := onlyItem(t, e.run(loginTarget)).Orphan; got != "" {
		t.Errorf("volume, orphan %q", got)
	}
}

func TestLaunchdAppDeletedNeedsBundleGone(t *testing.T) {
	e := launchdEnv(t)
	p := userAgent(e, "com.example.agent")
	programApart(e, p)
	gone := appParent("com.example.gone", "")
	e.btm(legacyRecord(e, os.Getuid(), "legacy agent", p, gone.Identifier), gone)
	// The bundle of the parent record is not on disk.
	if got := onlyItem(t, e.run(launchdTarget)).Orphan; got != "app deleted" {
		t.Errorf("bundle gone, orphan %q", got)
	}
	bundleAt(e, exampleBundle)
	if got := onlyItem(t, e.run(launchdTarget)).Orphan; got != "" {
		t.Errorf("bundle present, orphan %q", got)
	}
}

func TestLoginToolFails(t *testing.T) {
	cases := map[string]func(e *env){
		"ps":         func(e *env) { e.psFails() },
		"btm":        func(e *env) { e.btmFails() },
		"launchctl":  func(e *env) { e.launchctlFails() },
		"print-only": func(e *env) { e.tool("launchctl", `[ "$1" = print ] && exit 1; echo '"x" => enabled'`) },
		"disabled-only": func(e *env) {
			e.tool("launchctl", `[ "$1" = print-disabled ] && { echo failed >&2; exit 1; }; exit 0`)
		},
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			e := launchdEnv(t)
			agent, parent := agentInApp(e, "com.example.agent")
			e.btm(parent, agent, oldStyle("com.example.app", exampleBundle))
			breakIt(e)
			out, stderr := e.runFail(loginTarget)
			if out != "" || stderr == "" {
				t.Errorf("stdout %q, stderr %q", out, stderr)
			}
		})
	}
}

// See TestLaunchdRegistryOnlyWhenNeeded.
func TestLoginRegistryOnlyWhenNeeded(t *testing.T) {
	e := launchdEnv(t)
	agent, _ := agentInApp(e, "com.example.agent")
	gone := parentApp("com.example.gone", "")
	agent.ParentID = gone.Identifier
	e.btm(gone, agent)
	e.registryFails()
	if got := onlyItem(t, e.run(loginTarget)).Orphan; got != "" {
		t.Errorf("bundle present, orphan %q", got)
	}
	if err := os.RemoveAll(filepath.Join(e.Root, exampleBundle)); err != nil {
		t.Fatal(err)
	}
	if out, stderr := e.runFail(loginTarget); out != "" || stderr == "" {
		t.Errorf("bundle gone: stdout %q, stderr %q", out, stderr)
	}
}

func TestLoginAppOnlyNeedsNoLaunchctl(t *testing.T) {
	e := launchdEnv(t)
	bundleAt(e, exampleBundle)
	e.btm(oldStyle("com.example.app", exampleBundle))
	e.launchctlFails()
	onlyItem(t, e.run(loginTarget))
}

func TestProgramMissingHelper(t *testing.T) {
	e := newEnv(t)
	e.exe(filepath.Join(e.Root, "opt/there/tool"), "")
	e.link(filepath.Join(e.Root, "opt/dead"), filepath.Join(e.Root, "gone"))
	e.link(filepath.Join(e.Root, "opt/live"), filepath.Join(e.Root, "opt/there"))
	e.link(filepath.Join(e.Root, "opt/there/deadtool"), "/Volumes/Example Disk/example-tool")
	e.exe(filepath.Join(e.Home, "bin/mine"), "")
	cases := []struct {
		path    string
		missing bool
	}{
		{"/opt/there/tool", false},
		{"/opt/there/gone", true},
		{"/opt/nowhere/deep/tool", true},
		{"/opt/dead/tool", false},
		{"/opt/dead/deeper/tool", false},
		{"/opt/live/tool", false},
		{"/opt/live/gone", true},
		{"/opt/there/deadtool", false},
		{"/Volumes/Disk/tool", false},
		{"relative/tool", false},
		{"", false},
		{e.Home + "/bin/mine", false},
		{e.Home + "/bin/gone", true},
	}
	for _, c := range cases {
		_, _, rc := e.bashRC(". autostart.sh; mc_program_missing '" + c.path + "'")
		if (rc == 0) != c.missing {
			t.Errorf("%q: status %d, missing %v", c.path, rc, c.missing)
		}
	}
}

const kextTarget = "autostart-kext"

const sysextTarget = "autostart-sysext"

// kextEnv is a Mac with the registry, an installed app and no kernel
// extensions. The kext bundle is returned by kextBundle.
func kextEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	otherApp(e)
	e.kexts()
	return e
}

// teamApp installs an app signed by team (the registry has it, so does the
// disk).
func teamApp(e *env, team string) {
	e.t.Helper()
	path := filepath.Join(e.Root, "Applications/Example.app")
	e.app(path, "com.example.app", "Example")
	otherApp(e, regRecord{Path: path, ID: "com.example.app", Team: team, Name: "Example"})
}

// kextBundle writes a bundle with the bundle id in /Library/Extensions.
func kextBundle(e *env, file, id string) string {
	e.t.Helper()
	p := filepath.Join(e.Root, "Library/Extensions", file)
	e.app(p, id, "Example Driver")
	return p
}

func TestAutostartKextAbsent(t *testing.T) {
	e := kextEnv(t)
	// Only Apple's kernel extensions are loaded: nothing else is called.
	e.kexts(kextRow{ID: "com.apple.driver.example", Wired: "0x1000"}, kextRow{ID: "COM.APPLE.Other", Wired: "0x1000"})
	e.registryFails()
	if items := e.run(kextTarget); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
}

func TestAutostartSysextAbsent(t *testing.T) {
	e := newEnv(t)
	e.sysexts(sysextRow{Team: "ZZZZZ99999", ID: "com.apple.example", Name: "Apple"})
	e.psFails()
	if items := e.run(sysextTarget); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
}

func TestKextLoaded(t *testing.T) {
	e := kextEnv(t)
	bundle := kextBundle(e, "Example.kext", "com.example.driver")
	e.codesignTeam(bundle, "ABCDE12345")
	teamApp(e, "ABCDE12345")
	e.kexts(kextRow{ID: "com.example.driver", Wired: "0x5dd0"})

	var stdout, stderr bytes.Buffer
	cmd := e.command(filepath.Join("targets", kextTarget+".sh"))
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr %q: the notice of kmutil must not show", stderr.String())
	}
	items, err := protocol.Parse(&stdout)
	if err != nil {
		t.Fatal(err)
	}
	it := onlyItem(t, items)
	if it.ID != "kext/com.example.driver" || it.Name != "com.example.driver" || it.Category != "Kernel extensions" || !it.Autostart() {
		t.Errorf("unexpected identity %+v", it)
	}
	if it.State != "loaded" || it.Memory != 24016 || it.Enabled != "" || !it.Admin || it.Default != protocol.Stop {
		t.Errorf("unexpected state %+v", it)
	}
	if it.Owner != "" || it.Orphan != "" {
		t.Errorf("owner %q, orphan %q", it.Owner, it.Orphan)
	}
	if len(it.Paths) != 1 || it.Paths[0] != bundle {
		t.Errorf("paths %v", it.Paths)
	}
	if len(it.Commands) != 1 || it.Commands[protocol.Stop] != "sudo kmutil unload -b 'com.example.driver'" {
		t.Errorf("commands %v", it.Commands)
	}
	wantExplain := "Third-party kernel extension that is loaded now. Stop unloads it until the next boot; macOS on Apple Silicon may refuse. Only the uninstaller of the app that installed it removes it for good."
	if it.Explain != wantExplain {
		t.Errorf("explain %q", it.Explain)
	}
}

func TestKextFieldCountVaries(t *testing.T) {
	e := kextEnv(t)
	e.kexts(
		kextRow{ID: "com.example.one", Wired: "0x10", Linked: "1"},
		kextRow{ID: "com.example.two", Wired: "0x20", Linked: "1 2 3 4 5 6 7 8 9 10 11 12"},
		kextRow{ID: "com.example.one", Wired: "0x30"},
	)
	items := e.run(kextTarget)
	if len(items) != 2 {
		t.Fatalf("got %+v, want 2 items", items)
	}
	one, two := autostartItem(t, items, "kext/com.example.one"), autostartItem(t, items, "kext/com.example.two")
	if one.Memory != 16 || two.Memory != 32 {
		t.Errorf("memory %d and %d", one.Memory, two.Memory)
	}
	if len(one.Paths) != 0 {
		t.Errorf("paths %v without a bundle", one.Paths)
	}
}

func TestKextAppleSkipped(t *testing.T) {
	e := kextEnv(t)
	e.kexts(
		kextRow{ID: "com.apple.driver.example", Wired: "0x1000"},
		kextRow{ID: "Com.Apple.Other", Wired: "0x1000"},
		kextRow{ID: "com.example.driver", Wired: "0x2000"},
	)
	it := onlyItem(t, e.run(kextTarget))
	if it.ID != "kext/com.example.driver" {
		t.Errorf("id %q", it.ID)
	}
}

func TestKextQuoting(t *testing.T) {
	e := kextEnv(t)
	e.kexts(kextRow{ID: "com.example.it's", Wired: "0x1000"})
	it := onlyItem(t, e.run(kextTarget))
	if it.Commands[protocol.Stop] != `sudo kmutil unload -b 'com.example.it'\''s'` {
		t.Errorf("command %q", it.Commands[protocol.Stop])
	}
}

func TestKextAppDeleted(t *testing.T) {
	cases := []struct {
		name   string
		team   string // reported by codesign; "-" makes codesign fail, "" is "not set"
		bundle bool
		reg    func(e *env)
		orphan string
	}{
		{"team not installed", "ABCDE12345", true, nil, "app deleted"},
		{"team installed", "ABCDE12345", true, func(e *env) { teamApp(e, "ABCDE12345") }, ""},
		{"team installed but not mounted", "ABCDE12345", true, func(e *env) {
			otherApp(e, regRecord{Path: "/Volumes/X/Example.app", ID: "com.example.app", Team: "ABCDE12345", Name: "Example", Unmounted: true})
		}, ""},
		{"other team installed", "ABCDE12345", true, func(e *env) { teamApp(e, "ZZZZZ99999") }, "app deleted"},
		{"team not set", "", true, nil, ""},
		{"codesign fails", "-", true, nil, ""},
		{"no bundle", "ABCDE12345", false, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := kextEnv(t)
			if c.reg != nil {
				c.reg(e)
			}
			if c.bundle {
				bundle := kextBundle(e, "Example.kext", "com.example.driver")
				if c.team != "-" {
					e.codesignTeam(bundle, c.team)
				} else {
					e.codesignTeam(filepath.Join(e.Root, "elsewhere"), "ABCDE12345")
				}
			}
			e.kexts(kextRow{ID: "com.example.driver", Wired: "0x5dd0"})
			it := onlyItem(t, e.run(kextTarget))
			if it.Orphan != c.orphan {
				t.Errorf("orphan %q, want %q", it.Orphan, c.orphan)
			}
			if it.Default != protocol.Stop {
				t.Errorf("default %q", it.Default)
			}
			if c.bundle == (len(it.Paths) == 0) {
				t.Errorf("paths %v", it.Paths)
			}
		})
	}
}

func TestKextBundleLookup(t *testing.T) {
	e := kextEnv(t)
	kextBundle(e, "Other.kext", "com.example.other")
	want := kextBundle(e, "Wanted.kext", "com.example.driver")
	// Files and bundles without a readable plist do not count.
	e.write(filepath.Join(e.Root, "Library/Extensions/notes.txt"), 1)
	if err := os.MkdirAll(filepath.Join(e.Root, "Library/Extensions/Empty.kext"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.kexts(kextRow{ID: "com.example.driver", Wired: "0x1000"})
	it := onlyItem(t, e.run(kextTarget))
	if len(it.Paths) != 1 || it.Paths[0] != want {
		t.Errorf("paths %v, want %s", it.Paths, want)
	}
}

// The kext target needs only the teams of the registry, not the bundles of
// the installed apps: an app whose Info.plist can't be parsed (which fails
// the full inventory) does not matter.
func TestKextReadsOnlyTheRegistry(t *testing.T) {
	e := kextEnv(t)
	bundle := kextBundle(e, "Example.kext", "com.example.driver")
	e.codesignTeam(bundle, "ABCDE12345")
	teamApp(e, "ABCDE12345")
	e.write(filepath.Join(e.Root, "Applications/Bad.app/Contents/Info.plist"), 1)
	e.kexts(kextRow{ID: "com.example.driver", Wired: "0x1000"})
	if got := onlyItem(t, e.run(kextTarget)).Orphan; got != "" {
		t.Errorf("team installed, orphan %q", got)
	}
}

func TestKextToolFails(t *testing.T) {
	cases := map[string]func(e *env){
		"kmutil":   func(e *env) { e.kmutilFails() },
		"registry": func(e *env) { e.registryFails() },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			e := kextEnv(t)
			e.kexts(kextRow{ID: "com.example.driver", Wired: "0x5dd0"})
			breakIt(e)
			out, stderr := e.runFail(kextTarget)
			if out != "" || stderr == "" {
				t.Errorf("stdout %q, stderr %q", out, stderr)
			}
		})
	}
}

const sysextProcess = "/Library/SystemExtensions/01234567-89AB-CDEF-0123-456789ABCDEF/com.example.ext.systemextension/Contents/MacOS/com.example.ext"

func TestSysextRunning(t *testing.T) {
	e := newEnv(t)
	e.sysexts(sysextRow{Enabled: true, Active: true, Team: "ABCDE12345", ID: "com.example.ext", Name: "Example Extension"})
	e.ps(
		psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"},
		psLine{PID: 50, PPID: 1, RSSKB: 2000, Comm: sysextProcess},
		psLine{PID: 51, PPID: 50, RSSKB: 300, Comm: "/usr/libexec/child"},
		psLine{PID: 52, PPID: 1, RSSKB: 700, Comm: strings.Replace(sysextProcess, "01234567", "76543210", 1)},
		psLine{PID: 53, PPID: 1, RSSKB: 9999, Comm: "/Library/SystemExtensions/x/com.example.extra.systemextension/Contents/MacOS/x"},
	)
	it := onlyItem(t, e.run(sysextTarget))
	if it.ID != "sysext/com.example.ext" || it.Name != "Example Extension" || it.Category != "System extensions" || !it.Autostart() {
		t.Errorf("unexpected identity %+v", it)
	}
	if it.State != "running" || it.Memory != 3000*1024 || it.Enabled != "" || it.Admin || it.Default != "" {
		t.Errorf("unexpected state %+v", it)
	}
	if it.Owner != "" || it.Orphan != "" || len(it.Paths) != 0 {
		t.Errorf("owner %q, orphan %q, paths %v", it.Owner, it.Orphan, it.Paths)
	}
	if len(it.Commands) != 0 || len(it.Actions()) != 0 {
		t.Errorf("commands %v", it.Commands)
	}
	wantExplain := "System extension of the team ABCDE12345, active now. Only the app that installed it can remove it, so remove it or turn it off in that app."
	if it.Explain != wantExplain {
		t.Errorf("explain %q", it.Explain)
	}
}

func TestSysextStoppedAndFiltered(t *testing.T) {
	e := newEnv(t)
	e.sysexts(
		sysextRow{Team: "ABCDE12345", ID: "com.example.ext", Name: "Example Extension"},
		sysextRow{Team: "ABCDE12345", ID: "com.example.waiting", Name: "Waiting", State: "[activated waiting for user]"},
		sysextRow{Team: "ABCDE12345", ID: "com.example.gone", Name: "Gone", State: "[terminated waiting to uninstall on reboot]"},
		sysextRow{Team: "APPLE00000", ID: "com.apple.Example", Name: "Apple"},
		sysextRow{Team: "", ID: "com.example.noteam", Name: "", Version: "2.0"},
	)
	e.ps(psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"})
	items := e.run(sysextTarget)
	if len(items) != 2 {
		t.Fatalf("got %+v, want 2 items", items)
	}
	it := autostartItem(t, items, "sysext/com.example.ext")
	if it.State != "stopped" || it.Memory != 0 {
		t.Errorf("unexpected state %+v", it)
	}
	no := autostartItem(t, items, "sysext/com.example.noteam")
	if no.Name != "com.example.noteam" {
		t.Errorf("name %q, want the bundle id", no.Name)
	}
	if !strings.Contains(no.Explain, "unknown team") {
		t.Errorf("explain %q", no.Explain)
	}
}

func TestSysextMemoryCountedOnce(t *testing.T) {
	e := newEnv(t)
	e.sysexts(
		sysextRow{Team: "ABCDE12345", ID: "com.example.ext", Name: "One"},
		sysextRow{Team: "ABCDE12345", ID: "com.example.ext2", Name: "Two"},
	)
	// The second extension's process is a child of the first one's.
	e.ps(
		psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"},
		psLine{PID: 50, PPID: 1, RSSKB: 1000, Comm: "/x/com.example.ext.systemextension/Contents/MacOS/a"},
		psLine{PID: 51, PPID: 50, RSSKB: 500, Comm: "/x/com.example.ext2.systemextension/Contents/MacOS/b"},
	)
	items := e.run(sysextTarget)
	one, two := autostartItem(t, items, "sysext/com.example.ext"), autostartItem(t, items, "sysext/com.example.ext2")
	if one.Memory+two.Memory != 1500*1024 {
		t.Errorf("memory %d + %d, want 1500 KiB in total", one.Memory, two.Memory)
	}
}

func TestSysextToolFails(t *testing.T) {
	cases := map[string]func(e *env){
		"systemextensionsctl": func(e *env) { e.sysextFails() },
		"ps":                  func(e *env) { e.psFails() },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.sysexts(sysextRow{Team: "ABCDE12345", ID: "com.example.ext", Name: "Example Extension"})
			e.ps(psLine{PID: 1, PPID: 0, RSSKB: 100, Comm: "/sbin/launchd"})
			breakIt(e)
			out, stderr := e.runFail(sysextTarget)
			if out != "" || stderr == "" {
				t.Errorf("stdout %q, stderr %q", out, stderr)
			}
		})
	}
}

// With the records shared through MC_SHARED_AUTOSTART_BTM (as the scan does), both BTM
// targets give the same items as when they read sfltool themselves, and do not
// call it; a shared error makes them fail with its message.
func TestAutostartSharedBTM(t *testing.T) {
	for _, target := range []string{launchdTarget, loginTarget} {
		t.Run(target, func(t *testing.T) {
			e := launchdEnv(t)
			p := userAgent(e, "com.example.agent")
			agent, parent := agentInApp(e, "com.example.login")
			e.btm(legacyRecord(e, os.Getuid(), "legacy agent", p, "Example Corp"), developerParent("Example Corp"), parent, agent)
			e.launchd(launchdJob{Target: guiDomain() + "/com.example.agent"}, launchdJob{Target: guiDomain() + "/com.example.login"})
			want := e.run(target)
			if len(want) != 1 {
				t.Fatalf("with sfltool: %+v", want)
			}
			shared := e.output(e.command("shared/autostart-btm.sh"))
			calls := e.btmCalls()
			got := e.runWith(target, "MC_SHARED_AUTOSTART_BTM="+shared)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("shared records gave\n%+v\nwant\n%+v", got, want)
			}
			stdout, stderr, rc := e.runRC(withEnv(e.command(filepath.Join("targets", target+".sh")), "MC_SHARED_AUTOSTART_BTM_ERR=boom"))
			if rc == 0 || stdout != "" || !strings.Contains(stderr, "mac-cleanup: boom") {
				t.Errorf("shared error: rc = %d, stdout = %q, stderr = %q", rc, stdout, stderr)
			}
			if b, err := os.ReadFile(calls); err == nil {
				t.Errorf("sfltool was called: %s", b)
			}
		})
	}
}
