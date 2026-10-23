package scripts_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"mac-cleanup/internal/protocol"
)

// env is a sandbox for running target scripts: a fake HOME, a fake MC_ROOT
// and a bin directory that comes first on PATH.
type env struct {
	t               *testing.T
	Home, Root, Bin string
}

// fakePgrep succeeds when its pattern (pgrep -f -- PATTERN) is in pgrep.list.
const fakePgrep = `
while IFS= read -r p; do
	[ "$p" = "$3" ] && exit 0
done < "$(dirname "$0")/pgrep.list"
exit 1
`

func newEnv(t *testing.T) *env {
	t.Helper()
	base := t.TempDir()
	e := &env{t: t, Home: filepath.Join(base, "home"), Root: filepath.Join(base, "root"), Bin: filepath.Join(base, "bin")}
	for _, d := range []string{e.Home, e.Root, e.Bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.tool("pgrep", fakePgrep)
	e.running()
	e.bundles()
	return e
}

func (e *env) mkparent(path string) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		e.t.Fatal(err)
	}
}

// write creates a file of kb KiB, creating parent directories.
func (e *env) write(path string, kb int) {
	e.t.Helper()
	e.mkparent(path)
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, kb*1024), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// exe writes an executable bash script at an absolute path.
func (e *env) exe(path, body string) {
	e.t.Helper()
	e.mkparent(path)
	if err := os.WriteFile(path, []byte("#!/bin/bash\n"+body+"\n"), 0o755); err != nil {
		e.t.Fatal(err)
	}
}

// tool writes an executable bash script into Bin.
func (e *env) tool(name, body string) { e.exe(filepath.Join(e.Bin, name), body) }

// link creates a symlink at path pointing to target.
func (e *env) link(path, target string) {
	e.t.Helper()
	e.mkparent(path)
	if err := os.Symlink(target, path); err != nil {
		e.t.Fatal(err)
	}
}

// running makes the fake pgrep succeed for exactly the given -f patterns.
func (e *env) running(patterns ...string) {
	e.t.Helper()
	list := strings.Join(patterns, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(e.Bin, "pgrep.list"), []byte(list), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) command(args ...string) *exec.Cmd {
	cmd := exec.Command("/bin/bash", args...)
	cmd.Env = append(os.Environ(),
		"HOME="+e.Home,
		"MC_ROOT="+e.Root,
		"PATH="+e.Bin+":/usr/bin:/bin:/usr/sbin:/sbin",
	)
	return cmd
}

func (e *env) output(cmd *exec.Cmd) string {
	e.t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		e.t.Fatalf("%v: %v\nstderr: %s", cmd.Args, err, stderr.String())
	}
	return stdout.String()
}

// run executes targets/<target>.sh and parses its records.
func (e *env) run(target string) []protocol.Item {
	e.t.Helper()
	out := e.output(e.command(filepath.Join("targets", target+".sh")))
	items, err := protocol.Parse(strings.NewReader(out))
	if err != nil {
		e.t.Fatalf("parse %s output: %v\n%s", target, err, out)
	}
	return items
}

// runFail executes targets/<target>.sh, expects a non-zero exit and
// returns stdout and stderr.
func (e *env) runFail(target string) (string, string) {
	e.t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := e.command(filepath.Join("targets", target+".sh"))
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err == nil {
		e.t.Fatalf("%s: exit 0, want a failure\nstdout: %s", target, stdout.String())
	}
	return stdout.String(), stderr.String()
}

// bash sources lib.sh, runs the snippet and returns stdout.
func (e *env) bash(snippet string) string {
	e.t.Helper()
	return e.output(e.command("-c", ". lib.sh; "+snippet))
}

// lsregisterRel is the Launch Services registry tool, relative to MC_ROOT.
const lsregisterRel = "System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

// regRecord is one record of the fake Launch Services registry dump.
type regRecord struct {
	Path, ID, Team, Name string
	Unmounted            bool
}

// registry writes a fake lsregister that prints a dump of the records.
func (e *env) registry(recs ...regRecord) {
	e.t.Helper()
	var b strings.Builder
	field := func(k, v string) { fmt.Fprintf(&b, "%-28s%s\n", k+":", v) }
	b.WriteString("Checking data integrity......\n")
	for _, r := range recs {
		b.WriteString(strings.Repeat("-", 80) + "\n")
		if r.Path != "" {
			field("path", r.Path+" (0xa1a4)")
		}
		field("name", r.Name)
		field("displayName", r.Name)
		team := r.Team
		if team == "" {
			team = "0000000000"
		}
		field("teamID", team)
		field("identifier", r.ID)
		field("executable", "Contents/MacOS/"+r.Name)
		if r.Unmounted {
			field("mount state", "not mounted")
		} else {
			field("mount state", "mounted")
		}
	}
	b.WriteString(strings.Repeat("-", 80) + "\n")
	stub := filepath.Join(e.Root, lsregisterRel)
	e.mkparent(stub)
	if err := os.WriteFile(stub+".dump", []byte(b.String()), 0o644); err != nil {
		e.t.Fatal(err)
	}
	e.exe(stub, `cat "$0.dump"`)
}

// registryFails makes the fake lsregister exit 1.
func (e *env) registryFails() {
	e.t.Helper()
	e.exe(filepath.Join(e.Root, lsregisterRel), "echo broken >&2; exit 1")
}

// app writes a minimal Info.plist for a bundle at path.
func (e *env) app(path, id, name string) {
	e.t.Helper()
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>` + id + `</string>
<key>CFBundleName</key><string>` + name + `</string>
<key>CFBundleExecutable</key><string>` + name + `</string>
</dict></plist>
`
	f := filepath.Join(path, "Contents", "Info.plist")
	e.mkparent(f)
	if err := os.WriteFile(f, []byte(plist), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// bundles writes a fake lsappinfo that lists the given bundle ids.
func (e *env) bundles(ids ...string) {
	e.t.Helper()
	body := "echo 'ASN:0x0-0x1:'\necho '    bundleID=[ NULL ] '\n"
	for _, id := range ids {
		body += fmt.Sprintf("echo '    bundleID=\"%s\"'\n", id)
	}
	e.tool("lsappinfo", body)
}

// age sets atime and mtime of path and everything under it to now minus days.
func (e *env) age(path string, days int) {
	e.t.Helper()
	ts := time.Now().AddDate(0, 0, -days)
	err := filepath.WalkDir(path, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(p, ts, ts)
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

// atime returns the access time of path.
func (e *env) atime(path string) time.Time {
	e.t.Helper()
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		e.t.Fatal(err)
	}
	return time.Unix(st.Atimespec.Sec, st.Atimespec.Nsec)
}
