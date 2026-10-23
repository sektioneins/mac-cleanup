// Package action runs the one command the user chose, byte for byte.
package action

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"mac-cleanup/internal/protocol"
)

// Outcome is the result of running one item's command.
type Outcome struct {
	Key     string
	Action  protocol.Action
	Command string
	Output  string
	Err     error
}

// Run executes it.Commands[a] with /bin/bash -c, unchanged.
func Run(ctx context.Context, it protocol.Item, a protocol.Action) Outcome {
	o := Outcome{Key: it.Key(), Action: a}
	cmd, ok := it.Commands[a]
	if !ok || cmd == "" {
		o.Err = fmt.Errorf("no %s command for this item", a)
		return o
	}
	o.Command = cmd
	c := exec.CommandContext(ctx, "/bin/bash", "-c", cmd)
	// Own process group, so a cancel kills the command and everything it
	// started. SIGKILL: a stopped or tty-blocked child would ignore SIGINT.
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = time.Second
	out, err := c.CombinedOutput()
	o.Output, o.Err = string(out), err
	if err != nil && ctx.Err() != nil {
		o.Err = errors.New("interrupted")
	}
	return o
}

// SudoCmd returns the command that primes sudo credentials.
func SudoCmd() *exec.Cmd { return exec.Command("sudo", "-v") }

// SudoRefresh renews the sudo ticket without prompting; it fails if the
// ticket has expired.
func SudoRefresh() error { return exec.Command("sudo", "-n", "-v").Run() }

// FreeSpace reports available and total bytes of the volume holding path.
func FreeSpace(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bs := uint64(st.Bsize)
	return uint64(st.Bavail) * bs, uint64(st.Blocks) * bs, nil
}

// Reveal shows path in Finder.
func Reveal(path string) error { return exec.Command("open", "-R", path).Start() }
