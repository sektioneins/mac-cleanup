#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

[ -d "$MC_ROOT/opt/homebrew" ] && [ -d "$MC_ROOT/usr/local/Homebrew" ] || exit 0

paths=()
for p in "$MC_ROOT/usr/local/Homebrew" "$MC_ROOT/usr/local/Cellar" "$MC_ROOT/usr/local/Caskroom" "$MC_ROOT/usr/local/var/homebrew"; do
	[ -e "$p" ] && paths+=("$p")
done
size=$(mc_size "${paths[@]}")
[ "$size" -gt 0 ] || exit 0

mc_item
mc_field id intel
mc_field name "Old Intel Homebrew in /usr/local"
mc_field category Developer
mc_path_fields "$(mc_needs_admin "${paths[@]}")" "${paths[@]}"
mc_field grade mayneed
mc_field default trash
mc_field explain "A second Homebrew from the Intel era, next to the current one in /opt/homebrew. Anything installed only there is lost, and leftover links in /usr/local/bin then point nowhere and are harmless."
mc_end
