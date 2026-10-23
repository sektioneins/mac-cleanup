#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

command -v brew >/dev/null 2>&1 || exit 0
# brew prints its own error message on stderr; a failure fails the scan.
if ! prefix=$(brew --prefix) || [ -z "$prefix" ]; then
	echo "brew --prefix failed" >&2
	exit 1
fi

# An Intel Homebrew in /usr/local next to /opt/homebrew is covered by brew-intel.
if [ "$prefix" = "$MC_ROOT/usr/local" ] && [ -d "$MC_ROOT/opt/homebrew" ]; then
	exit 0
fi

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
