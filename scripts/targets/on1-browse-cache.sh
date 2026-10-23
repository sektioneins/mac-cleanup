#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

dir="$HOME/Library/Application Support/ON1/PerfectBrowseCache"
[ -d "$dir" ] || exit 0
size=$(mc_size "$dir")
[ "$size" -gt 0 ] || exit 0

mc_item
mc_field id browsecache
mc_field name "ON1 browse cache"
mc_field category "App data"
mc_path_fields "$(mc_needs_admin "$dir")" "$dir"
mc_field grade regenerates
mc_field default delete
mc_warn_running 'ON1 Photo RAW' 'ON1 Photo RAW'
mc_field explain "Thumbnails and previews ON1 Photo RAW made for your photo folders. ON1 recreates them when you browse the folders again, so removing them costs time and a slower first browse."
mc_end
