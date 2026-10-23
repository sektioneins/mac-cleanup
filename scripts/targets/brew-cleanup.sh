#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

command -v brew >/dev/null 2>&1 || exit 0

# One dry run, captured in one stream: stdout as is, stderr lines prefixed
# with "mc-err:", and brew's exit status last as "mc-rc:N".
# HOMEBREW_NO_AUTOREMOVE keeps brew from also removing formulae that are no
# longer needed as dependencies.
out=$( { {
	HOMEBREW_NO_AUTOREMOVE=1 brew cleanup --prune=all --dry-run
	printf '\nmc-rc:%s\n' "$?"
} 2>&1 1>&3 3>&- | sed 's/^/mc-err:/'; } 3>&1)
rc=$(printf '%s\n' "$out" | sed -n 's/^mc-rc://p' | tail -n 1)
err=$(printf '%s\n' "$out" | sed -n 's/^mc-err://p' | head -n 1)

line=$(printf '%s\n' "$out" | grep -v '^mc-' | grep 'free approximately [0-9.]*[KMG]*B of disk space' | tail -n 1)
size=0
if [ -n "$line" ]; then
	size=$(printf '%s\n' "$line" | sed -E 's/.*free approximately ([0-9.]+)([KMG]?B) of disk space.*/\1 \2/' | awk '
		{ m = 1; if ($2 == "KB") m = 1e3; else if ($2 == "MB") m = 1e6; else if ($2 == "GB") m = 1e9
		  printf "%.0f\n", $1 * m }')
	case $size in
	'' | *[!0-9]*) size=0 ;;
	esac
fi

failure=
if [ "$rc" != 0 ]; then
	if [ -n "$err" ]; then
		failure="brew cleanup --dry-run failed: $err"
	else
		failure="brew cleanup --dry-run failed (exit status ${rc:-unknown})"
	fi
	if [ "$size" -eq 0 ]; then
		echo "$failure" >&2
		exit 1
	fi
fi
[ "$size" -gt 0 ] || exit 0

mc_item
mc_field id cleanup
mc_field name "Homebrew cleanup"
mc_field category Developer
mc_field size "$size"
mc_field grade redownload
mc_field default delete
mc_field cmd_delete "HOMEBREW_NO_AUTOREMOVE=1 brew cleanup --prune=all"
[ -n "$failure" ] && mc_field warn "$failure"
mc_field explain "Old versions of Homebrew packages and its download cache. Homebrew downloads whatever it needs again, so removing them only costs download time."
mc_end
