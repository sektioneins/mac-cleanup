package main

import (
	"errors"
	"strings"

	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
	"os"
	"path/filepath"
	"testing"
)

func TestScriptsDir(t *testing.T) {
	root := t.TempDir()
	exeDir := filepath.Join(root, "bin")
	cwd := filepath.Join(root, "work")
	if err := os.MkdirAll(exeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(exeDir, "mac-cleanup")

	if got := scriptsDir("/x/custom", exe, cwd); got != "/x/custom" {
		t.Errorf("flag set: got %q", got)
	}
	if got, want := scriptsDir("", exe, cwd), filepath.Join(cwd, "scripts"); got != want {
		t.Errorf("no exe scripts: got %q, want %q", got, want)
	}
	if err := os.MkdirAll(filepath.Join(exeDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, want := scriptsDir("", exe, cwd), filepath.Join(exeDir, "scripts"); got != want {
		t.Errorf("exe dir present: got %q, want %q", got, want)
	}
}

func TestPrintList(t *testing.T) {
	var b strings.Builder
	printList(&b, []scan.Result{
		{Script: "a.sh", Items: []protocol.Item{{
			Category: "Caches", Name: "Foo", Size: 2_500_000_000, Default: protocol.Trash,
			Commands: map[protocol.Action]string{protocol.Delete: "rm x", protocol.Trash: "trash x"},
		}}},
		{Script: "b.sh", Err: errors.New("b.sh: boom")},
	})
	want := "Caches\tFoo\t2.5 GB\ttrash\ttrash x\nFAILED\tb.sh\tb.sh: boom\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

func TestPrintListAutostart(t *testing.T) {
	var b strings.Builder
	printList(&b, []scan.Result{
		{Script: "a.sh", Items: []protocol.Item{
			{
				Tab: protocol.TabAutostart, Category: "Login items", Name: "Example App", Memory: 1_200_000_000,
				Default: protocol.Disable, Commands: map[protocol.Action]string{protocol.Disable: "launchctl disable x"},
			},
			{Tab: protocol.TabAutostart, Category: "Login items", Name: "Other", Memory: 5_000_000},
		}},
	})
	want := "Login items\tExample App\tmem 1.2 GB\tdisable\tlaunchctl disable x\n" +
		"Login items\tOther\tmem 5 MB\t-\t-\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

func TestPrintListEscapesControlCharacters(t *testing.T) {
	var b strings.Builder
	printList(&b, []scan.Result{
		{Script: "a.sh", Items: []protocol.Item{{
			Category: "Caches", Name: "a\x1b[Kb\tc\rd", Size: 2_000, Default: protocol.Delete,
			Commands: map[protocol.Action]string{protocol.Delete: "rm '/x\r'\nls \u0085"},
		}}},
		{Script: "b\x7f.sh", Err: errors.New("b.sh: boom\nmore")},
	})
	want := "Caches\ta\\x1b[Kb\\tc\\rd\t2 KB\tdelete\trm '/x\\r'\\nls \\u0085\n" +
		"FAILED\tb\\x7f.sh\tb.sh: boom\\nmore\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

func TestRunFailsWithoutScripts(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{"-scripts", dir, "-list"}, {"-scripts", dir}} {
		var stdout, stderr strings.Builder
		if code := run(args, &stdout, &stderr); code != 1 {
			t.Errorf("%v: exit %d, want 1", args, code)
		}
		if want := "mac-cleanup: no target scripts in " + dir + "\n"; stderr.String() != want || stdout.String() != "" {
			t.Errorf("%v: stdout %q, stderr %q, want stderr %q", args, stdout.String(), stderr.String(), want)
		}
	}
}

func TestRunListMode(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "targets", "a.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "printf 'item\\nid\\tx\\nname\\tX\\ncategory\\tDeveloper\\nsize\\t2000\\ngrade\\tregenerates\\ndefault\\tdelete\\nexplain\\te\\ncmd_delete\\ttrue\\nend\\n'\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	if code := run([]string{"-scripts", dir, "-list"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if want := "Developer\tX\t2 KB\tdelete\ttrue\n"; stdout.String() != want {
		t.Errorf("stdout %q, want %q", stdout.String(), want)
	}
}

// -list runs the shared step once and gives its env to every target.
func TestRunListModeShared(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	files := map[string]string{
		"shared/x.sh":  "echo run >> '" + count + "'\nprintf shared-value\n",
		"targets/a.sh": "printf 'item\\nid\\ta\\nname\\ta %s\\ncategory\\tDeveloper\\nsize\\t2000\\ngrade\\tregenerates\\ndefault\\tdelete\\nexplain\\te\\ncmd_delete\\ttrue\\nend\\n' \"$MC_SHARED_X\"\n",
		"targets/b.sh": "printf 'item\\nid\\tb\\nname\\tb %s\\ncategory\\tDeveloper\\nsize\\t2000\\ngrade\\tregenerates\\ndefault\\tdelete\\nexplain\\te\\ncmd_delete\\ttrue\\nend\\n' \"$MC_SHARED_X\"\n",
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr strings.Builder
	if code := run([]string{"-scripts", dir, "-list"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if want := "Developer\ta shared-value\t2 KB\tdelete\ttrue\nDeveloper\tb shared-value\t2 KB\tdelete\ttrue\n"; stdout.String() != want {
		t.Errorf("stdout %q, want %q", stdout.String(), want)
	}
	if b, err := os.ReadFile(count); err != nil || string(b) != "run\n" {
		t.Errorf("shared step runs: %q (%v), want one", b, err)
	}
}

func TestResolveExe(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real", "mac-cleanup")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "mac-cleanup")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolveExe(link); got != want {
		t.Errorf("resolveExe(link) = %q, want %q", got, want)
	}
	missing := filepath.Join(dir, "missing")
	if got := resolveExe(missing); got != missing {
		t.Errorf("resolveExe(missing) = %q, want it unchanged", got)
	}
	if got := resolveExe(""); got != "" {
		t.Errorf("resolveExe(\"\") = %q", got)
	}
}
