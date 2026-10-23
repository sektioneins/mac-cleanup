package scripts_test

import (
	"path/filepath"
	"strings"
	"testing"

	"mac-cleanup/internal/protocol"
)

const chromeBase = "Library/Application Support/Google/Chrome"
const chromePattern = "Google Chrome.app/Contents/MacOS/Google Chrome"

func TestChromeCaches(t *testing.T) {
	e := newEnv(t)
	base := filepath.Join(e.Home, chromeBase)
	dirs := []string{
		filepath.Join(base, "Default/Cache"),
		filepath.Join(base, "Default/Service Worker"),
		filepath.Join(base, "Profile 1/Cache"),
	}
	for _, d := range dirs {
		e.write(filepath.Join(d, "data"), 32)
	}
	e.write(filepath.Join(base, "Default/Cookies"), 32)
	items := e.run("chrome-caches")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	it := items[0]
	if it.ID != "caches" || it.Category != "Browsers" || it.Grade != protocol.Regenerates || it.Default != protocol.Delete || it.Name == "" || it.Explain == "" {
		t.Errorf("unexpected item %+v", it)
	}
	if len(it.Paths) != 3 || it.Size < 3*32768 || len(it.Warnings) != 0 {
		t.Errorf("unexpected fields %+v", it)
	}
	for _, d := range dirs {
		if !strings.Contains(it.Commands[protocol.Delete], "'"+d+"'") {
			t.Errorf("cmd_delete %q lacks %s", it.Commands[protocol.Delete], d)
		}
	}
}

func TestChromeRunning(t *testing.T) {
	e := newEnv(t)
	e.write(filepath.Join(e.Home, chromeBase, "Default/Cache/data"), 32)
	e.running(chromePattern)
	items := e.run("chrome-caches")
	if len(items) != 1 || len(items[0].Warnings) != 1 || !strings.Contains(items[0].Warnings[0], "Chrome") {
		t.Fatalf("unexpected items %+v", items)
	}
}

func TestChromeCrx(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.Home, "Library/Application Support/Google/GoogleUpdater/crx_cache")
	e.write(filepath.Join(dir, "a.crx"), 32)
	items := e.run("chrome-crx-cache")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	it := items[0]
	if it.ID != "crx" || it.Category != "Browsers" || it.Grade != protocol.Redownload || it.Default != protocol.Delete || it.Name == "" || it.Explain == "" {
		t.Errorf("unexpected item %+v", it)
	}
	if len(it.Paths) != 1 || it.Paths[0] != dir || it.Size < 32768 || len(it.Warnings) != 0 {
		t.Errorf("unexpected fields %+v", it)
	}
}

func TestTeamsPartial(t *testing.T) {
	e := newEnv(t)
	base := filepath.Join(e.Home, "Library/Application Support/Microsoft/Teams")
	e.write(filepath.Join(base, "Cache/x"), 32)
	e.write(filepath.Join(base, "GPUCache/x"), 32)
	e.write(filepath.Join(base, "Local Storage/x"), 32)
	items := e.run("teams-caches")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	it := items[0]
	if it.ID != "caches" || it.Category != "App data" || it.Grade != protocol.Regenerates || it.Default != protocol.Delete || it.Name == "" || it.Explain == "" {
		t.Errorf("unexpected item %+v", it)
	}
	want := []string{filepath.Join(base, "Cache"), filepath.Join(base, "GPUCache")}
	if len(it.Paths) != 2 || it.Paths[0] != want[0] || it.Paths[1] != want[1] {
		t.Errorf("paths = %v, want %v", it.Paths, want)
	}
	e.running("Microsoft Teams")
	items = e.run("teams-caches")
	if len(items) != 1 || len(items[0].Warnings) != 1 || !strings.Contains(items[0].Warnings[0], "Teams") {
		t.Errorf("running: %+v", items)
	}
}

func TestOn1(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.Home, "Library/Application Support/ON1/PerfectBrowseCache")
	e.write(filepath.Join(dir, "x"), 32)
	items := e.run("on1-browse-cache")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	it := items[0]
	if it.ID != "browsecache" || it.Category != "App data" || it.Grade != protocol.Regenerates || it.Default != protocol.Delete || it.Name == "" || it.Explain == "" {
		t.Errorf("unexpected item %+v", it)
	}
	if len(it.Paths) != 1 || it.Paths[0] != dir || it.Size < 32768 || len(it.Warnings) != 0 {
		t.Errorf("unexpected fields %+v", it)
	}
	e.running("ON1 Photo RAW")
	items = e.run("on1-browse-cache")
	if len(items) != 1 || len(items[0].Warnings) != 1 || !strings.Contains(items[0].Warnings[0], "ON1 Photo RAW") {
		t.Errorf("running: %+v", items)
	}
}

func TestLogicLeftovers(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.Root, "Library/Application Support/Logic")
	e.write(filepath.Join(dir, "x"), 32)
	items := e.run("logic-leftovers")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	it := items[0]
	if it.ID != "logic" || it.Category != "Leftovers" || it.Grade != protocol.MayNeed || it.Default != protocol.Trash || it.Name == "" || it.Explain == "" {
		t.Errorf("unexpected item %+v", it)
	}
	if len(it.Paths) != 1 || it.Paths[0] != dir || it.Size < 32768 {
		t.Errorf("unexpected fields %+v", it)
	}
	for _, s := range []string{"instrument, loop and plug-in content", "MainStage", "GarageBand"} {
		if !strings.Contains(it.Explain, s) {
			t.Errorf("explain lacks %q: %q", s, it.Explain)
		}
	}
	if strings.Contains(it.Explain, "settings") {
		t.Errorf("explain claims settings: %q", it.Explain)
	}
	e.write(filepath.Join(e.Root, "Applications/Logic Pro.app/Contents/x"), 1)
	if items := e.run("logic-leftovers"); len(items) != 0 {
		t.Errorf("with app installed: got %d items, want 0", len(items))
	}
}

func TestLogicLeftoversOtherApps(t *testing.T) {
	for _, app := range []string{"Logic Pro X.app", "MainStage.app", "MainStage 3.app"} {
		e := newEnv(t)
		e.write(filepath.Join(e.Root, "Library/Application Support/Logic/x"), 32)
		e.write(filepath.Join(e.Root, "Applications", app, "Contents/x"), 1)
		if items := e.run("logic-leftovers"); len(items) != 0 {
			t.Errorf("with %s installed: got %d items, want 0", app, len(items))
		}
	}
}

func TestAppsAbsent(t *testing.T) {
	for _, name := range []string{"chrome-caches", "chrome-crx-cache", "teams-caches", "on1-browse-cache", "logic-leftovers"} {
		e := newEnv(t)
		if items := e.run(name); len(items) != 0 {
			t.Errorf("%s: got %d items, want 0", name, len(items))
		}
	}
}
