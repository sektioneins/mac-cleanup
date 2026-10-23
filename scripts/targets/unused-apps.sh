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

	file=$(basename "$app")
	name=${file%.app}
	keep="It does not come back by itself; you would have to install it again, and its settings stay in your Library."

	used=""
	date_text=$(mdls_value kMDItemLastUsedDate "$app")
	if [ -n "$date_text" ]; then
		used=$(date -j -f '%Y-%m-%d %H:%M:%S %z' "$date_text" +%s 2>/dev/null) || used=""
	fi
	if [ -n "$used" ]; then
		label="last opened $(date -j -r "$used" '+%b %Y')"
		explain="The application $file, not opened for over a year. $keep"
	else
		# No Spotlight date: fall back to the bundle's modification time,
		# which is usually when it was installed or last updated.
		used=$(stat -f %m "$app" 2>/dev/null) || continue
		since=$(date -j -r "$used" '+%b %Y')
		label="never opened per Spotlight, unchanged since $since"
		explain="macOS has no record of this app being opened. The app has not changed since $since, which is usually when it was installed or last updated. $keep"
	fi
	[ "$used" -lt "$limit" ] || continue

	size=$(mc_size "$app")
	[ "$size" -gt 0 ] || continue

	mc_item
	mc_field id "$file"
	mc_field name "$name ($label)"
	mc_field category "Unused apps"
	mc_path_fields "$(mc_needs_admin "$app")" "$app"
	mc_field grade mayneed
	mc_field default trash
	mc_field lastused "$used"
	mc_warn_running "$app/Contents/MacOS/" "$name"
	mc_field explain "$explain"
	mc_end
done
