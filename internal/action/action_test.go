package action

import (
	"context"
	"os"
	"path/filepath"
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
	o := Run(context.Background(), it, protocol.Delete)
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
	o := Run(context.Background(), item("echo boom; exit 3"), protocol.Delete)
	if o.Err == nil {
		t.Fatal("want error")
	}
	if o.Output != "boom\n" {
		t.Errorf("Output = %q", o.Output)
	}
}

func TestRunMissingAction(t *testing.T) {
	o := Run(context.Background(), item("true"), protocol.Trash)
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
	go func() { done <- Run(ctx, item("sleep 30; touch '"+marker+"'"), protocol.Delete) }()
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
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Error("marker exists: command kept running")
	}
}
