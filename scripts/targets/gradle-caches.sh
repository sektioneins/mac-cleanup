#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

dir="$HOME/.gradle/caches"
[ -d "$dir" ] || exit 0
size=$(mc_size "$dir")
[ "$size" -gt 0 ] || exit 0

mc_item
mc_field id caches
mc_field name "Gradle caches"
mc_field category Developer
mc_path_fields "$(mc_needs_admin "$dir")" "$dir"
mc_field grade regenerates
mc_field default delete
mc_warn_running 'GradleDaemon' 'Gradle'
mc_field explain "Downloaded build dependencies. Gradle fetches them again on the next build, so removing them costs download time and a slower first build."
mc_end
