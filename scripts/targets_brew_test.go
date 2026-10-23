package scripts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mac-cleanup/internal/protocol"
)

func fakeBrewDryRun(line string) string {
	return `case "$1" in
cleanup) echo 'Removing: /x/foo... (1 file)'; ` + line + ` ;;
esac`
}

func TestBrewCleanupSize(t *testing.T) {
	e := newEnv(t)
	e.tool("brew", fakeBrewDryRun(`echo '==> This operation would free approximately 1.5GB of disk space.'`))
	items := e.run("brew-cleanup")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	it := items[0]
	if it.ID != "cleanup" || it.Name != "Homebrew cleanup" || it.Category != "Developer" || it.Grade != protocol.Redownload || it.Default != protocol.Delete {
		t.Errorf("unexpected item %+v", it)
	}
	if it.Size != 1500000000 {
		t.Errorf("size = %d", it.Size)
	}
	if len(it.Paths) != 0 || it.Explain == "" {
		t.Errorf("unexpected fields %+v", it)
	}
	if got := it.Commands[protocol.Delete]; got != brewCleanupCmd || len(it.Commands) != 1 {
		t.Errorf("commands = %v", it.Commands)
	}
}

const brewCleanupCmd = "HOMEBREW_NO_AUTOREMOVE=1 brew cleanup --prune=all"

func TestBrewCleanupNoAutoremove(t *testing.T) {
	e := newEnv(t)
	record := filepath.Join(e.Bin, "brew.env")
	e.tool("brew", `{ echo "args=$*"; env; } >> '`+record+`'
echo '==> This operation would free approximately 2GB of disk space.'`)
	items := e.run("brew-cleanup")
	if len(items) != 1 || items[0].Commands[protocol.Delete] != brewCleanupCmd {
		t.Fatalf("items = %+v, want cmd_delete %q", items, brewCleanupCmd)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "args=cleanup --prune=all --dry-run\n") || !strings.Contains(got, "\nHOMEBREW_NO_AUTOREMOVE=1\n") {
		t.Errorf("scan call did not set HOMEBREW_NO_AUTOREMOVE=1:\n%s", got)
	}
}

func TestBrewCleanupFailedWithSize(t *testing.T) {
	e := newEnv(t)
	e.tool("brew", `echo '==> This operation would free approximately 1GB of disk space.'
echo 'Error: first problem' >&2
echo 'Error: second problem' >&2
exit 1`)
	items := e.run("brew-cleanup")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	want := "brew cleanup --dry-run failed: Error: first problem"
	if w := items[0].Warnings; len(w) != 1 || w[0] != want {
		t.Errorf("warnings = %q, want [%q]", w, want)
	}
	if items[0].Size != 1000000000 {
		t.Errorf("size = %d", items[0].Size)
	}
}

func TestBrewCleanupFailedNothing(t *testing.T) {
	e := newEnv(t)
	e.tool("brew", `echo 'Error: no network' >&2; exit 1`)
	stdout, stderr := e.runFail("brew-cleanup")
	if stdout != "" || !strings.Contains(stderr, "brew cleanup --dry-run failed: Error: no network") {
		t.Errorf("stdout %q, stderr %q", stdout, stderr)
	}
}

func TestBrewCleanupUnits(t *testing.T) {
	for line, want := range map[string]int64{
		"512MB": 512000000, "12KB": 12000, "100B": 100, "2GB": 2000000000, "0.5KB": 500,
	} {
		e := newEnv(t)
		e.tool("brew", fakeBrewDryRun(`echo '==> This operation would free approximately `+line+` of disk space.'`))
		items := e.run("brew-cleanup")
		if len(items) != 1 || items[0].Size != want {
			t.Errorf("%s: got %+v, want size %d", line, items, want)
		}
	}
}

func TestBrewCleanupNothing(t *testing.T) {
	e := newEnv(t)
	e.tool("brew", fakeBrewDryRun(`:`))
	if items := e.run("brew-cleanup"); len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

func TestBrewAbsent(t *testing.T) {
	e := newEnv(t)
	for _, name := range []string{"brew-cleanup", "brew-cask-installers", "brew-intel"} {
		if items := e.run(name); len(items) != 0 {
			t.Errorf("%s: got %d items, want 0", name, len(items))
		}
	}
}

func TestCaskInstallers(t *testing.T) {
	e := newEnv(t)
	prefix := filepath.Join(e.Root, "opt/homebrew")
	e.tool("brew", `[ "$1" = --prefix ] && echo '`+prefix+`'`)
	pkg := filepath.Join(prefix, "Caskroom/mactex/2026.0324/mactex-20260324.pkg")
	e.write(pkg, 32)
	e.write(filepath.Join(prefix, "Caskroom/other/1.0/other.dmg"), 32)
	e.write(filepath.Join(prefix, "Caskroom/other/1.0/readme.txt"), 32)
	items := e.run("brew-cask-installers")
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(items), items)
	}
	var it protocol.Item
	for _, x := range items {
		if x.ID == "mactex/2026.0324/mactex-20260324.pkg" {
			it = x
		}
	}
	if it.ID == "" {
		t.Fatalf("pkg item missing: %+v", items)
	}
	if it.Name != "mactex 2026.0324 installer" || it.Category != "Developer" || it.Grade != protocol.Redownload || it.Default != protocol.Delete {
		t.Errorf("unexpected item %+v", it)
	}
	if len(it.Paths) != 1 || it.Paths[0] != pkg || it.Size < 32768 || it.Explain == "" {
		t.Errorf("unexpected fields %+v", it)
	}
	if got, want := it.Commands[protocol.Delete], "rm -rf -- '"+pkg+"'"; got != want {
		t.Errorf("cmd_delete = %q, want %q", got, want)
	}
}

func TestCaskInstallersBrewFails(t *testing.T) {
	e := newEnv(t)
	e.tool("brew", `echo 'Error: broken' >&2; exit 1`)
	stdout, stderr := e.runFail("brew-cask-installers")
	if stdout != "" || !strings.Contains(stderr, "brew --prefix failed") {
		t.Errorf("stdout %q, stderr %q", stdout, stderr)
	}
}

func TestBrewIntel(t *testing.T) {
	e := newEnv(t)
	e.write(filepath.Join(e.Root, "usr/local/Homebrew/bin/brew"), 32)
	if items := e.run("brew-intel"); len(items) != 0 {
		t.Fatalf("only Intel: got %d items, want 0", len(items))
	}
	e.write(filepath.Join(e.Root, "opt/homebrew/bin/brew"), 32)
	e.write(filepath.Join(e.Root, "usr/local/Cellar/foo/1/x"), 32)
	items := e.run("brew-intel")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	it := items[0]
	if it.ID != "intel" || it.Name != "Old Intel Homebrew in /usr/local" || it.Category != "Developer" || it.Grade != protocol.MayNeed || it.Default != protocol.Trash {
		t.Errorf("unexpected item %+v", it)
	}
	want := []string{filepath.Join(e.Root, "usr/local/Homebrew"), filepath.Join(e.Root, "usr/local/Cellar")}
	if len(it.Paths) != 2 || it.Paths[0] != want[0] || it.Paths[1] != want[1] {
		t.Errorf("paths = %v, want %v", it.Paths, want)
	}
	if it.Size < 65536 || it.Explain == "" || it.Commands[protocol.Trash] == "" {
		t.Errorf("unexpected fields %+v", it)
	}
}

func TestMactexOld(t *testing.T) {
	e := newEnv(t)
	base := filepath.Join(e.Root, "usr/local/texlive")
	for _, y := range []string{"2025", "2026"} {
		e.write(filepath.Join(base, y, "bin/universal-darwin/tex"), 32)
	}
	e.write(filepath.Join(base, "texmf-local/x"), 32)
	e.link(filepath.Join(e.Root, "Library/TeX/texbin"), filepath.Join(base, "2026/bin/universal-darwin"))
	items := e.run("mactex-old")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1: %+v", len(items), items)
	}
	it := items[0]
	dir := filepath.Join(base, "2025")
	if it.ID != "2025" || it.Name != "TeX Live 2025" || it.Category != "Developer" || it.Grade != protocol.Redownload || it.Default != protocol.Delete {
		t.Errorf("unexpected item %+v", it)
	}
	if len(it.Paths) != 1 || it.Paths[0] != dir || it.Size < 32768 || it.Explain == "" {
		t.Errorf("unexpected fields %+v", it)
	}
	if got, want := it.Commands[protocol.Delete], "rm -rf -- '"+dir+"'"; got != want {
		t.Errorf("cmd_delete = %q, want %q", got, want)
	}
}

func TestMactexNoTexbin(t *testing.T) {
	e := newEnv(t)
	e.write(filepath.Join(e.Root, "usr/local/texlive/2025/x"), 32)
	if items := e.run("mactex-old"); len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}
