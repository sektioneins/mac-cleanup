package scripts_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"mac-cleanup/internal/protocol"
)

func TestQuoteRoundTrip(t *testing.T) {
	e := newEnv(t)
	for _, in := range []string{"plain", "with space", "it's", "a'b'c", "line\nbreak", "trailing\n", "-leading", "$HOME", "`x`"} {
		t.Setenv("X", in)
		got := e.bash(`eval "printf %s $(mc_quote "$X")"`)
		if got != in {
			t.Errorf("round trip of %q gave %q", in, got)
		}
	}
}

func TestFieldEscapingRoundTrip(t *testing.T) {
	e := newEnv(t)
	for _, want := range []string{"a\\b\tc\nd", `a\\b`, `a\nb`, `a\tb`, `\`, `x\` + "\n"} {
		checkFieldRoundTrip(t, e, want)
	}
}

func checkFieldRoundTrip(t *testing.T, e *env, want string) {
	t.Helper()
	t.Setenv("X", want)
	out := e.bash(`mc_item; mc_field id x; mc_field name "$X"; mc_field category Developer
mc_field size 1; mc_field grade regenerates; mc_field default delete
mc_field explain e; mc_field cmd_delete 'rm -rf -- /x'; mc_end`)
	items, err := protocol.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	if len(items) != 1 || items[0].Name != want {
		t.Errorf("got %+v, want name %q", items, want)
	}
}

func TestRmCmd(t *testing.T) {
	e := newEnv(t)
	got := e.bash(`mc_rm_cmd 0 "/a b" "/c'd"`)
	want := `rm -rf -- '/a b' '/c'\''d'`
	if strings.TrimSuffix(got, "\n") != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got = e.bash(`mc_rm_cmd 1 /a`)
	if !strings.HasPrefix(got, "sudo rm -rf -- ") {
		t.Errorf("admin command %q lacks sudo prefix", got)
	}
}

func TestTrashCmd(t *testing.T) {
	e := newEnv(t)
	got := strings.TrimSuffix(e.bash(`mc_trash_cmd 0 "/a b"`), "\n")
	if want := `/usr/bin/trash '/a b'`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got = strings.TrimSuffix(e.bash(`mc_trash_cmd 1 "/a b"`), "\n")
	re := regexp.MustCompile(`^\[ ! -e '` + regexp.QuoteMeta(e.Home) + `/\.Trash/a b \d{8}-\d{6} \d+' \] && sudo mv -- '/a b' '` + regexp.QuoteMeta(e.Home) + `/\.Trash/a b \d{8}-\d{6} \d+'$`)
	if !re.MatchString(got) {
		t.Errorf("admin trash command %q does not match %s", got, re)
	}
	got = strings.TrimSuffix(e.bash(`mc_trash_cmd 1 /a /b`), "\n")
	if strings.Count(got, " && ") != 3 || strings.Count(got, "sudo mv -- ") != 2 {
		t.Errorf("two paths should give two guarded mv joined by &&: %q", got)
	}
}

func TestSize(t *testing.T) {
	e := newEnv(t)
	f := filepath.Join(e.Root, "f")
	e.write(f, 64)
	got, err := strconv.Atoi(strings.TrimSpace(e.bash(`mc_size ` + f)))
	if err != nil || got < 65536 || got > 69632 {
		t.Errorf("size = %d (%v), want 65536..69632", got, err)
	}
	if got := strings.TrimSpace(e.bash(`mc_size ` + filepath.Join(e.Root, "missing"))); got != "0" {
		t.Errorf("missing path size = %q, want 0", got)
	}
}

func TestNeedsAdmin(t *testing.T) {
	e := newEnv(t)
	open := filepath.Join(e.Root, "open", "f")
	e.write(open, 1)
	if got := strings.TrimSpace(e.bash(`mc_needs_admin ` + open)); got != "0" {
		t.Errorf("writable: got %q, want 0", got)
	}
	locked := filepath.Join(e.Root, "locked", "f")
	e.write(locked, 1)
	dir := filepath.Dir(locked)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if got := strings.TrimSpace(e.bash(`mc_needs_admin ` + locked)); got != "1" {
		t.Errorf("read-only parent: got %q, want 1", got)
	}
}

func TestRunningCheck(t *testing.T) {
	e := newEnv(t)
	if got := e.bash(`mc_warn_running GradleDaemon Gradle`); got != "" {
		t.Errorf("not running: got %q, want nothing", got)
	}
	e.running("GradleDaemon")
	want := "warn\tGradle is running: quit it first\n"
	if got := e.bash(`mc_warn_running GradleDaemon Gradle`); got != want {
		t.Errorf("running: got %q, want %q", got, want)
	}
}

func TestRunningCheckError(t *testing.T) {
	e := newEnv(t)
	e.tool("pgrep", "exit 3")
	want := "warn\tCould not check whether Gradle is running\n"
	if got := e.bash(`mc_warn_running GradleDaemon Gradle`); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPathFields(t *testing.T) {
	e := newEnv(t)
	f := filepath.Join(e.Root, "d", "f")
	e.write(f, 4)
	out := e.bash(fmt.Sprintf(`mc_item; mc_field id x; mc_field name n; mc_field category Developer
mc_field grade regenerates; mc_field default delete; mc_field explain e
mc_path_fields 0 %s; mc_end`, f))
	items, err := protocol.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	it := items[0]
	if len(it.Paths) != 1 || it.Paths[0] != f || it.Size < 4096 || it.Admin {
		t.Errorf("unexpected item %+v", it)
	}
	if it.Commands[protocol.Delete] != "rm -rf -- '"+f+"'" || it.Commands[protocol.Trash] != "/usr/bin/trash '"+f+"'" {
		t.Errorf("unexpected commands %+v", it.Commands)
	}
}

func TestTrashCmdDestinationsDiffer(t *testing.T) {
	e := newEnv(t)
	got := e.bash(`mc_trash_cmd 1 /Library/LaunchAgents/com.x.plist /Library/LaunchDaemons/com.x.plist`)
	dests := regexp.MustCompile(`\.Trash/com\.x\.plist \d{8}-\d{6} \d+`).FindAllString(got, -1)
	// Each move names its destination twice: once in the guard, once in mv.
	if len(dests) != 4 || dests[0] != dests[1] || dests[2] != dests[3] || dests[0] == dests[2] {
		t.Errorf("destinations should differ per source path: %q", got)
	}
	if strings.Count(got, "[ ! -e ") != 2 {
		t.Errorf("every move needs an existence guard: %q", got)
	}
}

func TestDeleteCommandEndToEnd(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.Home, "d")
	target := filepath.Join(dir, `it's a\n b$HOME\\x`)
	sibling := filepath.Join(dir, `it's a\n b$HOME`)
	e.write(target, 1)
	e.write(sibling, 1)
	t.Setenv("T", target)
	out := e.bash(`mc_item; mc_field id x; mc_field name n; mc_field category Developer
mc_field grade regenerates; mc_field default delete; mc_field explain e
mc_path_fields 0 "$T"; mc_end`)
	items, err := protocol.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	if items[0].Paths[0] != target {
		t.Fatalf("path = %q, want %q", items[0].Paths[0], target)
	}
	e.output(e.command("-c", items[0].Commands[protocol.Delete]))
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("target still exists (err %v)", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Errorf("sibling was removed: %v", err)
	}
}
