package scan

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const record = `printf 'item\nid\t%s\nname\tN\ncategory\tC\nsize\t10\ngrade\tregenerates\ndefault\tdelete\nexplain\tE\ncmd_delete\ttrue\nend\n' `

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "targets", "b.sh"), "")
	write(t, filepath.Join(dir, "targets", "a.sh"), "")
	write(t, filepath.Join(dir, "targets", "notes.txt"), "")
	write(t, filepath.Join(dir, "lib.sh"), "")
	got, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "targets", "a.sh"), filepath.Join(dir, "targets", "b.sh")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRunOK(t *testing.T) {
	p := write(t, filepath.Join(t.TempDir(), "ok.sh"), record+"one\n")
	r := Run(context.Background(), p)
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.Script != "ok.sh" || len(r.Items) != 1 || r.Items[0].Script != "ok.sh" || r.Items[0].Key() != "ok.sh/one" {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestRunExitCode(t *testing.T) {
	p := write(t, filepath.Join(t.TempDir(), "bad.sh"), "echo boom >&2\nexit 3\n")
	r := Run(context.Background(), p)
	if r.Err == nil || !strings.Contains(r.Err.Error(), "boom") || !strings.Contains(r.Err.Error(), "bad.sh") {
		t.Fatalf("err = %v", r.Err)
	}
	if len(r.Items) != 0 {
		t.Fatalf("items = %v", r.Items)
	}
}

func TestRunMalformed(t *testing.T) {
	p := write(t, filepath.Join(t.TempDir(), "mal.sh"), "echo garbage\n")
	r := Run(context.Background(), p)
	if r.Err == nil || !strings.Contains(r.Err.Error(), "mal.sh") || len(r.Items) != 0 {
		t.Fatalf("result: %+v", r)
	}
}

func TestRunDuplicateIDs(t *testing.T) {
	p := write(t, filepath.Join(t.TempDir(), "dup.sh"), record+"x\n"+record+"x\n")
	r := Run(context.Background(), p)
	if r.Err == nil || !strings.Contains(r.Err.Error(), "duplicate") || len(r.Items) != 0 {
		t.Fatalf("result: %+v", r)
	}
}

func TestRunTimeout(t *testing.T) {
	p := write(t, filepath.Join(t.TempDir(), "slow.sh"), "sleep 5\n")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	r := Run(ctx, p)
	if r.Err == nil {
		t.Fatal("expected error")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %v", d)
	}
}
