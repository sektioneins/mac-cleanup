#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

command -v brew >/dev/null 2>&1 || exit 0
prefix=$(brew --prefix 2>/dev/null) || exit 0
[ -n "$prefix" ] || exit 0

for file in "$prefix"/Caskroom/*/*/*.pkg "$prefix"/Caskroom/*/*/*.dmg; do
	[ -f "$file" ] || continue
	size=$(mc_size "$file")
	[ "$size" -gt 0 ] || continue
	version=$(basename "$(dirname "$file")")
	cask=$(basename "$(dirname "$(dirname "$file")")")

	mc_item
	mc_field id "$cask/$version/$(basename "$file")"
	mc_field name "$cask $version installer"
	mc_field category Developer
	mc_path_fields "$(mc_needs_admin "$file")" "$file"
	mc_field grade redownload
	mc_field default delete
	mc_field explain "The installer file that Homebrew kept after installing $cask. The installed app is not affected, and Homebrew downloads the installer again if you reinstall it."
	mc_end
done
