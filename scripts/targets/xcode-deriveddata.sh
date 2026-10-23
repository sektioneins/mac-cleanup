#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

dir="$HOME/Library/Developer/Xcode/DerivedData"
[ -d "$dir" ] || exit 0
size=$(mc_size "$dir")
[ "$size" -gt 0 ] || exit 0

mc_item
mc_field id deriveddata
mc_field name "Xcode DerivedData"
mc_field category Developer
mc_path_fields "$(mc_needs_admin "$dir")" "$dir"
mc_field grade regenerates
mc_field default delete
mc_warn_running 'Xcode.app/Contents/MacOS/Xcode' 'Xcode'
mc_field explain "Build products and indexes that Xcode creates for your projects. Xcode rebuilds them on the next build, so removing them only makes the first build and indexing slower."
mc_end
