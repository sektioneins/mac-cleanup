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
	Delete Action = "delete"
	Trash  Action = "trash"
)

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
}

// Key identifies an item across scripts.
func (it Item) Key() string { return it.Script + "/" + it.ID }

// Actions lists the actions that have a command, Delete first.
func (it Item) Actions() []Action {
	var out []Action
	for _, a := range []Action{Delete, Trash} {
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
		case Delete, Trash:
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
	case "cmd_delete":
		it.Commands[Delete] = val
	case "cmd_trash":
		it.Commands[Trash] = val
	default:
		return fmt.Errorf("unknown key %q", key)
	}
	return nil
}

func finish(it *Item, seen map[string]bool) error {
	for _, k := range []string{"id", "name", "category", "size", "grade", "default", "explain"} {
		if !seen[k] {
			return fmt.Errorf("missing required key %q", k)
		}
	}
	if _, ok := it.Commands[it.Default]; !ok {
		return fmt.Errorf("default %q has no cmd_%s", it.Default, it.Default)
	}
	return nil
}
