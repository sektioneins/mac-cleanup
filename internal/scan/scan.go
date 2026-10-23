// Package scan discovers target scripts and runs them to collect items.
package scan

import (
	"bytes"
	"context"
	"fmt"
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

// Run executes script with /bin/bash and parses its output into items.
func Run(ctx context.Context, script string) Result {
	name := filepath.Base(script)
	res := Result{Script: name}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "/bin/bash", script)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = 500 * time.Millisecond

	fail := func(err error) Result {
		res.Err = fmt.Errorf("%s: %w%s", name, err, stderrTail(stderr.String()))
		return res
	}

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			err = fmt.Errorf("%w (%v)", err, ctx.Err())
		}
		return fail(err)
	}
	items, err := protocol.Parse(&stdout)
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
