package protocol

import (
	"reflect"
	"strings"
	"testing"
)

const fullRecord = "item\n" +
	"id\tnpm-cache\n" +
	"name\tnpm cache\n" +
	"category\tDeveloper\n" +
	"path\t/Users/x/.npm\n" +
	"path\t/Users/x/.npm2\n" +
	"size\t12345\n" +
	"grade\tredownload\n" +
	"default\ttrash\n" +
	"admin\t1\n" +
	"warn\tfirst\n" +
	"warn\tsecond\n" +
	"explain\tCached packages.\n" +
	"lastused\t1700000000\n" +
	"cmd_delete\trm -rf '/Users/x/.npm'\n" +
	"cmd_trash\tmv '/Users/x/.npm' ~/.Trash\n" +
	"end\n"

func TestParseFull(t *testing.T) {
	got, err := Parse(strings.NewReader(fullRecord))
	if err != nil {
		t.Fatal(err)
	}
	want := []Item{{
		ID:       "npm-cache",
		Name:     "npm cache",
		Category: "Developer",
		Explain:  "Cached packages.",
		Paths:    []string{"/Users/x/.npm", "/Users/x/.npm2"},
		Size:     12345,
		Grade:    Redownload,
		Default:  Trash,
		Admin:    true,
		Warnings: []string{"first", "second"},
		LastUsed: 1700000000,
		Commands: map[Action]string{
			Delete: "rm -rf '/Users/x/.npm'",
			Trash:  "mv '/Users/x/.npm' ~/.Trash",
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

const base = "id\ta\nname\tA\ncategory\tLibrary\nsize\t1\ngrade\tmayneed\ndefault\tdelete\nexplain\te\ncmd_delete\ttrue\n"

func TestParseUnescape(t *testing.T) {
	in := "item\n" + base + "path\t/a\\\\b\\nc\\td\nend\n"
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if want := "/a\\b\nc\td"; got[0].Paths[0] != want {
		t.Fatalf("got %q want %q", got[0].Paths[0], want)
	}
}

func TestParseCarriageReturn(t *testing.T) {
	in := "item\n" + base + "path\t/Icon\\r\npath\t/a\\\\r\nend\n"
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if p := got[0].Paths; len(p) != 2 || p[0] != "/Icon\r" || p[1] != `/a\r` {
		t.Fatalf("paths = %q", p)
	}
}

func TestParseMultiple(t *testing.T) {
	one := "item\n" + base + "end\n"
	two := strings.Replace(one, "id\ta", "id\tb", 1)
	got, err := Parse(strings.NewReader(one + "\n\n" + two))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("got %#v", got)
	}
}

const autostartBase = "tab\tautostart\nid\ta\nname\tA\ncategory\tAutostart\nexplain\te\nmemory\t100\nstate\trunning\n"

func autostartWithout(prefix string) string {
	var out []string
	for _, l := range strings.Split(strings.TrimSuffix(autostartBase, "\n"), "\n") {
		if !strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n") + "\n"
}

func TestParseErrors(t *testing.T) {
	without := func(prefix string) string {
		var out []string
		for _, l := range strings.Split(strings.TrimSuffix(base, "\n"), "\n") {
			if !strings.HasPrefix(l, prefix) {
				out = append(out, l)
			}
		}
		return strings.Join(out, "\n") + "\n"
	}
	cases := []struct{ name, in, line string }{
		{"unknown key", "item\n" + base + "colour\tred\nend\n", "line 10"},
		{"missing id", "item\n" + without("id\t") + "end\n", "line 9"},
		{"bad size", "item\n" + strings.Replace(base, "size\t1", "size\tabc", 1) + "end\n", "line 5"},
		{"bad grade", "item\n" + strings.Replace(base, "mayneed", "maybe", 1) + "end\n", "line 6"},
		{"default without command", "item\n" + strings.Replace(base, "default\tdelete", "default\ttrash", 1) + "end\n", "line 10"},
		{"field outside item", "id\ta\n", "line 1"},
		{"missing end", "item\n" + base, "line 9"},
		{"memory without tab", "item\n" + base + "memory\t5\nend\n", "needs tab autostart"},
		{"state without tab", "item\n" + base + "state\trunning\nend\n", "needs tab autostart"},
		{"cmd_stop without tab", "item\n" + base + "cmd_stop\ttrue\nend\n", "needs tab autostart"},
		{"tab other", "item\n" + strings.Replace(autostartBase, "tab\tautostart", "tab\tother", 1) + "end\n", "line 2"},
		{"autostart missing memory", "item\n" + autostartWithout("memory\t") + "end\n", "missing required key \"memory\""},
		{"autostart missing state", "item\n" + autostartWithout("state\t") + "end\n", "missing required key \"state\""},
		{"bad state", "item\n" + strings.Replace(autostartBase, "state\trunning", "state\tsleeping", 1) + "end\n", "line 8"},
		{"bad enabled", "item\n" + autostartBase + "enabled\tyes\nend\n", "line 9"},
		{"bad memory", "item\n" + strings.Replace(autostartBase, "memory\t100", "memory\t-1", 1) + "end\n", "line 7"},
		{"autostart cmd_delete", "item\n" + autostartBase + "cmd_delete\ttrue\nend\n", "cmd_delete"},
		{"autostart command without default", "item\n" + autostartBase + "cmd_stop\ttrue\nend\n", "default"},
		{"autostart default without command", "item\n" + autostartBase + "default\tstop\nend\n", "default"},
		{"default stop without cmd_stop", "item\n" + autostartBase + "default\tstop\ncmd_disable\ttrue\nend\n", "cmd_stop"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(c.in))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.line) {
				t.Fatalf("error %q does not mention %q", err, c.line)
			}
		})
	}
}

func TestActions(t *testing.T) {
	it := Item{Commands: map[Action]string{Delete: "x"}}
	if got := it.Actions(); !reflect.DeepEqual(got, []Action{Delete}) {
		t.Fatalf("got %v", got)
	}
	it.Commands[Trash] = "y"
	if got := it.Actions(); !reflect.DeepEqual(got, []Action{Delete, Trash}) {
		t.Fatalf("got %v", got)
	}
}

func TestKey(t *testing.T) {
	if got := (Item{Script: "s", ID: "i"}).Key(); got != "s/i" {
		t.Fatalf("got %q", got)
	}
}

func TestParseView(t *testing.T) {
	got, err := Parse(strings.NewReader("item\n" + base + "view\t~/Library/Caches\nend\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].View != "~/Library/Caches" {
		t.Errorf("View = %q", got[0].View)
	}
	got, err = Parse(strings.NewReader("item\n" + base + "end\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].View != "" {
		t.Errorf("View = %q, want empty", got[0].View)
	}
	_, err = Parse(strings.NewReader("item\n" + base + "view\ta\nview\tb\nend\n"))
	if err == nil || !strings.Contains(err.Error(), "line 11") || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("duplicate view error = %v", err)
	}
}

func TestParseAutostart(t *testing.T) {
	in := "item\n" + autostartBase +
		"enabled\ton\nowner\tExample App\norphan\tprogram missing\n" +
		"cmd_stop\tlaunchctl bootout x\ncmd_disable\tlaunchctl disable x\ncmd_trash\ttrash x\n" +
		"default\tdisable\nend\n"
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []Item{{
		Tab: TabAutostart, ID: "a", Name: "A", Category: "Autostart", Explain: "e",
		Memory: 100, State: "running", Enabled: "on", Owner: "Example App", Orphan: "program missing",
		Default: Disable,
		Commands: map[Action]string{
			Stop:    "launchctl bootout x",
			Disable: "launchctl disable x",
			Trash:   "trash x",
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
	if !got[0].Autostart() {
		t.Error("Autostart() = false")
	}
	if a := got[0].Actions(); !reflect.DeepEqual(a, []Action{Disable, Stop, Trash}) {
		t.Errorf("Actions() = %v", a)
	}
	if (Item{}).Autostart() {
		t.Error("zero Item reports Autostart()")
	}
}

func TestParseAutostartNoAction(t *testing.T) {
	got, err := Parse(strings.NewReader("item\n" + autostartBase + "end\n"))
	if err != nil {
		t.Fatal(err)
	}
	if a := got[0].Actions(); len(a) != 0 {
		t.Errorf("Actions() = %v, want none", a)
	}
	if got[0].Enabled != "" || got[0].Default != "" {
		t.Errorf("Enabled = %q, Default = %q, want empty", got[0].Enabled, got[0].Default)
	}
}

func TestActionsOrder(t *testing.T) {
	it := Item{Commands: map[Action]string{Trash: "a", Stop: "b", Disable: "c", Delete: "d"}}
	if got := it.Actions(); !reflect.DeepEqual(got, []Action{Delete, Disable, Stop, Trash}) {
		t.Fatalf("got %v", got)
	}
}
