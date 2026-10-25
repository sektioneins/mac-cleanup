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
	r := Run(context.Background(), p, nil)
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.Script != "ok.sh" || len(r.Items) != 1 || r.Items[0].Script != "ok.sh" || r.Items[0].Key() != "ok.sh/one" {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestRunExitCode(t *testing.T) {
	p := write(t, filepath.Join(t.TempDir(), "bad.sh"), "echo boom >&2\nexit 3\n")
	r := Run(context.Background(), p, nil)
	if r.Err == nil || !strings.Contains(r.Err.Error(), "boom") || !strings.Contains(r.Err.Error(), "bad.sh") {
		t.Fatalf("err = %v", r.Err)
	}
	if len(r.Items) != 0 {
		t.Fatalf("items = %v", r.Items)
	}
}

func TestRunMalformed(t *testing.T) {
	p := write(t, filepath.Join(t.TempDir(), "mal.sh"), "echo garbage\n")
	r := Run(context.Background(), p, nil)
	if r.Err == nil || !strings.Contains(r.Err.Error(), "mal.sh") || len(r.Items) != 0 {
		t.Fatalf("result: %+v", r)
	}
}

func TestRunDuplicateIDs(t *testing.T) {
	p := write(t, filepath.Join(t.TempDir(), "dup.sh"), record+"x\n"+record+"x\n")
	r := Run(context.Background(), p, nil)
	if r.Err == nil || !strings.Contains(r.Err.Error(), "duplicate") || len(r.Items) != 0 {
		t.Fatalf("result: %+v", r)
	}
}

func TestRunTimeout(t *testing.T) {
	p := write(t, filepath.Join(t.TempDir(), "slow.sh"), "sleep 5\n")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	r := Run(ctx, p, nil)
	if r.Err == nil {
		t.Fatal("expected error")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %v", d)
	}
}

func TestShared(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "shared", "x.sh"), "echo one\necho two\n")
	write(t, filepath.Join(dir, "shared", "y.sh"), "echo noise >&2\necho boom >&2\necho >&2\nexit 1\n")
	write(t, filepath.Join(dir, "shared", "big.sh"), "head -c 262145 /dev/zero | tr '\\0' a\n")
	write(t, filepath.Join(dir, "shared", "notes.txt"), "")
	write(t, filepath.Join(dir, "targets", "t.sh"), "echo target >&2; exit 1\n")
	got := Shared(context.Background(), dir)
	want := []string{"MC_SHARED_X=one\ntwo\n", "MC_SHARED_Y_ERR=boom"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSharedLimit(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "shared", "max.sh"), "head -c 262144 /dev/zero | tr '\\0' a\n")
	got := Shared(context.Background(), dir)
	if len(got) != 1 || len(got[0]) != len("MC_SHARED_MAX=")+262144 {
		t.Fatalf("got %d entries", len(got))
	}
}

func TestSharedNames(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "shared", "b-tm.2.sh"), "printf v\n")
	write(t, filepath.Join(dir, "shared", "a.sh"), "exit 3\n")
	got := Shared(context.Background(), dir)
	if len(got) != 2 || !strings.HasPrefix(got[0], "MC_SHARED_A_ERR=a.sh failed: exit status 3") || got[1] != "MC_SHARED_B_TM_2=v" {
		t.Fatalf("got %q", got)
	}
}

func TestSharedTimeout(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "shared", "slow.sh"), "sleep 5\n")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	got := Shared(ctx, dir)
	if len(got) != 1 || !strings.HasPrefix(got[0], "MC_SHARED_SLOW_ERR=slow.sh failed: ") || strings.Contains(got[0], "\n") {
		t.Fatalf("got %q", got)
	}
}

func TestSharedNone(t *testing.T) {
	if got := Shared(context.Background(), t.TempDir()); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestRunEnv(t *testing.T) {
	p := write(t, filepath.Join(t.TempDir(), "env.sh"), `printf 'item\nid\t%s\nname\tN\ncategory\tC\nsize\t10\ngrade\tregenerates\ndefault\tdelete\nexplain\t%s\ncmd_delete\ttrue\nend\n' "$MC_SHARED_X" "$HOME"`+"\n")
	r := Run(context.Background(), p, []string{"MC_SHARED_X=from-env", "HOME=/later/wins"})
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if len(r.Items) != 1 || r.Items[0].ID != "from-env" || r.Items[0].Explain != "/later/wins" {
		t.Fatalf("items %+v", r.Items)
	}
}

// MC_SHARED_* variables in mac-cleanup's own environment never reach a
// script: only the round's env counts.
func TestRunDropsInheritedShared(t *testing.T) {
	t.Setenv("MC_SHARED_BTM_ERR", "stale")
	t.Setenv("MC_SHARED_OTHER", "stale")
	p := write(t, filepath.Join(t.TempDir(), "env.sh"), `printf 'item\nid\t%s\nname\tN\ncategory\tC\nsize\t10\ngrade\tregenerates\ndefault\tdelete\nexplain\t%s %s\ncmd_delete\ttrue\nend\n' "$MC_SHARED_BTM" "${MC_SHARED_BTM_ERR-unset}" "${MC_SHARED_OTHER-unset}"`+"\n")
	r := Run(context.Background(), p, []string{"MC_SHARED_BTM=fresh"})
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if len(r.Items) != 1 || r.Items[0].ID != "fresh" || r.Items[0].Explain != "unset unset" {
		t.Fatalf("items %+v", r.Items)
	}
}

func TestSharedDropsInheritedShared(t *testing.T) {
	t.Setenv("MC_SHARED_X", "stale")
	dir := t.TempDir()
	write(t, filepath.Join(dir, "shared", "x.sh"), `printf '%s' "${MC_SHARED_X-unset}"`+"\n")
	if got := Shared(context.Background(), dir); !reflect.DeepEqual(got, []string{"MC_SHARED_X=unset"}) {
		t.Fatalf("got %q", got)
	}
}

// A NUL byte cannot go into the environment: the source counts as failed, and
// a NUL in its stderr does not reach the message.
func TestSharedNUL(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "shared", "n.sh"), `printf 'a\0b\n'`+"\n")
	write(t, filepath.Join(dir, "shared", "o.sh"), `printf 'bo\0om\n' >&2; exit 1`+"\n")
	got := Shared(context.Background(), dir)
	want := []string{"MC_SHARED_N_ERR=n.sh printed a NUL byte", "MC_SHARED_O_ERR=boom"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	// The entries are usable as an environment.
	p := write(t, filepath.Join(dir, "targets", "t.sh"), record+"x\n")
	if r := Run(context.Background(), p, got); r.Err != nil {
		t.Fatal(r.Err)
	}
}
