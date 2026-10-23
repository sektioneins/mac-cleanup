#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

command -v brew >/dev/null 2>&1 || exit 0

line=$(brew cleanup --prune=all --dry-run 2>/dev/null | grep 'free approximately [0-9.]*[KMG]*B of disk space' | tail -n 1)
[ -n "$line" ] || exit 0
size=$(printf '%s\n' "$line" | sed -E 's/.*free approximately ([0-9.]+)([KMG]?B) of disk space.*/\1 \2/' | awk '
	{ m = 1; if ($2 == "KB") m = 1e3; else if ($2 == "MB") m = 1e6; else if ($2 == "GB") m = 1e9
	  printf "%.0f\n", $1 * m }')
case $size in
'' | *[!0-9]*) exit 0 ;;
esac
[ "$size" -gt 0 ] || exit 0

mc_item
mc_field id cleanup
mc_field name "Homebrew cleanup"
mc_field category Developer
mc_field size "$size"
mc_field grade redownload
mc_field default delete
mc_field cmd_delete "brew cleanup --prune=all"
mc_field explain "Old versions of Homebrew packages and its download cache. Homebrew downloads whatever it needs again, so removing them only costs download time."
mc_end
