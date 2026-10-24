// Package protocol parses the records that target scripts print to stdout.
package protocol

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Grade string

const (
	Regenerates Grade = "regenerates"
	Redownload  Grade = "redownload"
	MayNeed     Grade = "mayneed"
)

type Action string

const (
	Delete  Action = "delete"
	Trash   Action = "trash"
	Stop    Action = "stop"
	Disable Action = "disable"
)

// TabAutostart is the only value of the tab key. Items without a tab belong
// to the cleanup list.
const TabAutostart = "autostart"

// Item is one reclaimable thing reported by a target script.
type Item struct {
	Script                      string // set by scan.Run, not by Parse
	ID, Name, Category, Explain string
	View                        string // separate view name; empty for the main list
	Paths                       []string
	Size                        int64
	Grade                       Grade
	Default                     Action
	Admin                       bool
	Warnings                    []string
	LastUsed                    int64 // 0 when absent
	Commands                    map[Action]string

	// Autostart items only (Tab == TabAutostart).
	Tab     string
	Memory  int64  // resident memory in bytes
	State   string // running, stopped or loaded
	Enabled string // on, off, or empty when the script cannot tell
	Owner   string // app that owns the item, empty when unknown
	Orphan  string // why the item points at nothing, empty when it does not
}

// Key identifies an item across scripts.
func (it Item) Key() string { return it.Script + "/" + it.ID }

// Autostart reports whether the item belongs on the Autostart tab.
func (it Item) Autostart() bool { return it.Tab == TabAutostart }

// Actions lists the actions that have a command, in the order Delete,
// Disable, Stop, Trash.
func (it Item) Actions() []Action {
	var out []Action
	for _, a := range []Action{Delete, Disable, Stop, Trash} {
		if _, ok := it.Commands[a]; ok {
			out = append(out, a)
		}
	}
	return out
}

var unescaper = strings.NewReplacer(`\\`, `\`, `\n`, "\n", `\t`, "\t", `\r`, "\r")

// Parse reads records from r. Errors name the offending line number.
func Parse(r io.Reader) ([]Item, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var items []Item
	var cur *Item
	var seen map[string]bool
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if line == "" && cur == nil {
			continue
		}
		if cur == nil {
			if line != "item" {
				return nil, fmt.Errorf("line %d: field outside item: %q", n, line)
			}
			cur = &Item{Commands: map[Action]string{}}
			seen = map[string]bool{}
			continue
		}
		if line == "end" {
			if err := finish(cur, seen); err != nil {
				return nil, fmt.Errorf("line %d: %w", n, err)
			}
			items = append(items, *cur)
			cur = nil
			continue
		}
		key, raw, _ := strings.Cut(line, "\t")
		if err := setField(cur, seen, key, unescaper.Replace(raw)); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("line %d: %w", n+1, err)
	}
	if cur != nil {
		return nil, fmt.Errorf("line %d: missing final end", n)
	}
	return items, nil
}

func setField(it *Item, seen map[string]bool, key, val string) error {
	repeatable := key == "path" || key == "warn"
	if seen[key] && !repeatable {
		return fmt.Errorf("duplicate key %q", key)
	}
	seen[key] = true
	switch key {
	case "id":
		it.ID = val
	case "name":
		it.Name = val
	case "category":
		it.Category = val
	case "explain":
		it.Explain = val
	case "view":
		it.View = val
	case "path":
		it.Paths = append(it.Paths, val)
	case "warn":
		it.Warnings = append(it.Warnings, val)
	case "size":
		v, err := strconv.ParseInt(val, 10, 64)
		if err != nil || v < 0 {
			return fmt.Errorf("size %q is not a non-negative integer", val)
		}
		it.Size = v
	case "lastused":
		v, err := strconv.ParseInt(val, 10, 64)
		if err != nil || v < 0 {
			return fmt.Errorf("lastused %q is not a non-negative integer", val)
		}
		it.LastUsed = v
	case "grade":
		switch g := Grade(val); g {
		case Regenerates, Redownload, MayNeed:
			it.Grade = g
		default:
			return fmt.Errorf("unknown grade %q", val)
		}
	case "default":
		switch a := Action(val); a {
		case Delete, Trash, Stop, Disable:
			it.Default = a
		default:
			return fmt.Errorf("unknown default action %q", val)
		}
	case "admin":
		switch val {
		case "1":
			it.Admin = true
		case "0":
			it.Admin = false
		default:
			return fmt.Errorf("admin %q must be 0 or 1", val)
		}
	case "tab":
		if val != TabAutostart {
			return fmt.Errorf("unknown tab %q", val)
		}
		it.Tab = val
	case "memory":
		v, err := strconv.ParseInt(val, 10, 64)
		if err != nil || v < 0 {
			return fmt.Errorf("memory %q is not a non-negative integer", val)
		}
		it.Memory = v
	case "state":
		switch val {
		case "running", "stopped", "loaded":
			it.State = val
		default:
			return fmt.Errorf("unknown state %q", val)
		}
	case "enabled":
		switch val {
		case "on", "off":
			it.Enabled = val
		default:
			return fmt.Errorf("enabled %q must be on or off", val)
		}
	case "owner":
		it.Owner = val
	case "orphan":
		it.Orphan = val
	case "cmd_delete":
		it.Commands[Delete] = val
	case "cmd_trash":
		it.Commands[Trash] = val
	case "cmd_stop":
		it.Commands[Stop] = val
	case "cmd_disable":
		it.Commands[Disable] = val
	default:
		return fmt.Errorf("unknown key %q", key)
	}
	return nil
}

// autostartOnly are the keys that need tab autostart.
var autostartOnly = []string{"memory", "state", "enabled", "owner", "orphan", "cmd_stop", "cmd_disable"}

// finish runs the checks that depend on more than one key, because keys
// come in any order.
func finish(it *Item, seen map[string]bool) error {
	if !it.Autostart() {
		for _, k := range autostartOnly {
			if seen[k] {
				return fmt.Errorf("key %q needs tab autostart", k)
			}
		}
		return finishListItem(it, seen)
	}
	for _, k := range []string{"id", "name", "category", "explain", "memory", "state"} {
		if !seen[k] {
			return fmt.Errorf("missing required key %q", k)
		}
	}
	if seen["cmd_delete"] {
		return fmt.Errorf("key %q is not allowed with tab autostart", "cmd_delete")
	}
	if len(it.Commands) == 0 {
		if seen["default"] {
			return fmt.Errorf("default %q but the item has no commands", it.Default)
		}
		return nil
	}
	if !seen["default"] {
		return fmt.Errorf("missing required key %q", "default")
	}
	return checkDefault(it)
}

func finishListItem(it *Item, seen map[string]bool) error {
	for _, k := range []string{"id", "name", "category", "size", "grade", "default", "explain"} {
		if !seen[k] {
			return fmt.Errorf("missing required key %q", k)
		}
	}
	return checkDefault(it)
}

func checkDefault(it *Item) error {
	if _, ok := it.Commands[it.Default]; !ok {
		return fmt.Errorf("default %q has no cmd_%s", it.Default, it.Default)
	}
	return nil
}
