#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

logs="$HOME/Library/Logs"
[ -d "$logs" ] || exit 0

size=$(find "$logs" -type f -mtime +30 -exec stat -f %z {} + 2>/dev/null | awk '{ s += $1 } END { printf "%.0f\n", s }')
case $size in
'' | *[!0-9]*) exit 0 ;;
esac
[ "$size" -gt 0 ] || exit 0

mc_item
mc_field id old
mc_field name "Logs older than 30 days"
mc_field category Library
mc_field path "$logs"
mc_field size "$size"
mc_field admin "$(mc_needs_admin "$logs")"
mc_field grade regenerates
mc_field default delete
mc_field cmd_delete "find $(mc_quote "$logs") -type f -mtime +30 -delete"
mc_field explain "Log files in ~/Library/Logs that have not changed for over 30 days. Apps write new logs as needed, and old logs only matter when you are hunting a past problem."
mc_end
