#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

dir="$MC_ROOT/Library/Application Support/Logic"
[ -d "$dir" ] || exit 0
# Logic Pro and MainStage both use this folder.
for app in "$MC_ROOT"/Applications/Logic\ Pro*.app "$MC_ROOT"/Applications/MainStage*.app; do
	[ -e "$app" ] && exit 0
done
size=$(mc_size "$dir")
[ "$size" -gt 0 ] || exit 0

mc_item
mc_field id logic
mc_field name "Logic Pro leftovers"
mc_field category Leftovers
mc_path_fields "$(mc_needs_admin "$dir")" "$dir"
mc_field grade mayneed
mc_field default trash
mc_field explain "Logic Pro and MainStage instrument, loop and plug-in content, left behind although neither app is installed. It does not come back by itself, and GarageBand may also use parts of it."
mc_end
