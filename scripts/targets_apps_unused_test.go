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

// app creates a 16 KiB fake app bundle and registers its mdls answers.
func (e *env) app(name, bundleID, lastUsed string) string {
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
	dir := e.app("Old", "com.example.old", mdlsDate(used))
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
	e.app("New", "com.example.new", mdlsDate(time.Now().AddDate(0, 0, -7)))
	if items := e.run("unused-apps"); len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

func TestUnusedAppApple(t *testing.T) {
	e := newEnv(t)
	e.app("Safari", "com.apple.Safari", mdlsDate(time.Now().AddDate(-3, 0, 0)))
	if items := e.run("unused-apps"); len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

func TestUnusedAppUnknownDate(t *testing.T) {
	e := newEnv(t)
	dir := e.app("Mystery", "(null)", "(null)")
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
	if items[0].Name != "Mystery (last opened: unknown)" {
		t.Errorf("name = %q", items[0].Name)
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
	if items[0].Name != "Plain (last opened: unknown)" {
		t.Errorf("name = %q", items[0].Name)
	}
	if d := items[0].LastUsed - old.Unix(); d < -1 || d > 1 {
		t.Errorf("LastUsed = %d, want mtime %d", items[0].LastUsed, old.Unix())
	}
}

func TestUnusedAppSpotlightDateWins(t *testing.T) {
	e := newEnv(t)
	dir := e.app("Fresh", "com.example.fresh", mdlsDate(time.Now().AddDate(0, 0, -3)))
	old := time.Now().AddDate(-2, 0, 0)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	if items := e.run("unused-apps"); len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}
