// Command mac-cleanup is a TUI that finds disk space to clean up.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"mac-cleanup/internal/scan"
	"mac-cleanup/internal/ui"
)

// scriptsDir picks the scripts directory: the flag value if set, else
// <exe dir>/scripts if it is a directory, else <cwd>/scripts.
func scriptsDir(flagValue, exe, cwd string) string {
	if flagValue != "" {
		return flagValue
	}
	if exe != "" {
		d := filepath.Join(filepath.Dir(exe), "scripts")
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d
		}
	}
	return filepath.Join(cwd, "scripts")
}

// clean makes a field safe for tab separated output: control characters are
// escaped as on screen (ui.Visible), tab and newline as \t and \n.
func clean(s string) string {
	return strings.NewReplacer("\t", `\t`, "\n", `\n`).Replace(ui.Visible(s))
}

// printList writes one line per item and one per failed script, in order.
func printList(w io.Writer, results []scan.Result) {
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(w, "FAILED\t%s\t%s\n", clean(r.Script), clean(r.Err.Error()))
			continue
		}
		for _, it := range r.Items {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", clean(it.Category), clean(it.Name),
				ui.HumanSize(it.Size), it.Default, clean(it.Commands[it.Default]))
		}
	}
}

// listMode scans every script (in parallel) and prints the items. It never
// runs an item command.
func listMode(w io.Writer, scripts []string) {
	results := make([]scan.Result, len(scripts))
	var wg sync.WaitGroup
	for i, s := range scripts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = scan.Run(context.Background(), s)
		}()
	}
	wg.Wait()
	printList(w, results)
}

// resolveExe follows symlinks to the real executable, so scripts next to it
// are found when it is started through a link; on error exe is returned.
func resolveExe(exe string) string {
	if exe == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		return real
	}
	return exe
}

// run is the program without os.Exit; it returns the exit status.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mac-cleanup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("scripts", "", "scripts directory (default: next to the executable, else ./scripts)")
	list := fs.Bool("list", false, "scan, print the items and exit; changes nothing")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	exe, _ := os.Executable()
	cwd, _ := os.Getwd()
	sdir := scriptsDir(*dir, resolveExe(exe), cwd)
	scripts, err := scan.Discover(sdir)
	if err != nil {
		fmt.Fprintln(stderr, "mac-cleanup:", err)
		return 1
	}
	if len(scripts) == 0 {
		fmt.Fprintln(stderr, "mac-cleanup: no target scripts in", sdir)
		return 1
	}

	if *list {
		listMode(stdout, scripts)
		return 0
	}

	free, err := os.UserHomeDir()
	if err != nil {
		free = "/"
	}
	if _, err := tea.NewProgram(ui.New(scripts, ui.DefaultDeps(free)), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(stderr, "mac-cleanup:", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
