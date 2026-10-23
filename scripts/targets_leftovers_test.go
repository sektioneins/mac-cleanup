package scripts_test

import (
	"os"
	"path/filepath"
	"strings"
	"time"
	"testing"

	"mac-cleanup/internal/protocol"
)

const leftoversView = "~/Library leftovers"

// leftoverFile writes a file of kb KiB under ~/Library/rel, aged 200 days.
func leftoverFile(e *env, rel string, kb int) string {
	e.t.Helper()
	p := filepath.Join(e.Home, "Library", rel)
	e.write(p, kb)
	e.age(p, 200)
	return p
}

// otherApp registers one unrelated installed app, so the registry is usable.
func otherApp(e *env, extra ...regRecord) {
	e.t.Helper()
	path := filepath.Join(e.Root, "Applications/Other.app")
	e.app(path, "com.example.other", "Other")
	e.registry(append([]regRecord{{Path: path, ID: "com.example.other", Name: "Other"}}, extra...)...)
}

func TestAppLeftoversAbsent(t *testing.T) {
	e := newEnv(t)
	otherApp(e)
	if items := e.run("app-leftovers"); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
}

func TestAppLeftoversRegistryFails(t *testing.T) {
	e := newEnv(t)
	e.registryFails()
	leftoverFile(e, "Containers/com.example.gone/data", 5120)
	out, stderr := e.runFail("app-leftovers")
	if out != "" || stderr == "" {
		t.Errorf("stdout %q, stderr %q", out, stderr)
	}
}

func TestAppLeftoversInstalled(t *testing.T) {
	e := newEnv(t)
	otherApp(e)
	leftoverFile(e, "Containers/com.example.other/data", 5120)
	if items := e.run("app-leftovers"); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
}

func TestAppLeftoversDeleted(t *testing.T) {
	e := newEnv(t)
	otherApp(e)
	file := leftoverFile(e, "Containers/com.example.gone/data", 5120)
	dir := filepath.Dir(file)
	items := e.run("app-leftovers")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	it := items[0]
	if it.ID != "com.example.gone" || it.Category != "Leftovers" || it.Grade != protocol.MayNeed || it.Default != protocol.Trash || it.View != leftoversView {
		t.Errorf("unexpected item %+v", it)
	}
	if it.Name != "com.example.gone leftovers" || it.Size < 5*1024*1024 || len(it.Warnings) != 0 {
		t.Errorf("unexpected fields %+v", it)
	}
	for _, a := range []protocol.Action{protocol.Delete, protocol.Trash} {
		if !strings.Contains(it.Commands[a], "'"+dir+"'") {
			t.Errorf("%s command %q lacks %s", a, it.Commands[a], dir)
		}
	}
	want := "Data in ~/Library from the app com.example.gone, which no installed app claims. Nothing in it was read or changed in the last 90 days. It does not come back, and without the app it is not needed."
	if it.Explain != want {
		t.Errorf("explain %q", it.Explain)
	}
}

func TestAppLeftoversRecent(t *testing.T) {
	for _, kind := range []string{"atime", "mtime"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			otherApp(e)
			leftoverFile(e, "Containers/com.example.gone/old", 5120)
			recent := leftoverFile(e, "Containers/com.example.gone/new", 4)
			now := time.Now()
			var err error
			if kind == "atime" {
				st, serr := os.Stat(recent)
				if serr != nil {
					t.Fatal(serr)
				}
				err = os.Chtimes(recent, now, st.ModTime())
			} else {
				err = os.Chtimes(recent, e.atime(recent), now)
			}
			if err != nil {
				t.Fatal(err)
			}
			if items := e.run("app-leftovers"); len(items) != 0 {
				t.Fatalf("got %+v, want none", items)
			}
		})
	}
}

func TestAppLeftoversApple(t *testing.T) {
	e := newEnv(t)
	otherApp(e)
	leftoverFile(e, "Containers/com.apple.Notes/data", 5120)
	leftoverFile(e, "Preferences/is.workflow.my.app.plist", 5120)
	leftoverFile(e, "Group Containers/XYZ1234567.groups.com.apple.example/data", 5120)
	if items := e.run("app-leftovers"); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
}

func TestAppLeftoversUnknownOwner(t *testing.T) {
	e := newEnv(t)
	otherApp(e)
	leftoverFile(e, "Group Containers/examplegroup/data", 5120)
	leftoverFile(e, "Group Containers/ABCDE12345.xyz/data", 5120)
	if items := e.run("app-leftovers"); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
}

func TestAppLeftoversHelper(t *testing.T) {
	e := newEnv(t)
	helper := filepath.Join(e.Root, "Library/Application Support/Example/Reporter.app")
	e.app(helper, "com.example.reporter", "Reporter")
	otherApp(e, regRecord{Path: helper, ID: "com.example.reporter", Name: "Reporter"})
	leftoverFile(e, "Containers/com.example.reporter/data", 5120)
	if items := e.run("app-leftovers"); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
}

func TestAppLeftoversStaleRecord(t *testing.T) {
	e := newEnv(t)
	otherApp(e, regRecord{Path: filepath.Join(e.Root, "Applications/Gone.app"), ID: "com.example.gone", Name: "Gone App"})
	leftoverFile(e, "Containers/com.example.gone/data", 5120)
	items := e.run("app-leftovers")
	if len(items) != 1 || items[0].Name != "Gone App leftovers" {
		t.Fatalf("unexpected items %+v", items)
	}
}

func TestAppLeftoversUnmounted(t *testing.T) {
	e := newEnv(t)
	otherApp(e, regRecord{Path: "/Volumes/Ext/Ext.app", ID: "com.example.ext", Name: "Ext", Unmounted: true})
	leftoverFile(e, "Containers/com.example.ext/data", 5120)
	if items := e.run("app-leftovers"); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
}

func TestAppLeftoversGrouped(t *testing.T) {
	e := newEnv(t)
	otherApp(e)
	files := []string{
		"Containers/com.example.gone/data",
		"Containers/com.example.gone.Extension/data",
		"Preferences/com.example.gone.plist",
		"Preferences/ByHost/com.example.gone.0A1B2C3D-0000-0000-0000-000000000000.plist",
		"Saved Application State/com.example.gone.savedState/data",
		"Group Containers/ZZZZZZZZZZ.com.example.gone/data",
	}
	for _, f := range files {
		leftoverFile(e, f, 1024)
	}
	items := e.run("app-leftovers")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1: %+v", len(items), items)
	}
	it := items[0]
	if it.ID != "com.example.gone" || len(it.Paths) != 6 || it.Size < 6*1024*1024 {
		t.Errorf("unexpected item %+v", it)
	}
}

func TestAppLeftoversTeamClaimed(t *testing.T) {
	e := newEnv(t)
	otherApp(e, regRecord{Path: filepath.Join(e.Root, "Applications/Other.app"), ID: "com.example.team", Team: "ABCDE12345", Name: "Team"})
	leftoverFile(e, "Group Containers/ABCDE12345.com.example.auth/data", 5120)
	if items := e.run("app-leftovers"); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
}

func TestAppLeftoversGroupPrefixes(t *testing.T) {
	e := newEnv(t)
	otherApp(e)
	leftoverFile(e, "Group Containers/group.QWERT12345.com.example.services/data", 5120)
	items := e.run("app-leftovers")
	if len(items) != 1 || items[0].ID != "com.example.services" {
		t.Fatalf("unexpected items %+v", items)
	}
}

func TestAppLeftoversRunning(t *testing.T) {
	e := newEnv(t)
	otherApp(e)
	e.bundles("com.example.gone")
	leftoverFile(e, "Containers/com.example.gone/data", 5120)
	items := e.run("app-leftovers")
	if len(items) != 1 || len(items[0].Warnings) != 1 || !strings.Contains(items[0].Warnings[0], "is running") {
		t.Fatalf("unexpected items %+v", items)
	}
}

func TestAppLeftoversUnreadable(t *testing.T) {
	e := newEnv(t)
	otherApp(e)
	file := leftoverFile(e, "Containers/com.example.gone/data", 5120)
	locked := filepath.Join(filepath.Dir(file), "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	e.write(filepath.Join(locked, "x"), 1)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if items := e.run("app-leftovers"); len(items) != 0 {
		t.Fatalf("got %+v, want none", items)
	}
}

func TestAppLeftoversKeepsAtime(t *testing.T) {
	e := newEnv(t)
	otherApp(e)
	file := leftoverFile(e, "Containers/com.example.gone/data", 5120)
	before := e.atime(file)
	if items := e.run("app-leftovers"); len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if after := e.atime(file); after.Unix() != before.Unix() {
		t.Errorf("atime changed from %v to %v", before, after)
	}
}
