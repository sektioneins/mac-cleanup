package scripts_test

import (
	"bytes"
	"fmt"
	"html"
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
	// Groups are the application-groups entitlement values.
	Groups []string
	// GroupString is a single-string application-groups entitlement, written
	// before the mount state.
	GroupString string
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
		if r.GroupString != "" {
			field("entitlements", "1 values (...)")
			ind := strings.Repeat(" ", 28)
			b.WriteString(ind + "{\n" + ind + "    \"com.apple.security.application-groups\" = \"" + r.GroupString + "\";\n" + ind + "}\n")
		}
		if r.Unmounted {
			field("mount state", "not mounted")
		} else {
			field("mount state", "mounted")
		}
		if len(r.Groups) > 0 {
			field("entitlements", "6 values (...)")
			ind := strings.Repeat(" ", 28)
			b.WriteString(ind + "{\n")
			b.WriteString(ind + "    \"com.apple.security.application-groups\" =     (\n")
			for i, g := range r.Groups {
				sep := ","
				if i == len(r.Groups)-1 {
					sep = ""
				}
				// Values are quoted, except those with a bare word.
				if strings.HasPrefix(g, "bare:") {
					b.WriteString(ind + "        " + strings.TrimPrefix(g, "bare:") + sep + "\n")
				} else {
					b.WriteString(ind + "        \"" + g + "\"" + sep + "\n")
				}
			}
			b.WriteString(ind + "    );\n")
			b.WriteString(ind + "    \"com.apple.security.app-sandbox\" = 1;\n")
			b.WriteString(ind + "    \"some.other.key\" =     (\n")
			b.WriteString(ind + "        \"x\"\n")
			b.WriteString(ind + "    );\n")
			b.WriteString(ind + "}\n")
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

// bashRC sources lib.sh, runs the snippet and returns stdout, stderr and the
// exit status (a failure is not fatal).
func (e *env) bashRC(snippet string) (string, string, int) {
	e.t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := e.command("-c", ". lib.sh; "+snippet)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return stdout.String(), stderr.String(), ee.ExitCode()
	}
	e.t.Fatal(err)
	return "", "", -1
}

// psLine is one process of the fake `ps -axo pid=,ppid=,rss=,comm=`.
type psLine struct {
	PID, PPID, RSSKB int
	Comm             string
}

// ps writes a fake ps that prints the processes in the layout of the real one
// (right-aligned numbers, then the comm, which may contain spaces).
func (e *env) ps(lines ...psLine) {
	e.t.Helper()
	var b strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&b, "%5d %5d %6d %s\n", l.PID, l.PPID, l.RSSKB, l.Comm)
	}
	if err := os.WriteFile(filepath.Join(e.Bin, "ps.out"), []byte(b.String()), 0o644); err != nil {
		e.t.Fatal(err)
	}
	e.tool("ps", `cat "$(dirname "$0")/ps.out"`)
}

// psFails makes the fake ps exit 1.
func (e *env) psFails() {
	e.t.Helper()
	e.tool("ps", "echo 'ps: operation not permitted' >&2; exit 1")
}

// launchdJob is a loaded launchd job of the fake launchctl. Target is
// gui/501/label or system/label; State defaults to running; the pid line is
// printed only when PID > 0.
type launchdJob struct {
	Target, State string
	PID           int
}

// text is the `launchctl print` output: tab-indented key = value lines with
// nested blocks, which carry state and pid lines of their own.
func (j launchdJob) text() string {
	label := j.Target[strings.LastIndex(j.Target, "/")+1:]
	state := j.State
	if state == "" {
		state = "running"
	}
	var b strings.Builder
	b.WriteString(j.Target + " = {\n")
	b.WriteString("\tactive count = 1\n")
	b.WriteString("\tpath = /Users/example/Library/LaunchAgents/" + label + ".plist\n")
	b.WriteString("\ttype = LaunchAgent\n")
	b.WriteString("\tproperties = {\n\t\tstate = nested-before\n\t\tpid = 998\n\t}\n")
	b.WriteString("\tstate = " + state + "\n")
	b.WriteString("\tprogram = /Applications/Example App.app/Contents/MacOS/Example Agent\n")
	b.WriteString("\tenvironment = {\n\t\tstate = nested-after\n\t\tpid = 999\n\t}\n")
	if j.PID > 0 {
		fmt.Fprintf(&b, "\tpid = %d\n", j.PID)
	}
	b.WriteString("\tlast exit code = (never exited)\n}\n")
	return b.String()
}

const fakeLaunchctl = `
dir=$(dirname "$0")
case "$1" in
print)
	i=0
	while IFS= read -r t; do
		i=$((i + 1))
		if [ "$t" = "$2" ]; then
			cat "$dir/launchctl.job.$i"
			exit 0
		fi
	done < "$dir/launchctl.targets"
	case "$2" in
	gui/*/*)
		echo 'Bad request.'
		uid=${2#gui/}
		echo "Could not find service \"${2##*/}\" in domain for user gui: ${uid%%/*}"
		exit 113
		;;
	system/*)
		echo 'Bad request.'
		echo "Could not find service \"${2##*/}\" in domain for system"
		exit 113
		;;
	gui/*|system) exit 0 ;;
	esac
	exit 1
	;;
print-disabled)
	f="$dir/launchctl.disabled.$(printf '%s' "$2" | tr '/' '_')"
	printf '\tdisabled services = {\n'
	[ -f "$f" ] && cat "$f"
	printf '\t\t"com.example.enabled" => enabled\n\t}\n'
	exit 0
	;;
esac
echo "launchctl: unexpected subcommand $1" >&2
exit 1
`

// launchd writes a fake launchctl that serves `print TARGET` for the jobs
// (exit 113 for any other service), `print gui/<n>` and `print system`
// (exit 0), and `print-disabled DOMAIN` (see disabled). Every other
// subcommand exits 1.
func (e *env) launchd(jobs ...launchdJob) {
	e.t.Helper()
	var targets strings.Builder
	for i, j := range jobs {
		targets.WriteString(j.Target + "\n")
		name := filepath.Join(e.Bin, fmt.Sprintf("launchctl.job.%d", i+1))
		if err := os.WriteFile(name, []byte(j.text()), 0o644); err != nil {
			e.t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(e.Bin, "launchctl.targets"), []byte(targets.String()), 0o644); err != nil {
		e.t.Fatal(err)
	}
	e.tool("launchctl", fakeLaunchctl)
}

// disabled makes `launchctl print-disabled DOMAIN` report the labels as
// disabled. It also lists one enabled service, which parsers must skip.
func (e *env) disabled(domain string, labels ...string) {
	e.t.Helper()
	if _, err := os.Stat(filepath.Join(e.Bin, "launchctl")); err != nil {
		e.launchd()
	}
	var b strings.Builder
	for _, l := range labels {
		b.WriteString("\t\t\"" + l + "\" => disabled\n")
	}
	name := filepath.Join(e.Bin, "launchctl.disabled."+strings.ReplaceAll(domain, "/", "_"))
	if err := os.WriteFile(name, []byte(b.String()), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// launchctlFails makes the fake launchctl exit 1 for every subcommand.
func (e *env) launchctlFails() {
	e.t.Helper()
	e.tool("launchctl", "echo 'launchctl: failed' >&2; exit 1")
}

// btmRecord is one record of the fake `sfltool dumpbtm`. Empty Name, Developer
// and URL are printed as (null), like the real tool; the other empty fields
// are not printed. Flags and Disposition are the bracket contents, e.g. "legacy,
// curated" and "enabled, allowed, notified". Embedded are the ids of an
// `Embedded Item Identifiers:` list (developer records).
type btmRecord struct {
	UID                                                                 int
	Type, Flags, Disposition, Name, Developer, Identifier, URL, ExePath string
	BundleID, ParentID, TeamID                                          string
	Embedded                                                            []string
}

func btmText(recs []btmRecord) string {
	var uids []int
	byUID := map[int][]btmRecord{}
	for _, r := range recs {
		if _, ok := byUID[r.UID]; !ok {
			uids = append(uids, r.UID)
		}
		byUID[r.UID] = append(byUID[r.UID], r)
	}
	var b strings.Builder
	for _, uid := range uids {
		b.WriteString("========================\n")
		fmt.Fprintf(&b, " Records for UID %d : 01234567-89AB-CDEF-0123-456789ABCDEF\n", uid)
		b.WriteString("========================\n\n")
		for i, r := range byUID[uid] {
			fmt.Fprintf(&b, " #%d:\n", i+1)
			field := func(k, v string) { fmt.Fprintf(&b, "%21s: %s\n", k, v) }
			opt := func(k, v string) {
				if v != "" {
					field(k, v)
				}
			}
			field("UUID", fmt.Sprintf("01234567-89AB-CDEF-0123-4567890000%02d", i+1))
			nul := func(v string) string {
				if v == "" {
					return "(null)"
				}
				return v
			}
			field("Name", nul(r.Name))
			field("Developer Name", nul(r.Developer))
			opt("Team Identifier", r.TeamID)
			field("Type", r.Type+" (0x2)")
			field("Flags", "[ "+r.Flags+" ] (0x40)")
			field("Disposition", "["+r.Disposition+"] (0xb)")
			opt("Identifier", r.Identifier)
			field("URL", nul(r.URL))
			opt("Executable Path", r.ExePath)
			field("Generation", "1")
			opt("Bundle Identifier", r.BundleID)
			opt("Parent Identifier", r.ParentID)
			if len(r.Embedded) > 0 {
				b.WriteString("  Embedded Item Identifiers:\n")
				for k, id := range r.Embedded {
					fmt.Fprintf(&b, "    #%d: %s\n", k+1, id)
				}
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// btm writes a fake sfltool whose `dumpbtm` prints the records in the layout
// of the real tool: a section per UID (in order of first appearance) with the
// records numbered from #1.
func (e *env) btm(recs ...btmRecord) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.Bin, "btm.out"), []byte(btmText(recs)), 0o644); err != nil {
		e.t.Fatal(err)
	}
	e.tool("sfltool", `[ "$1" = dumpbtm ] || exit 1
cat "$(dirname "$0")/btm.out"`)
}

// btmFails makes the fake sfltool exit 1.
func (e *env) btmFails() {
	e.t.Helper()
	e.tool("sfltool", "echo 'sfltool: not permitted' >&2; exit 1")
}

// plist writes an XML property list with a Label and, when args are given, a
// ProgramArguments array (which /usr/bin/plutil can read).
func (e *env) plist(path, label string, args ...string) {
	e.t.Helper()
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>` + html.EscapeString(label) + `</string>
`)
	if len(args) > 0 {
		b.WriteString("<key>ProgramArguments</key><array>\n")
		for _, a := range args {
			b.WriteString("<string>" + html.EscapeString(a) + "</string>\n")
		}
		b.WriteString("</array>\n")
	}
	b.WriteString("</dict></plist>\n")
	e.mkparent(path)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		e.t.Fatal(err)
	}
}
