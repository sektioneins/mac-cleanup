package scripts_test

import (
	"net/url"
	"os"
	"path/filepath"
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
		"registry":   func(e *env) { e.registryFails() },
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
