#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

base="$HOME/Library/Application Support/Microsoft/Teams"
paths=()
for n in 'Service Worker/CacheStorage' Cache 'Code Cache' GPUCache; do
	[ -d "$base/$n" ] && paths+=("$base/$n")
done
[ "${#paths[@]}" -gt 0 ] || exit 0
size=$(mc_size "${paths[@]}")
[ "$size" -gt 0 ] || exit 0

mc_item
mc_field id caches
mc_field name "Teams caches"
mc_field category "App data"
mc_path_fields "$(mc_needs_admin "${paths[@]}")" "${paths[@]}"
mc_field grade regenerates
mc_field default delete
# Classic Teams only: its main process is .../Contents/MacOS/Teams, in
# "Microsoft Teams.app" or "Microsoft Teams classic.app". New Teams runs as
# MSTeams and keeps its data elsewhere.
mc_warn_running 'Microsoft Teams[^/]*\.app/Contents/MacOS/Teams' 'Teams classic'
mc_field explain "Cached web content, scripts and graphics of the classic Teams app. Teams rebuilds them on the next start, so removing them costs a slower first launch; chats and sign-in stay."
mc_end
