// Package action runs the one command the user chose, byte for byte.
package action

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	c.Cancel = func() error {
		err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone // already finished: not an interruption
		}
		return err
	}
	c.WaitDelay = time.Second
	out, err := c.CombinedOutput()
	o.Output, o.Err = string(out), err
	success := c.ProcessState != nil && c.ProcessState.Success()
	o.Err = classify(err, success, ctx.Err() != nil, func() bool { return c.Process != nil && groupStillThere(c.Process.Pid) })
	return o
}

// classify turns the result of Wait into the outcome error. A command that
// exited 0 succeeded, whatever happened to the cancel that raced with its end
// (on macOS that cancel fails with EPERM against the zombie leader). Cancel
// does not map EPERM to "done": it could also mean a privileged survivor,
// which survivors() reports after Wait.
func classify(err error, success, cancelled bool, survivors func() bool) error {
	switch {
	case errors.Is(err, exec.ErrWaitDelay):
		return errors.New("finished, but a background process kept its output open")
	case success || err == nil:
		return nil
	case cancelled:
		if survivors() {
			return errors.New("interrupted, but part of the command could not be stopped and may still be running")
		}
		return errors.New("interrupted")
	}
	return err
}

// groupStillThere reports whether process group pgid has a member we may not
// signal. macOS also answers EPERM for zombies that are about to be reaped, so
// it waits briefly for those to go.
func groupStillThere(pgid int) bool {
	for i := 0; i < 10; i++ {
		if !errors.Is(syscall.Kill(-pgid, 0), syscall.EPERM) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return true
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
