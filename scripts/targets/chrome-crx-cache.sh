#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

dir="$HOME/Library/Application Support/Google/GoogleUpdater/crx_cache"
[ -d "$dir" ] || exit 0
size=$(mc_size "$dir")
[ "$size" -gt 0 ] || exit 0

mc_item
mc_field id crx
mc_field name "Chrome updater download cache"
mc_field category Browsers
mc_path_fields "$(mc_needs_admin "$dir")" "$dir"
mc_field grade redownload
mc_field default delete
mc_field explain "Installer packages the Google updater kept after updating Chrome and other Google software. They are downloaded again when needed, so removing them only costs a download."
mc_end
