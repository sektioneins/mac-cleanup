# mac-cleanup

A terminal UI that finds reclaimable disk space on a Mac and the things that start by themselves and use its memory. It explains each item and removes only what you select. Before anything runs, it shows the exact shell command, and that string is what runs: no hidden steps, nothing rewritten in between.

```
 mac-cleanup                                     free: 41.2 GB of 994 GB
 [Space]   Autostart   scanning 18/19 ██████████████████░░  app-leftovers
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

The main screen has two tabs, Space and Autostart. Each target is a small bash script in `scripts/targets/`. A tab runs its targets at the same time, and while they run, the line with the tabs shows how many have finished and the names of those still running, as many as fit.

### Space

The Space tab is about disk:

| Category | Targets |
|---|---|
| Developer | Xcode DerivedData, Homebrew cleanup and kept cask installers, an old Intel Homebrew in `/usr/local`, Gradle caches, Android emulator images, Java runtimes, old TeX Live years, VS Code workspace storage |
| Browsers | Chrome caches, Chrome updater download cache |
| App data | Microsoft Teams caches, ON1 browse cache |
| Library | each folder in `~/Library/Caches` (in a separate sub-list), logs older than 30 days |
| Leftovers | Logic Pro and MainStage content with neither app installed; data of deleted apps in `~/Library` (by bundle id, quiet for 90 days) and plain-name `~/Library` folders that no installed app matches and nothing has used for a year, both in a separate sub-list |
| Unused apps | apps not opened for over a year, or never opened as far as macOS knows |

Items under 1 MB are not listed. If something a target looks for isn't there, the target reports nothing.

### Autostart

The Autostart tab is about memory. It lists what starts by itself on this Mac and is not part of macOS: nothing from Apple is shown, and only your own account and the system-wide entries are looked at. The 1 MB minimum does not apply, so every entry is listed. Each category is sorted by memory, largest first, with entries that aren't running at the end. The tab is scanned the first time you open it, not at startup, because reading the background task records asks for your admin password. After a rescan it's scanned again when you next open it.

| Category | What is listed |
|---|---|
| Login items | apps that open at login, and the background items and agents that an app registers for itself |
| Agents and daemons | launch agents from `~/Library/LaunchAgents` and `/Library/LaunchAgents`, launch daemons from `/Library/LaunchDaemons` |
| Kernel extensions | third-party kernel extensions that are loaded now |
| System extensions | third-party system extensions that are active and enabled |

Each row shows:

- the memory in use: for login items, agents, daemons and system extensions the process and everything it started, so helper processes are included (within a category a process is counted once, even if two entries would claim it; a helper inside an app bundle that also runs as a launch agent can be counted under both Login items and Agents and daemons); for kernel extensions the wired size that `kmutil` reports
- whether it is running (loaded, for kernel extensions) or stopped
- whether it is on or off, as far as launchd and the Background Task Management database say
- the app it belongs to, or "owner unknown" when that can't be worked out

The header shows the memory of everything that is running. An entry is marked as an orphan when its program no longer exists ("program missing"), or when the app it belongs to has been identified and is no longer installed ("app deleted"). "Program missing" does not depend on knowing the owner, but "app deleted" does: an owner that can't be worked out is never guessed to be a deleted app.

Autostart items never have Delete. What you can do depends on the kind:

| Kind | Stop | Disable | Trash |
|---|---|---|---|
| Launch agent or daemon | unloads the job until the next login or boot | unloads it and tells launchd not to start it again | unloads it, then moves its plist to the Trash |
| Agent or login item registered by an app | unloads the job | unloads it and disables it | none |
| App that opens at login | quits the app, only while it runs and its bundle id is known | removes it from the login items | none |
| Kernel extension | unloads it until the next boot | none | none |
| System extension | none | none | none |

The default action depends on the kind. A launch agent or daemon defaults to Disable if it has it, otherwise Stop, otherwise Trash, and to Trash when its program is missing, because its plist points at nothing. An agent or item registered by an app defaults to Disable, or to Stop when only that is left, and has no default when it is neither loaded nor on. An app that opens at login defaults to Disable, except that a running app that is switched off in System Settings defaults to Stop. Kernel extensions default to Stop. A job that isn't loaded has no Stop, and Disable is missing only when a job is neither loaded nor on. macOS lets only the app that installed a system extension remove it, so those rows say "no action" and can't be selected. Kernel extensions are listed only while they are loaded, and macOS on Apple Silicon may refuse to unload one.

System Settings can keep showing an item as allowed after you disabled it here. A "running" entry is not a warning: stopping running things is the point of the tab. If one of the sources can't be read (`ps`, the Background Task Management database, `launchctl`, `kmutil` or `systemextensionsctl`), that target fails and the error list on the main screen says so, rather than listing everything as unowned.

## How it decides

Every item has a grade:

- **● regenerates**: caches and build output that tools recreate by themselves. Removing them costs a rebuild or a download.
- **◐ re-download**: things you can get back, but only by downloading or reinstalling them yourself.
- **○ may need**: things that don't come back on their own, such as an old Java version or an app you haven't opened for a year.

This section and the grades are about the Space tab; the Autostart section above describes its defaults. Well-known items that regenerate default to Delete. Anything obscure or uncertain defaults to Trash, so you can still restore it. You can switch either way in the review. The pane under the list explains what each item is, whether it comes back and what removing it costs.

Some safety rules hold throughout:

- Nothing is selected when the list opens, and there is no select-all key.
- If an item belongs to an app that is still running, or the check for that failed, the run stays blocked until you quit the app and re-check, or skip the item. There is no "run anyway".
- Commands that need admin rights contain `sudo` in the text you review, and you're asked for the password when the run starts.
- Trash uses `/usr/bin/trash`. With admin rights it moves the item into `~/.Trash` and refuses to overwrite anything already there.
- `brew cleanup` runs with `HOMEBREW_NO_AUTOREMOVE=1`, so it never removes formulae you didn't ask about.
- Scans only read. They never change anything, and the Android scan works from disk without network access.
- No config, cache or state is kept between runs. Each run writes a log (every command, its output and the result) to `~/Library/Logs/mac-cleanup/`, which the app never reads back.

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

This scans everything, including the Autostart tab (so it asks for the admin password), and prints one tab-separated line per item: category, name, size, default action and its command. For Autostart items the size column shows the memory, as `mem` and the amount. It changes nothing.

## Keys

| Screen | Keys |
|---|---|
| List | `↑`/`↓` move, `space` select, `tab` switch between Space and Autostart (or `1` for Space, `2` for Autostart), `o` show in Finder, `enter` open a sub-list or go to the review, `q` quit |
| Sub-list | `↑`/`↓` move, `space` select, `o` show in Finder, `esc` back |
| Review | `↑`/`↓` move, `←`/`→` switch between the item's actions (delete and trash; disable, stop and trash on the Autostart tab), `d`/`t` pick delete or trash, `s` skip an item, `r` re-check warnings, `tab` or `enter` go to the buttons, `esc` back |
| Review buttons | `←`/`→` choose run or back, `enter` press, `tab` back to the items |
| Running | `ctrl+c`, `q` or `esc` stop after the current item; a second `ctrl+c` interrupts it, unless it runs as admin |
| Done | `↑`/`↓`, `pgup`/`pgdn` scroll through the output and summary, `r` back to the list and scan again, `q` quit |

Each tab keeps its own cursor, and what you select on both goes into one review. Running anything always takes two presses of `enter` in the review: the first moves to the run button, the second runs. While it runs, each command and its output appear as they happen.

When it's done, mac-cleanup shows how much free space you gained (not when only Autostart entries ran, since stopping or disabling them frees no disk space). It also lists anything that failed, with its output, and anything that didn't run. Space taken by trashed items is only freed once you empty the Trash.

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

An item for the Autostart tab sets `tab` to `autostart`. It then needs `id`, `name`, `category`, `explain`, `memory` and `state`, and it does not use `size` or `grade`. The extra keys exist only with that tab, and a record that uses one without it is rejected:

| Key | Value |
|---|---|
| `tab` | `autostart`; items without it belong to the Space tab |
| `memory` | resident memory in bytes, kept apart from `size` so disk and memory are never added together |
| `state` | `running`, `stopped` or `loaded` |
| `enabled` | `on` or `off`; leave it out when the script can't tell |
| `owner` | the app it belongs to; leave it out when unknown |
| `orphan` | why it points at nothing, such as `program missing` or `app deleted` |
| `cmd_stop`, `cmd_disable` | the commands for the Stop and Disable actions, next to `cmd_trash` |

Autostart items never have `cmd_delete`. An item may have no command at all, as the system extensions do, and then it has no `default` either; otherwise `default` is `stop`, `disable` or `trash`, whichever of them has a command. The four autostart targets share `scripts/autostart.sh`, which reads the process table, `launchctl` and the Background Task Management database. Together with `scripts/installed.sh` it works out who owns an item. If a source can't be read, the target fails with a message on stderr and the UI lists the error.

Look at an existing target, such as `scripts/targets/gradle-caches.sh`, and at its tests in `scripts/targets_*_test.go`. The tests run each script against a fake home directory with stub tools.

## Development

```sh
just test        # go test ./..., including the target script tests
just race        # tests with the race detector
just lint        # gofmt, go vet, shellcheck
just check       # all of the above, then build
```

The UI is written in Go with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss). Colours come from your terminal's own palette, and setting `NO_COLOR` turns them off. Each tab has its own colour, and the current one is shown in reverse; without colour it is in brackets, as in the example at the top.

## Licence

Copyright 2026 SektionEins GmbH (Ben Fuhrmannek). Licensed under the [Apache License, Version 2.0](LICENSE).
