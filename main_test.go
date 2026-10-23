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
