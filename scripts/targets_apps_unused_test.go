package scripts_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mac-cleanup/internal/protocol"
)

// fakeMdls answers `mdls -raw -name ATTR PATH` from mdls.table next to it.
// Lines are PATH|ATTR|VALUE; a missing line makes mdls fail like Spotlight.
const fakeMdls = `
attr=$3
path=$4
while IFS='|' read -r p a v; do
	if [ "$p" = "$path" ] && [ "$a" = "$attr" ]; then
		printf '%s' "$v"
		exit 0
	fi
done < "$(dirname "$0")/mdls.table"
echo "could not find $path." >&2
exit 1
`

// mdlsApp creates a 16 KiB fake app bundle and registers its mdls answers.
func (e *env) mdlsApp(name, bundleID, lastUsed string) string {
	e.t.Helper()
	dir := filepath.Join(e.Root, "Applications", name+".app")
	e.write(filepath.Join(dir, "Contents/MacOS/"+name), 16)
	e.tool("mdls", fakeMdls)
	table := filepath.Join(e.Bin, "mdls.table")
	f, err := os.OpenFile(table, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		e.t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(dir + "|kMDItemCFBundleIdentifier|" + bundleID + "\n" +
		dir + "|kMDItemLastUsedDate|" + lastUsed + "\n"); err != nil {
		e.t.Fatal(err)
	}
	return dir
}

func mdlsDate(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 +0000") }

func TestUnusedAppOld(t *testing.T) {
	e := newEnv(t)
	used := time.Now().AddDate(-2, 0, 0)
	dir := e.mdlsApp("Old", "com.example.old", mdlsDate(used))
	items := e.run("unused-apps")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	it := items[0]
	if it.ID != "Old.app" || it.Category != "Unused apps" || it.Grade != protocol.MayNeed || it.Default != protocol.Trash {
		t.Errorf("unexpected item %+v", it)
	}
	if want := "Old (last opened " + used.Local().Format("Jan 2006") + ")"; it.Name != want {
		t.Errorf("name = %q, want %q", it.Name, want)
	}
	if d := it.LastUsed - used.Unix(); d < -1 || d > 1 {
		t.Errorf("LastUsed = %d, want %d", it.LastUsed, used.Unix())
	}
	if len(it.Paths) != 1 || it.Paths[0] != dir || it.Size < 16384 {
		t.Errorf("paths/size = %v %d", it.Paths, it.Size)
	}
	if it.Explain == "" {
		t.Error("missing explain")
	}
}

func TestUnusedAppRecent(t *testing.T) {
	e := newEnv(t)
	e.mdlsApp("New", "com.example.new", mdlsDate(time.Now().AddDate(0, 0, -7)))
	if items := e.run("unused-apps"); len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

func TestUnusedAppApple(t *testing.T) {
	e := newEnv(t)
	e.mdlsApp("Safari", "com.apple.Safari", mdlsDate(time.Now().AddDate(-3, 0, 0)))
	if items := e.run("unused-apps"); len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

func TestUnusedAppUnknownDate(t *testing.T) {
	e := newEnv(t)
	dir := e.mdlsApp("Mystery", "(null)", "(null)")
	old := time.Now().AddDate(-2, 0, 0)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	items := e.run("unused-apps")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if d := items[0].LastUsed - old.Unix(); d < -1 || d > 1 {
		t.Errorf("LastUsed = %d, want mtime %d", items[0].LastUsed, old.Unix())
	}
	if want := "Mystery (never opened per Spotlight, unchanged since " + old.Format("Jan 2006") + ")"; items[0].Name != want {
		t.Errorf("name = %q, want %q", items[0].Name, want)
	}
	want := "macOS has no record of this app being opened. The app has not changed since " + old.Format("Jan 2006") +
		", which is usually when it was installed or last updated. It does not come back by itself; you would have to install it again, and its settings stay in your Library."
	if items[0].Explain != want {
		t.Errorf("explain = %q, want %q", items[0].Explain, want)
	}
}

func TestUnusedAppKnownDateExplain(t *testing.T) {
	e := newEnv(t)
	e.mdlsApp("Old", "com.example.old", mdlsDate(time.Now().AddDate(-2, 0, 0)))
	items := e.run("unused-apps")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	want := "The application Old.app, not opened for over a year. It does not come back by itself; you would have to install it again, and its settings stay in your Library."
	if items[0].Explain != want || len(items[0].Warnings) != 0 {
		t.Errorf("explain = %q, warnings %q", items[0].Explain, items[0].Warnings)
	}
}

func TestUnusedAppRunningWarns(t *testing.T) {
	e := newEnv(t)
	dir := e.mdlsApp("Old", "com.example.old", mdlsDate(time.Now().AddDate(-2, 0, 0)))
	e.tool("pgrep", regexPgrep)
	if err := os.WriteFile(filepath.Join(e.Bin, "procs.list"), []byte(dir+"/Contents/MacOS/Old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	items := e.run("unused-apps")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if w := items[0].Warnings; len(w) != 1 || w[0] != "Old is running: quit it first" {
		t.Errorf("warnings = %q", w)
	}
}

func TestUnusedAppMdlsFails(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.Root, "Applications", "Plain.app")
	e.write(filepath.Join(dir, "Contents/Info.plist"), 16)
	e.tool("mdls", "exit 1")
	old := time.Now().AddDate(-2, 0, 0)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	items := e.run("unused-apps")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if want := "Plain (never opened per Spotlight, unchanged since " + old.Format("Jan 2006") + ")"; items[0].Name != want {
		t.Errorf("name = %q, want %q", items[0].Name, want)
	}
	if d := items[0].LastUsed - old.Unix(); d < -1 || d > 1 {
		t.Errorf("LastUsed = %d, want mtime %d", items[0].LastUsed, old.Unix())
	}
}

func TestUnusedAppSpotlightDateWins(t *testing.T) {
	e := newEnv(t)
	dir := e.mdlsApp("Fresh", "com.example.fresh", mdlsDate(time.Now().AddDate(0, 0, -3)))
	old := time.Now().AddDate(-2, 0, 0)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	if items := e.run("unused-apps"); len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

func TestUnusedAppRunningRegexNames(t *testing.T) {
	for _, name := range []string{"Foo (Beta)", "C++", "What?", "App [x]", "Money$", "X{2}"} {
		e := newEnv(t)
		dir := e.mdlsApp(name, "com.example.x", mdlsDate(time.Now().AddDate(-2, 0, 0)))
		e.tool("pgrep", regexPgrep)
		if err := os.WriteFile(filepath.Join(e.Bin, "procs.list"), []byte(dir+"/Contents/MacOS/x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		items := e.run("unused-apps")
		if len(items) != 1 {
			t.Fatalf("%s: got %d items, want 1", name, len(items))
		}
		if w := items[0].Warnings; len(w) != 1 || w[0] != name+" is running: quit it first" {
			t.Errorf("%s: warnings = %q", name, w)
		}
	}
}
