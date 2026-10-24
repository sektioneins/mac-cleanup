package action

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mac-cleanup/internal/protocol"
)

func item(cmd string) protocol.Item {
	return protocol.Item{Commands: map[protocol.Action]string{protocol.Delete: cmd}}
}

func TestRunExactCommand(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	cmd := "printf '%s' 'a b' > '" + out + "'"
	it := item(cmd)
	o := Run(context.Background(), it, protocol.Delete, nil)
	if o.Err != nil {
		t.Fatalf("err: %v (%s)", o.Err, o.Output)
	}
	if o.Command != cmd {
		t.Errorf("Command = %q, want %q", o.Command, cmd)
	}
	if o.Key != it.Key() || o.Action != protocol.Delete {
		t.Errorf("Key/Action = %q/%q", o.Key, o.Action)
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != "a b" {
		t.Errorf("file = %q, %v", got, err)
	}
}

func TestRunFailure(t *testing.T) {
	o := Run(context.Background(), item("echo boom; exit 3"), protocol.Delete, nil)
	if o.Err == nil {
		t.Fatal("want error")
	}
	if o.Output != "boom\n" {
		t.Errorf("Output = %q", o.Output)
	}
}

func TestRunMissingAction(t *testing.T) {
	o := Run(context.Background(), item("true"), protocol.Trash, nil)
	if o.Err == nil {
		t.Fatal("want error")
	}
	if o.Output != "" || o.Command != "" {
		t.Errorf("executed something: %+v", o)
	}
}

func TestFreeSpace(t *testing.T) {
	free, total, err := FreeSpace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if free == 0 || free > total {
		t.Errorf("free=%d total=%d", free, total)
	}
}

func TestRunCancelKillsProcessGroup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Outcome, 1)
	// The subshell is a second process in the group; killing only bash would
	// leave it running and it would touch the marker.
	go func() { done <- Run(ctx, item("(sleep 1; touch '"+marker+"'); :"), protocol.Delete, nil) }()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case o := <-done:
		if o.Err == nil || o.Err.Error() != "interrupted" {
			t.Errorf("Err = %v, want interrupted", o.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of cancel")
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Error("marker exists: part of the command kept running")
	}
}

func TestRunFinishedCommandNotInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	it := item("true")
	o := Run(ctx, it, protocol.Delete, nil)
	cancel()
	if o.Err != nil {
		t.Fatalf("Err = %v", o.Err)
	}
}

func TestRunBackgroundProcessHoldingOutput(t *testing.T) {
	o := Run(context.Background(), item("sleep 3 & :"), protocol.Delete, nil)
	if o.Err == nil || !strings.Contains(o.Err.Error(), "background process kept its output open") {
		t.Fatalf("Err = %v", o.Err)
	}
}

func TestClassify(t *testing.T) {
	canceling := errors.New("exec: canceling Cmd: operation not permitted")
	exit1 := errors.New("exit status 1")
	cases := []struct {
		name      string
		err       error
		success   bool
		cancelled bool
		want      string // "" means nil
	}{
		{"ok", nil, true, false, ""},
		{"ok but cancelled late", nil, true, true, ""},
		{"exit 0, cancel EPERM race", canceling, true, true, ""},
		{"failed", exit1, false, false, "exit status 1"},
		{"failed and cancelled", exit1, false, true, "interrupted"},
		{"killed", errors.New("signal: killed"), false, true, "interrupted"},
		{"exit 0 but output held open", exec.ErrWaitDelay, true, false, "finished, but a background process kept its output open"},
		{"exit 0, held open, cancelled", exec.ErrWaitDelay, true, true, "finished, but a background process kept its output open"},
	}
	for _, c := range cases {
		got := classify(c.err, c.success, c.cancelled, func() bool { return false })
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%s: got %v, want nil", c.name, got)
		case c.want != "" && (got == nil || got.Error() != c.want):
			t.Errorf("%s: got %v, want %q", c.name, got, c.want)
		}
	}
	got := classify(errors.New("signal: killed"), false, true, func() bool { return true })
	if got == nil || !strings.Contains(got.Error(), "may still be running") {
		t.Errorf("survivor: got %v", got)
	}
}

// chunkWriter records writes and signals the first one.
type chunkWriter struct {
	mu    sync.Mutex
	buf   strings.Builder
	first chan struct{}
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf.Len() == 0 {
		close(w.first)
	}
	w.buf.Write(p)
	return len(p), nil
}

func TestRunStreamsOutput(t *testing.T) {
	w := &chunkWriter{first: make(chan struct{})}
	done := make(chan Outcome, 1)
	go func() { done <- Run(context.Background(), item("echo one; echo two >&2; sleep 1; exit 2"), protocol.Delete, w) }()
	select {
	case <-w.first:
	case <-done:
		t.Fatal("output arrived only after the command finished")
	case <-time.After(900 * time.Millisecond):
		t.Fatal("no output while the command runs")
	}
	o := <-done
	if o.Err == nil {
		t.Fatal("want error")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf.String() != "one\ntwo\n" || o.Output != "one\ntwo\n" {
		t.Fatalf("streamed %q, Output %q", w.buf.String(), o.Output)
	}
}
