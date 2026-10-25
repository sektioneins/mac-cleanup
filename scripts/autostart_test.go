package scripts_test

import (
	"os"
	"strings"
	"testing"
)

const us = "\x1f"

func fields(f ...string) string { return strings.Join(f, us) + "\n" }

// psTree is pid 10 (1000 KiB) with children 11 (500) and 12 (250) and the
// grandchild 13 (100), plus an unrelated process.
func psTree(e *env) {
	e.ps(
		psLine{1, 0, 4000, "/sbin/launchd"},
		psLine{10, 1, 1000, "/Applications/Example App.app/Contents/MacOS/Example App"},
		psLine{11, 10, 500, "/Applications/Example App.app/Contents/Frameworks/Helper"},
		psLine{12, 10, 250, "/usr/libexec/example-helper"},
		psLine{13, 11, 100, "/usr/libexec/example-grandchild"},
		psLine{99, 1, 77, "/usr/libexec/unrelated"},
	)
}

func TestPSSnapshot(t *testing.T) {
	e := newEnv(t)
	psTree(e)
	out := e.bash(`. autostart.sh; mc_ps_snapshot "$HOME/snap" && wc -l < "$HOME/snap" | tr -d ' '`)
	if strings.TrimSpace(out) != "6" {
		t.Errorf("lines = %q, want 6", out)
	}
}

func TestPSSnapshotFails(t *testing.T) {
	e := newEnv(t)
	e.psFails()
	_, stderr, rc := e.bashRC(`. autostart.sh; mc_ps_snapshot "$HOME/snap"`)
	if rc != 1 || stderr == "" {
		t.Errorf("rc = %d, stderr = %q; want 1 and a message", rc, stderr)
	}
	e.ps()
	_, stderr, rc = e.bashRC(`. autostart.sh; mc_ps_snapshot "$HOME/snap"`)
	if rc != 1 || stderr == "" {
		t.Errorf("empty ps: rc = %d, stderr = %q; want 1 and a message", rc, stderr)
	}
}

func TestTreeRSS(t *testing.T) {
	e := newEnv(t)
	psTree(e)
	out := e.bash(`. autostart.sh; mc_ps_snapshot "$HOME/s"; : >"$HOME/c"; mc_tree_rss "$HOME/s" "$HOME/c" 10`)
	if out != "1894400\n" {
		t.Errorf("rss = %q, want 1894400", out)
	}
}

func TestTreeRSSOnce(t *testing.T) {
	e := newEnv(t)
	psTree(e)
	out := e.bash(`. autostart.sh; mc_ps_snapshot "$HOME/s"; : >"$HOME/c"
mc_tree_rss "$HOME/s" "$HOME/c" 10
mc_tree_rss "$HOME/s" "$HOME/c" 11
mc_tree_rss "$HOME/s" "$HOME/c" 10 12 99
sort -n "$HOME/c" | tr '\n' ' '`)
	if out != "1894400\n0\n78848\n10 11 12 13 99 " {
		t.Errorf("out = %q", out)
	}
}

func TestTreeRSSLoop(t *testing.T) {
	e := newEnv(t)
	e.ps(psLine{20, 21, 300, "/usr/libexec/a"}, psLine{21, 20, 200, "/usr/libexec/b"})
	out := e.bash(`. autostart.sh; mc_ps_snapshot "$HOME/s"; : >"$HOME/c"; mc_tree_rss "$HOME/s" "$HOME/c" 20`)
	if out != "512000\n" {
		t.Errorf("rss = %q, want 512000", out)
	}
}

func TestTreeRSSMissingClaimedFile(t *testing.T) {
	e := newEnv(t)
	psTree(e)
	out := e.bash(`. autostart.sh; mc_ps_snapshot "$HOME/s"; mc_tree_rss "$HOME/s" "$HOME/new" 99; mc_tree_rss "$HOME/s" "$HOME/new" 99 4242`)
	if out != "78848\n0\n" {
		t.Errorf("out = %q", out)
	}
}

func TestTreeRSSBadRoots(t *testing.T) {
	e := newEnv(t)
	psTree(e)
	out := e.bash(`. autostart.sh; mc_ps_snapshot "$HOME/s"; : >"$HOME/c"
mc_tree_rss "$HOME/s" "$HOME/c" 0 1 abc '' -5 1.5
mc_tree_rss "$HOME/s" "$HOME/c" 0 99 abc
wc -l <"$HOME/c" | tr -d ' '`)
	if out != "0\n78848\n1\n" {
		t.Errorf("out = %q, want 0, 78848, 1", out)
	}
	// A claimed path with a backslash is used as is.
	out = e.bash(`. autostart.sh; mc_ps_snapshot "$HOME/s"; mkdir "$HOME/a\\nb"; mc_tree_rss "$HOME/s" "$HOME/a\\nb/c" 99; cat "$HOME/a\\nb/c"`)
	if out != "78848\n99\n" {
		t.Errorf("backslash path: out = %q", out)
	}
}

func TestPidsMatching(t *testing.T) {
	e := newEnv(t)
	e.ps(
		psLine{10, 1, 1, "/Applications/Example App.app/Contents/MacOS/Example App"},
		psLine{11, 10, 1, "/Applications/Example App.app/Contents/Frameworks/Helper"},
		psLine{12, 1, 1, "/Applications/Other.app/Contents/MacOS/Other"},
		psLine{13, 1, 1, "/Library/SystemExtensions/1/com.example.ext.systemextension/Contents/MacOS/com.example.ext"},
	)
	out := e.bash(`. autostart.sh; mc_ps_snapshot "$HOME/s"
mc_pids_matching "$HOME/s" '/Example App.app/' | tr '\n' ' '; echo
mc_pids_matching "$HOME/s" '/com.example.ext.systemextension/' | tr '\n' ' '; echo
mc_pids_matching "$HOME/s" '/Missing.app/' | tr '\n' ' '; echo`)
	if out != "10 11 \n13 \n\n" {
		t.Errorf("out = %q", out)
	}
}

func TestPidsUnder(t *testing.T) {
	e := newEnv(t)
	e.ps(
		psLine{10, 1, 1, "/Applications/Example App.app/Contents/MacOS/Example App"},
		psLine{11, 10, 1, "/Applications/Example App.app/Contents/Frameworks/Helper"},
		psLine{12, 1, 1, "/Applications/Example App.app Two/Contents/MacOS/Other"},
		psLine{13, 1, 1, "/Volumes/Backup/Applications/Example App.app/Contents/MacOS/Example App"},
	)
	out := e.bash(`. autostart.sh; mc_ps_snapshot "$HOME/s"
mc_pids_under "$HOME/s" '/Applications/Example App.app/' | tr '\n' ' '; echo
mc_pids_under "$HOME/s" '/Missing.app/' | tr '\n' ' '; echo`)
	if out != "10 11 \n\n" {
		t.Errorf("out = %q", out)
	}
}

func TestLaunchdJob(t *testing.T) {
	e := newEnv(t)
	e.launchd(
		launchdJob{Target: "gui/501/com.example.run", State: "running", PID: 42, Path: "/Library/Launch Agents/com.example.run.plist"},
		launchdJob{Target: "system/com.example.idle", State: "not running"},
		launchdJob{Target: "system/com.example.wait", State: "waiting", Path: "/Library/LaunchDaemons/com.example.wait.plist"},
	)
	// The third field is the top-level path, not the one of a nested block.
	out := e.bash(`. autostart.sh
mc_launchd_job gui/501/com.example.run
mc_launchd_job system/com.example.idle
mc_launchd_job system/com.example.wait`)
	if out != "running\t42\t/Library/Launch Agents/com.example.run.plist\nnot running\t\t\nwaiting\t\t/Library/LaunchDaemons/com.example.wait.plist\n" {
		t.Errorf("out = %q", out)
	}
	stdout, _, rc := e.bashRC(`. autostart.sh; mc_launchd_job gui/501/com.example.unknown`)
	if rc != 1 || stdout != "" {
		t.Errorf("unknown: rc = %d, stdout = %q; want 1 and nothing", rc, stdout)
	}
}

func TestLaunchdDomainOK(t *testing.T) {
	e := newEnv(t)
	e.launchd()
	for _, d := range []string{"gui/501", "system"} {
		if _, _, rc := e.bashRC(". autostart.sh; mc_launchd_domain_ok " + d); rc != 0 {
			t.Errorf("%s: rc = %d, want 0", d, rc)
		}
	}
	e.launchctlFails()
	if _, _, rc := e.bashRC(". autostart.sh; mc_launchd_domain_ok gui/501"); rc == 0 {
		t.Error("failing launchctl: rc = 0, want non-zero")
	}
}

func TestDisabledLabels(t *testing.T) {
	e := newEnv(t)
	e.disabled("gui/501", "com.example.a", "com.example.c")
	out := e.bash(`. autostart.sh; mc_disabled_labels gui/501 "$HOME/d" && cat "$HOME/d"`)
	if out != "com.example.a\ncom.example.c\n" {
		t.Errorf("out = %q", out)
	}
	// A domain without disabled services gives an empty file.
	out = e.bash(`. autostart.sh; mc_disabled_labels system "$HOME/d" && wc -c < "$HOME/d" | tr -d ' '`)
	if out != "0\n" {
		t.Errorf("system: out = %q, want an empty file", out)
	}
}

func TestDisabledLabelsFails(t *testing.T) {
	e := newEnv(t)
	e.launchctlFails()
	_, stderr, rc := e.bashRC(`. autostart.sh; mc_disabled_labels gui/501 "$HOME/d"`)
	if rc != 1 || stderr == "" {
		t.Errorf("rc = %d, stderr = %q; want 1 and a message", rc, stderr)
	}
	_, _, rc = e.bashRC(`. autostart.sh; mc_launchd_job gui/501/com.example.a`)
	if rc != 1 {
		t.Errorf("mc_launchd_job: rc = %d, want 1", rc)
	}
}

func TestBTMParse(t *testing.T) {
	e := newEnv(t)
	e.btm(
		btmRecord{UID: 501, Type: "legacy agent", Flags: "legacy, curated", Disposition: "enabled, allowed, notified",
			Name: "Example Updater", Identifier: "9.com.example.updater",
			URL:     "file:///Library/Launch%20Agents/com.example.updater.plist",
			ExePath: "/Library/Application Support/Example/updater", ParentID: "Example Corp"},
		btmRecord{UID: 501, Type: "login item", Flags: "sandboxed", Disposition: "enabled, allowed, notified",
			Name: "Example Helper", Developer: "Example Corp", TeamID: "ABCDE12345",
			Identifier: "4.com.example.helper", URL: "Contents/Library/LoginItems/Example%20Helper.app",
			BundleID: "com.example.helper", ParentID: "2.com.example.app"},
		btmRecord{UID: 501, Type: "developer", Disposition: "disabled, allowed, notified",
			Identifier: "Example Corp", Embedded: []string{"9.com.example.updater", "4.com.example.helper"}},
		btmRecord{UID: -2, Type: "legacy daemon", Flags: "legacy", Disposition: "enabled, allowed, notified",
			Name: "Example Daemon", Identifier: "10.com.example.daemon",
			URL: "file:///Library/LaunchDaemons/com.example.daemon.plist", ParentID: "Example Corp"},
		btmRecord{UID: 0, Type: "app", Disposition: "enabled, allowed, notified",
			Name: "Example App", Developer: "Example Corp", TeamID: "ABCDE12345", Identifier: "2.com.example.app",
			URL: "file:///Applications/Example%20App.app/", BundleID: "com.example.app"},
		// Another record of the first section after the other sections were
		// written: it joins its section.
		btmRecord{UID: 501, Type: "app", Disposition: "disabled, allowed, notified",
			Name: "Example Other", Identifier: "3.com.example.other"},
	)
	out := e.bash(`. autostart.sh; mc_btm "$HOME/b" && cat "$HOME/b"`)
	want := fields("501", "legacy agent", "legacy, curated", "enabled, allowed, notified", "Example Updater", "",
		"9.com.example.updater", "file:///Library/Launch%20Agents/com.example.updater.plist",
		"/Library/Application Support/Example/updater", "", "Example Corp", "") +
		fields("501", "login item", "sandboxed", "enabled, allowed, notified", "Example Helper", "Example Corp",
			"4.com.example.helper", "Contents/Library/LoginItems/Example%20Helper.app", "", "com.example.helper",
			"2.com.example.app", "ABCDE12345") +
		fields("501", "developer", "", "disabled, allowed, notified", "", "", "Example Corp", "", "", "", "", "") +
		fields("501", "app", "", "disabled, allowed, notified", "Example Other", "", "3.com.example.other", "", "", "", "", "") +
		fields("-2", "legacy daemon", "legacy", "enabled, allowed, notified", "Example Daemon", "",
			"10.com.example.daemon", "file:///Library/LaunchDaemons/com.example.daemon.plist", "", "", "Example Corp", "") +
		fields("0", "app", "", "enabled, allowed, notified", "Example App", "Example Corp", "2.com.example.app",
			"file:///Applications/Example%20App.app/", "", "com.example.app", "", "ABCDE12345")
	if out != want {
		t.Errorf("out =\n%q\nwant\n%q", out, want)
	}
}

func TestBTMFails(t *testing.T) {
	e := newEnv(t)
	e.btmFails()
	_, stderr, rc := e.bashRC(`. autostart.sh; mc_btm "$HOME/b"`)
	if rc != 1 || stderr == "" {
		t.Errorf("rc = %d, stderr = %q; want 1 and a message", rc, stderr)
	}
	// Output without any UID section is a failure too.
	e.tool("sfltool", "echo 'nothing here'")
	_, stderr, rc = e.bashRC(`. autostart.sh; mc_btm "$HOME/b"`)
	if rc != 1 || stderr == "" {
		t.Errorf("no header: rc = %d, stderr = %q; want 1 and a message", rc, stderr)
	}
}

// btmSample is a few BTM records of two sections.
func btmSample() []btmRecord {
	return []btmRecord{
		{UID: 501, Type: "login item", Flags: "sandboxed", Disposition: "enabled, allowed, notified",
			Name: "Example Helper", Developer: "Example Corp", TeamID: "ABCDE12345",
			Identifier: "4.com.example.helper", URL: "Contents/Library/LoginItems/Example%20Helper.app",
			BundleID: "com.example.helper", ParentID: "2.com.example.app"},
		{UID: -2, Type: "legacy daemon", Flags: "legacy", Disposition: "enabled, allowed, notified",
			Name: "Example Daemon", Identifier: "10.com.example.daemon",
			URL: "file:///Library/LaunchDaemons/com.example.daemon.plist", ParentID: "Example Corp"},
	}
}

// The shared source prints exactly the lines mc_btm writes and ignores the
// shared variables (it is their source).
func TestBTMSource(t *testing.T) {
	e := newEnv(t)
	e.btm(btmSample()...)
	want := e.bash(`. autostart.sh; mc_btm "$HOME/b" && cat "$HOME/b"`)
	if strings.Count(want, "\n") != 2 {
		t.Fatalf("mc_btm wrote %q", want)
	}
	got := e.output(withEnv(e.command("shared/btm.sh"), "MC_SHARED_BTM=bogus", "MC_SHARED_BTM_ERR=bogus"))
	if got != want {
		t.Errorf("btm.sh printed\n%q\nwant\n%q", got, want)
	}
}

func TestBTMSourceFails(t *testing.T) {
	e := newEnv(t)
	e.btmFails()
	_, want, _ := e.bashRC(`. autostart.sh; mc_btm "$HOME/b"`)
	stdout, stderr, rc := e.runRC(e.command("shared/btm.sh"))
	if rc != 1 || stdout != "" || stderr != want || want == "" {
		t.Errorf("rc = %d, stdout = %q, stderr = %q; want 1, nothing and %q", rc, stdout, stderr, want)
	}
}

func TestBTMShared(t *testing.T) {
	e := newEnv(t)
	calls := e.btmCalls()
	shared := fields("501", "app", "", "enabled", "Example App", "", "2.com.example.app", "", "", "", "", "") +
		fields("0", "app", "", "disabled", "Example Other", "", "3.com.example.other", "", "", "", "", "")
	for _, c := range []struct{ value, want string }{
		{shared, shared},
		{strings.TrimSuffix(shared, "\n"), shared},
		{"", ""},
		{"\n", ""},
	} {
		cmd := withEnv(e.command("-c", `. lib.sh; . autostart.sh; mc_btm "$HOME/b" && cat "$HOME/b"`), "MC_SHARED_BTM="+c.value)
		out, stderr, rc := e.runRC(cmd)
		if rc != 0 || out != c.want {
			t.Errorf("MC_SHARED_BTM=%q: rc = %d, out = %q, stderr = %q; want %q", c.value, rc, out, stderr, c.want)
		}
	}
	if b, err := os.ReadFile(calls); err == nil {
		t.Errorf("sfltool was called: %s", b)
	}
}

func TestBTMSharedErr(t *testing.T) {
	e := newEnv(t)
	calls := e.btmCalls()
	for _, c := range []struct{ value, want string }{
		{"mac-cleanup: could not read the background task records (sfltool dumpbtm failed)", "mac-cleanup: could not read the background task records (sfltool dumpbtm failed)\n"},
		{"btm.sh failed: exit status 2", "mac-cleanup: btm.sh failed: exit status 2\n"},
	} {
		// The error wins over a value that is set as well.
		cmd := withEnv(e.command("-c", `. lib.sh; . autostart.sh; mc_btm "$HOME/b"`), "MC_SHARED_BTM_ERR="+c.value, "MC_SHARED_BTM=")
		_, stderr, rc := e.runRC(cmd)
		if rc != 1 || stderr != c.want {
			t.Errorf("rc = %d, stderr = %q; want 1 and %q", rc, stderr, c.want)
		}
	}
	if b, err := os.ReadFile(calls); err == nil {
		t.Errorf("sfltool was called: %s", b)
	}
}

func TestBTMID(t *testing.T) {
	e := newEnv(t)
	out := e.bash(`. autostart.sh; mc_btm_id 2.com.example.app; mc_btm_id 'Example Corp'; mc_btm_id 12.x; mc_btm_id 1password`)
	if out != "com.example.app\nExample Corp\nx\n1password\n" {
		t.Errorf("out = %q", out)
	}
}

func TestURLPath(t *testing.T) {
	e := newEnv(t)
	out := e.bash(`. autostart.sh
mc_url_path 'file:///Library/Launch%20Agents/x%27y.plist'
mc_url_path 'file:///Applications/Example%20App.app/'
mc_url_path 'file:///tmp/%C3%A4%5Cx%25'
mc_url_path 'Contents/Library/LoginItems/Example%20Helper.app'
mc_url_path 'file:///'`)
	want := "/Library/Launch Agents/x'y.plist\n/Applications/Example App.app\n/tmp/ä\\x%\nContents/Library/LoginItems/Example Helper.app\n/\n"
	if out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
}

func TestAppleScriptQuote(t *testing.T) {
	e := newEnv(t)
	out := e.bash(`. autostart.sh; mc_as_quote 'Say "hi" \ there'`)
	if out != `"Say \"hi\" \\ there"`+"\n" {
		t.Errorf("out = %q", out)
	}
}

func TestServiceTarget(t *testing.T) {
	e := newEnv(t)
	out := e.bash(`. autostart.sh; mc_service_target gui/501 "com.exam'ple.a"`)
	if out != `'gui/501/com.exam'\''ple.a'`+"\n" {
		t.Errorf("out = %q", out)
	}
}

// installed.sh defines _MC_US as well; both orders of sourcing work.
func TestSourceWithInstalled(t *testing.T) {
	e := newEnv(t)
	for _, order := range []string{". autostart.sh; . installed.sh", ". installed.sh; . autostart.sh"} {
		out := e.bash(order + `; printf '%s' "$_MC_US" | od -An -c | tr -d ' '`)
		if strings.TrimSpace(out) != "037" {
			t.Errorf("%s: _MC_US = %q", order, out)
		}
	}
}
