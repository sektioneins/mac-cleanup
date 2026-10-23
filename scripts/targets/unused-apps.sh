#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

now=$(date +%s)
limit=$((now - 31536000))

# mdls_value ATTR APP: the attribute value, or nothing when unknown.
mdls_value() {
	local v
	v=$(mdls -raw -name "$1" "$2" 2>/dev/null) || return 0
	[ "$v" = "(null)" ] || printf '%s' "$v"
}

for app in "$MC_ROOT"/Applications/*.app; do
	[ -d "$app" ] || continue
	bundle=$(mdls_value kMDItemCFBundleIdentifier "$app")
	case $bundle in com.apple.*) continue ;; esac

	used=""
	date_text=$(mdls_value kMDItemLastUsedDate "$app")
	if [ -n "$date_text" ]; then
		used=$(date -j -f '%Y-%m-%d %H:%M:%S %z' "$date_text" +%s 2>/dev/null) || used=""
	fi
	[ -n "$used" ] || used=$(stat -f %m "$app" 2>/dev/null) || continue
	[ "$used" -lt "$limit" ] || continue

	size=$(mc_size "$app")
	[ "$size" -gt 0 ] || continue
	file=$(basename "$app")
	name=${file%.app}

	mc_item
	mc_field id "$file"
	mc_field name "$name (last opened $(date -j -r "$used" '+%b %Y'))"
	mc_field category "Unused apps"
	mc_path_fields "$(mc_needs_admin "$app")" "$app"
	mc_field grade mayneed
	mc_field default trash
	mc_field lastused "$used"
	mc_field explain "The application $file, not opened for over a year. It does not come back by itself; you would have to install it again, and its settings stay in your Library."
	mc_end
done
