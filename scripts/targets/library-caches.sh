#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

for dir in "$HOME"/Library/Caches/*; do
	[ -d "$dir" ] || continue
	name=$(basename "$dir")
	[ "$name" = Homebrew ] && continue
	size=$(mc_size "$dir")
	[ "$size" -gt 0 ] || continue

	mc_item
	mc_field id "$name"
	mc_field name "$name"
	mc_field category Library
	mc_path_fields "$(mc_needs_admin "$dir")" "$dir"
	mc_field grade regenerates
	mc_field default delete
	# shellcheck disable=SC2088 # display name, not a path
	mc_field view "~/Library/Caches"
	mc_field explain "The cache folder $name in ~/Library/Caches. The folder is removed and the app that owns it rebuilds it on demand, so this costs some speed at the next start."
	mc_end
done
