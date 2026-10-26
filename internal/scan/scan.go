// Package scan discovers target scripts and runs them to collect items.
package scan

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mac-cleanup/internal/protocol"
)

// Timeout bounds a single script run.
const Timeout = 3 * time.Minute

// Result is the outcome of running one target script.
type Result struct {
	Script string
	Items  []protocol.Item
	Err    error
}

// Discover returns the absolute, sorted paths of dir/targets/*.sh.
func Discover(dir string) ([]string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	paths, err := filepath.Glob(filepath.Join(abs, "targets", "*.sh"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

// SharedLimit is the largest output of a shared source that is passed on in
// the environment; a larger one is left out, which keeps the environment well
// under the system's limit for arguments and environment together.
const SharedLimit = 256 << 10

// AutostartPrefix starts the file names of the targets and shared sources of
// the Autostart tab; all other scripts belong to the Space tab.
const AutostartPrefix = "autostart-"

// Autostart reports whether the script at path (or with that base name)
// belongs to the Autostart tab.
func Autostart(path string) bool {
	return strings.HasPrefix(filepath.Base(path), AutostartPrefix)
}

// Shared runs the dir/shared/*.sh of one tab once, in name order: those of
// the Autostart tab if autostart is true, else those of the Space tab. It
// returns the environment entries that share their output with the targets of
// that tab's scan round: MC_SHARED_<NAME>=<stdout> on success and
// MC_SHARED_<NAME>_ERR=<one line> on failure. NAME is the file name without
// .sh, upper-cased, with every character other than a letter or digit
// replaced by _. A source whose output is larger than SharedLimit gets no
// entry at all.
func Shared(ctx context.Context, dir string, autostart bool) []string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	paths, _ := filepath.Glob(filepath.Join(abs, "shared", "*.sh"))
	sort.Strings(paths)
	var env []string
	for _, p := range paths {
		if Autostart(p) != autostart {
			continue
		}
		base := filepath.Base(p)
		key := SharedPrefix + sharedName(strings.TrimSuffix(base, ".sh"))
		stdout, stderr, err := runBash(ctx, p, nil)
		switch {
		case err != nil:
			env = append(env, key+"_ERR="+sharedErr(base, stderr, err))
		case strings.IndexByte(stdout, 0) >= 0:
			// The environment cannot hold a NUL byte.
			env = append(env, key+"_ERR="+base+" printed a NUL byte")
		case len(stdout) <= SharedLimit:
			env = append(env, key+"="+stdout)
		}
	}
	return env
}

// sharedName upper-cases name and replaces everything but ASCII letters and
// digits with _.
func sharedName(name string) string {
	b := []byte(strings.ToUpper(name))
	for i, c := range b {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			b[i] = '_'
		}
	}
	return string(b)
}

// sharedErr is the one-line message of a failed shared source: the last
// non-empty line of its stderr (NUL bytes removed), else "<base> failed:
// <err>".
func sharedErr(base, stderr string, err error) string {
	lines := strings.Split(strings.ReplaceAll(stderr, "\x00", ""), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return strings.Join(strings.Fields(base+" failed: "+err.Error()), " ")
}

// SharedPrefix starts the names of the variables of a scan round.
const SharedPrefix = "MC_SHARED_"

// baseEnv is os.Environ() without MC_SHARED_* variables, so ones inherited
// from mac-cleanup's own environment cannot override or shadow those of the
// round.
func baseEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, SharedPrefix) {
			env = append(env, kv)
		}
	}
	return env
}

// runBash runs script with /bin/bash under Timeout, with baseEnv() plus env
// (later entries win), and returns its stdout and stderr.
func runBash(ctx context.Context, script string, env []string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "/bin/bash", script)
	cmd.Env = append(baseEnv(), env...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = 500 * time.Millisecond

	err := cmd.Run()
	if err != nil && ctx.Err() != nil {
		err = fmt.Errorf("%w (%v)", err, ctx.Err())
	}
	return stdout.String(), stderr.String(), err
}

// Run executes script with /bin/bash and parses its output into items. The
// script gets os.Environ() without inherited MC_SHARED_* variables, plus env
// (later entries win).
func Run(ctx context.Context, script string, env []string) Result {
	name := filepath.Base(script)
	res := Result{Script: name}

	stdout, stderr, err := runBash(ctx, script, env)
	fail := func(err error) Result {
		res.Err = fmt.Errorf("%s: %w%s", name, err, stderrTail(stderr))
		return res
	}

	if err != nil {
		return fail(err)
	}
	items, err := protocol.Parse(strings.NewReader(stdout))
	if err != nil {
		return fail(err)
	}
	seen := map[string]bool{}
	for i := range items {
		items[i].Script = name
		if seen[items[i].ID] {
			return fail(fmt.Errorf("duplicate id %q", items[i].ID))
		}
		seen[items[i].ID] = true
	}
	res.Items = items
	return res
}

// stderrTail formats the last 5 non-empty lines of s for an error message.
func stderrTail(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	tail := strings.TrimSpace(strings.Join(lines, "\n"))
	if tail == "" {
		return ""
	}
	return "\n" + tail
}
