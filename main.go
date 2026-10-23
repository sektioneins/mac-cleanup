// Command mac-cleanup is a TUI that finds disk space to clean up.
package main

import (
	"context"
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

// clean makes a field safe for tab separated output.
func clean(s string) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s)
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

func main() {
	dir := flag.String("scripts", "", "scripts directory (default: next to the executable, else ./scripts)")
	list := flag.Bool("list", false, "scan, print the items and exit; changes nothing")
	flag.Parse()

	exe, _ := os.Executable()
	cwd, _ := os.Getwd()
	scripts, err := scan.Discover(scriptsDir(*dir, exe, cwd))
	if err != nil {
		fmt.Fprintln(os.Stderr, "mac-cleanup:", err)
		os.Exit(1)
	}

	if *list {
		listMode(os.Stdout, scripts)
		return
	}

	free, err := os.UserHomeDir()
	if err != nil {
		free = "/"
	}
	if _, err := tea.NewProgram(ui.New(scripts, ui.DefaultDeps(free)), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "mac-cleanup:", err)
		os.Exit(1)
	}
}
