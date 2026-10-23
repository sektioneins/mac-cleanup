package scripts_test

import (
	"path/filepath"
	"strings"
	"testing"

	"mac-cleanup/internal/protocol"
)

type devTarget struct {
	name, id, rel, pattern string
}

var devTargets = []devTarget{
	{"xcode-deriveddata", "deriveddata", "Library/Developer/Xcode/DerivedData", "Xcode.app/Contents/MacOS/Xcode"},
	{"gradle-caches", "caches", ".gradle/caches", "GradleDaemon"},
	{"vscode-workspace", "workspacestorage", "Library/Application Support/Code/User/workspaceStorage", "Visual Studio Code.app/Contents/MacOS"},
}

func TestDevTargetsFound(t *testing.T) {
	for _, tc := range devTargets {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			dir := filepath.Join(e.Home, tc.rel)
			e.write(filepath.Join(dir, "sub", "data"), 32)
			items := e.run(tc.name)
			if len(items) != 1 {
				t.Fatalf("got %d items, want 1", len(items))
			}
			it := items[0]
			if it.ID != tc.id || it.Category != "Developer" || it.Grade != protocol.Regenerates || it.Default != protocol.Delete {
				t.Errorf("unexpected item %+v", it)
			}
			if len(it.Paths) != 1 || it.Paths[0] != dir {
				t.Errorf("paths = %v, want [%s]", it.Paths, dir)
			}
			if it.Size < 32768 {
				t.Errorf("size = %d", it.Size)
			}
			if it.Admin || len(it.Warnings) != 0 || it.Name == "" || it.Explain == "" {
				t.Errorf("unexpected fields %+v", it)
			}
			if got, want := it.Commands[protocol.Delete], "rm -rf -- '"+dir+"'"; got != want {
				t.Errorf("cmd_delete = %q, want %q", got, want)
			}
			if !strings.Contains(it.Commands[protocol.Trash], dir) {
				t.Errorf("cmd_trash = %q", it.Commands[protocol.Trash])
			}
		})
	}
}

func TestDevTargetsRunning(t *testing.T) {
	for _, tc := range devTargets {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.write(filepath.Join(e.Home, tc.rel, "data"), 32)
			e.running(tc.pattern)
			items := e.run(tc.name)
			if len(items) != 1 || len(items[0].Warnings) != 1 {
				t.Fatalf("items = %+v", items)
			}
		})
	}
}

func TestDevTargetsAbsent(t *testing.T) {
	for _, tc := range devTargets {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			if items := e.run(tc.name); len(items) != 0 {
				t.Fatalf("got %d items, want 0", len(items))
			}
		})
	}
}
