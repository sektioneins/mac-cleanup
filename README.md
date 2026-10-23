# mac-cleanup

A terminal UI that finds reclaimable disk space on a Mac, explains each item and removes only what you select. Before anything runs, it shows the exact shell command, and that string is what runs: no hidden steps, nothing rewritten in between.

```
 mac-cleanup                                     free: 41.2 GB of 994 GB
 [Space]
──────────────────────────────────────────────────────────────────────────
▾ Developer                                                      18.4 GB
> [ ] Xcode DerivedData                      9.1 GB  ● regenerates
  [x] Homebrew cleanup                       3.2 GB  ● regenerates
  [ ] Android image 34 google_apis arm64     2.8 GB  ◐ re-download
  [ ] Java 11 (Temurin)                      312 MB  ○ may need
▾ Library                                                         6.0 GB
      ~/Library/Caches  (41 folders)  5.7 GB  enter to open
```

## What it looks for

Each cleanup target is a small bash script in `scripts/targets/`:

| Category | Targets |
|---|---|
| Developer | Xcode DerivedData, Homebrew cleanup and kept cask installers, an old Intel Homebrew in `/usr/local`, Gradle caches, Android emulator images, Java runtimes, old TeX Live years, VS Code workspace storage |
| Browsers | Chrome caches, Chrome updater download cache |
| App data | Microsoft Teams caches, ON1 browse cache |
| Library | each folder in `~/Library/Caches` (in a separate sub-list), logs older than 30 days |
| Leftovers | Logic Pro and MainStage content with neither app installed; data of deleted apps in `~/Library` (by bundle id, quiet for 90 days) and plain-name `~/Library` folders that no installed app matches and nothing has used for a year, both in a separate sub-list |
| Unused apps | apps not opened for over a year, or never opened as far as macOS knows |

Items under 1 MB are not listed. If something a target looks for isn't there, the target reports nothing.

## How it decides

Every item has a grade:

- **● regenerates**: caches and build output that tools recreate by themselves. Removing them costs a rebuild or a download.
- **◐ re-download**: things you can get back, but only by downloading or reinstalling them yourself.
- **○ may need**: things that don't come back on their own, such as an old Java version or an app you haven't opened for a year.

Well-known items that regenerate default to Delete. Anything obscure or uncertain defaults to Trash, so you can still restore it. You can switch either way in the review. The pane under the list explains what each item is, whether it comes back and what removing it costs.

Some safety rules hold throughout:

- Nothing is selected when the list opens, and there is no select-all key.
- If an item belongs to an app that is still running, or the check for that failed, the run stays blocked until you quit the app and re-check, or skip the item. There is no "run anyway".
- Commands that need admin rights contain `sudo` in the text you review, and you're asked for the password when the run starts.
- Trash uses `/usr/bin/trash`. With admin rights it moves the item into `~/.Trash` and refuses to overwrite anything already there.
- `brew cleanup` runs with `HOMEBREW_NO_AUTOREMOVE=1`, so it never removes formulae you didn't ask about.
- Scans only read. They never change anything, and the Android scan works from disk without network access.
- No config, cache or state is kept between runs.

## Requirements

- macOS. The Trash action uses `/usr/bin/trash`, which macOS has included since version 15.
- Go 1.27 or later to build.
- Optional: [just](https://github.com/casey/just) and [shellcheck](https://www.shellcheck.net) for development.

## Build and run

```sh
git clone <repository-url> mac-cleanup
cd mac-cleanup
go build -o mac-cleanup .     # or: just build
./mac-cleanup
```

The binary finds its target scripts in `scripts/` next to the executable, even through a symlink, and otherwise in `./scripts`. Use `-scripts DIR` to point it somewhere else.

To see what would be offered without opening the UI, run:

```sh
./mac-cleanup -list
```

This scans everything and prints one tab-separated line per item: category, name, size, default action and its command. It changes nothing.

## Keys

| Screen | Keys |
|---|---|
| List | `↑`/`↓` move, `space` select, `o` show in Finder, `enter` open a sub-list or go to the review, `q` quit |
| Sub-list | `↑`/`↓` move, `space` select, `o` show in Finder, `esc` back |
| Review | `↑`/`↓` move, `←`/`→` or `d`/`t` switch between delete and trash, `s` skip an item, `r` re-check warnings, `tab` or `enter` go to the buttons, `esc` back |
| Review buttons | `←`/`→` choose run or back, `enter` press, `tab` back to the items |
| Running | `ctrl+c`, `q` or `esc` stop after the current item; a second `ctrl+c` interrupts it, unless it runs as admin |
| Done | `↑`/`↓`, `pgup`/`pgdn` scroll, `q` quit |

Running anything always takes two presses of `enter` in the review: the first moves to the run button, the second runs.

When it's done, mac-cleanup shows how much free space you gained. It also lists anything that failed, with its output, and anything that didn't run. Space taken by trashed items is only freed once you empty the Trash.

## Adding a target

A target is a bash 3.2 script in `scripts/targets/` that prints item records to stdout and never changes anything itself. The delete and trash commands exist only as text in its output. `scripts/lib.sh` has helpers for quoting paths, sizes, admin detection and building the commands. A new target needs no recompiling.

Each item is a block of `key<TAB>value` lines between `item` and `end`:

```
item
id	caches
name	Gradle caches
category	Developer
path	/Users/me/.gradle/caches
size	2147483648
admin	0
cmd_delete	rm -rf -- '/Users/me/.gradle/caches'
cmd_trash	/usr/bin/trash '/Users/me/.gradle/caches'
grade	regenerates
default	delete
explain	Downloaded build dependencies. Gradle fetches them again on the next build, so removing them costs download time and a slower first build.
end
```

A `warn` line, for example when Gradle is still running, blocks the item until it is skipped or re-checked clean.

Look at an existing target, such as `scripts/targets/gradle-caches.sh`, and at its tests in `scripts/targets_*_test.go`. The tests run each script against a fake home directory with stub tools.

## Development

```sh
just test        # go test ./..., including the target script tests
just race        # tests with the race detector
just lint        # gofmt, go vet, shellcheck
just check       # all of the above, then build
```

The UI is written in Go with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss). Colours come from your terminal's own palette, and setting `NO_COLOR` turns them off.

## Licence

Copyright 2026 Ben Fuhrmannek. Licensed under the [Apache License, Version 2.0](LICENSE).
