#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"
# shellcheck source=../autostart.sh
. "$(dirname "$0")/../autostart.sh"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/mc-sysext.XXXXXX") || exit 1
trap 'rm -rf "$tmp"' EXIT

if ! listing=$(systemextensionsctl list 2>"$tmp/sysext.err"); then
	echo "mac-cleanup: could not list the system extensions (systemextensionsctl failed): $(head -n 3 "$tmp/sysext.err")" >&2
	exit 1
fi

# The rows are tab-separated: enabled, active, team, "bundle id (version)",
# name, [state]. The count and category lines and the header row have another
# shape. Only active extensions count, and Apple's are left out.
printf '%s\n' "$listing" | tr -d '\r' | awk -F'\t' -v US="$_MC_US" '
	NF >= 6 && $6 == "[activated enabled]" {
		id = $4
		sub(/ \([^()]*\)$/, "", id)
		if (id == "" || tolower(id) ~ /^com\.apple\./ || id in seen) next
		seen[id] = 1
		print id US $5 US $3
	}' >"$tmp/exts"
[ -s "$tmp/exts" ] || exit 0

mc_ps_snapshot "$tmp/ps" || exit 1

while IFS=$_MC_US read -r id name team; do
	[ -n "$name" ] || name=$id
	pids=$(mc_pids_matching "$tmp/ps" "/$id.systemextension/")
	memory=0
	state=stopped
	if [ -n "$pids" ]; then
		state=running
		# Memory tie-break: a process is counted for the first extension
		# that claims it, in the order systemextensionsctl lists them.
		# The pids are digits only.
		# shellcheck disable=SC2086
		memory=$(mc_tree_rss "$tmp/ps" "$tmp/claimed" $pids)
	fi
	if [ -n "$team" ]; then
		who="the team $team"
	else
		who="an unknown team"
	fi

	mc_item
	mc_field id "sysext/$id"
	mc_field name "$name"
	mc_field category "System extensions"
	mc_field tab autostart
	mc_field memory "${memory:-0}"
	mc_field state "$state"
	mc_field explain "System extension of $who, active now. Only the app that installed it can remove it, so remove it or turn it off in that app."
	mc_end
done <"$tmp/exts"
exit 0
