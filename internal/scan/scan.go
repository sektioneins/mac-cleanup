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

// Shared runs each dir/shared/*.sh once, in name order, and returns the
// environment entries that share their output with the targets of one scan
// round: MC_SHARED_<NAME>=<stdout> on success and MC_SHARED_<NAME>_ERR=<one
// line> on failure. NAME is the file name without .sh, upper-cased, with every
// character other than a letter or digit replaced by _. A source whose output
// is larger than SharedLimit gets no entry at all.
func Shared(ctx context.Context, dir string) []string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	paths, _ := filepath.Glob(filepath.Join(abs, "shared", "*.sh"))
	sort.Strings(paths)
	var env []string
	for _, p := range paths {
		base := filepath.Base(p)
		key := "MC_SHARED_" + sharedName(strings.TrimSuffix(base, ".sh"))
		stdout, stderr, err := runBash(ctx, p, nil)
		switch {
		case err != nil:
			env = append(env, key+"_ERR="+sharedErr(base, stderr, err))
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
// non-empty line of its stderr, else "<base> failed: <err>".
func sharedErr(base, stderr string, err error) string {
	lines := strings.Split(stderr, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return strings.Join(strings.Fields(base+" failed: "+err.Error()), " ")
}

// runBash runs script with /bin/bash under Timeout, with os.Environ() plus
// env (later entries win), and returns its stdout and stderr.
func runBash(ctx context.Context, script string, env []string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "/bin/bash", script)
	cmd.Env = append(os.Environ(), env...)
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
// script gets os.Environ() plus env (later entries win).
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
