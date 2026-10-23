package scripts_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
