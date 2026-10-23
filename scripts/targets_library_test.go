package scripts_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mac-cleanup/internal/protocol"
)

func TestLibraryCaches(t *testing.T) {
	e := newEnv(t)
	caches := filepath.Join(e.Home, "Library/Caches")
	e.write(filepath.Join(caches, "com.example.app/data"), 32)
	e.write(filepath.Join(caches, "Homebrew/x"), 32)
	e.write(filepath.Join(caches, "loose-file"), 32)
	items := e.run("library-caches")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1: %+v", len(items), items)
	}
	it := items[0]
	dir := filepath.Join(caches, "com.example.app")
	if it.ID != "com.example.app" || it.Name != "com.example.app" || it.Category != "Library" || it.Grade != protocol.Regenerates || it.Default != protocol.Trash {
		t.Errorf("unexpected item %+v", it)
	}
	if len(it.Paths) != 1 || it.Paths[0] != dir || it.Size < 32768 || it.Explain == "" {
		t.Errorf("unexpected fields %+v", it)
	}
	if got, want := it.Commands[protocol.Delete], "rm -rf -- '"+dir+"'"; got != want {
		t.Errorf("cmd_delete = %q, want %q", got, want)
	}
	if it.Commands[protocol.Trash] == "" {
		t.Errorf("cmd_trash missing: %v", it.Commands)
	}
}

func TestLibraryCachesAbsent(t *testing.T) {
	e := newEnv(t)
	if items := e.run("library-caches"); len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

func TestLibraryCachesUnreadable(t *testing.T) {
	e := newEnv(t)
	caches := filepath.Join(e.Home, "Library/Caches")
	e.write(filepath.Join(caches, "locked/sub/x"), 32)
	e.write(filepath.Join(caches, "ok/x"), 32)
	if err := os.Chmod(filepath.Join(caches, "locked/sub"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(caches, "locked/sub"), 0o755) })
	items := e.run("library-caches")
	found := false
	for _, it := range items {
		if it.ID == "ok" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ok item missing: %+v", items)
	}
}

func ageFile(t *testing.T, path string, days int) {
	t.Helper()
	old := time.Now().AddDate(0, 0, -days)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryLogs(t *testing.T) {
	e := newEnv(t)
	logs := filepath.Join(e.Home, "Library/Logs")
	oldFile := filepath.Join(logs, "App/old.log")
	e.write(oldFile, 3)
	e.write(filepath.Join(logs, "new.log"), 5)
	ageFile(t, oldFile, 40)
	items := e.run("library-logs")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1: %+v", len(items), items)
	}
	it := items[0]
	if it.ID != "old" || it.Name != "Logs older than 30 days" || it.Category != "Library" || it.Grade != protocol.Regenerates || it.Default != protocol.Delete {
		t.Errorf("unexpected item %+v", it)
	}
	if len(it.Paths) != 1 || it.Paths[0] != logs || it.Explain == "" {
		t.Errorf("unexpected fields %+v", it)
	}
	if it.Size != 3*1024 {
		t.Errorf("size = %d, want %d", it.Size, 3*1024)
	}
	if got, want := it.Commands[protocol.Delete], "find '"+logs+"' -type f -mtime +30 -delete"; got != want || len(it.Commands) != 1 {
		t.Errorf("commands = %v, want delete %q", it.Commands, want)
	}
}

func TestLibraryLogsNoneOld(t *testing.T) {
	e := newEnv(t)
	e.write(filepath.Join(e.Home, "Library/Logs/new.log"), 5)
	if items := e.run("library-logs"); len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

func TestLibraryLogsAbsent(t *testing.T) {
	e := newEnv(t)
	if items := e.run("library-logs"); len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}
