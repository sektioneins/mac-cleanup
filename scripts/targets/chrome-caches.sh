#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

base="$HOME/Library/Application Support/Google/Chrome"
[ -d "$base" ] || exit 0

paths=()
while IFS= read -r -d '' p; do
	paths+=("$p")
done < <(find "$base" -type d \( -name Cache -o -name 'Service Worker' \) -prune -print0 2>/dev/null | sort -z)
[ "${#paths[@]}" -gt 0 ] || exit 0
size=$(mc_size "${paths[@]}")
[ "$size" -gt 0 ] || exit 0

mc_item
mc_field id caches
mc_field name "Chrome caches"
mc_field category Browsers
mc_path_fields "$(mc_needs_admin "${paths[@]}")" "${paths[@]}"
mc_field grade regenerates
mc_field default delete
mc_warn_running 'Google Chrome.app/Contents/MacOS/Google Chrome' 'Chrome'
mc_field explain "Cached web content and service worker data in every Chrome profile. Chrome rebuilds them as you browse, so removing them costs slower first page loads; logins and bookmarks stay. Deleting Service Worker data also removes offline data and push notification registrations of sites and installed web apps; they set themselves up again on the next visit."
mc_end
