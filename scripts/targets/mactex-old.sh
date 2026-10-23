#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

texbin=$(cd -P "$MC_ROOT/Library/TeX/texbin" 2>/dev/null && pwd -P) || exit 0
active=$(printf '%s\n' "$texbin" | sed -nE 's|.*/texlive/([0-9]{4})(/.*)?$|\1|p')
[ -n "$active" ] || exit 0

for dir in "$MC_ROOT"/usr/local/texlive/[0-9][0-9][0-9][0-9]; do
	[ -d "$dir" ] || continue
	year=$(basename "$dir")
	[ "$year" = "$active" ] && continue
	size=$(mc_size "$dir")
	[ "$size" -gt 0 ] || continue

	mc_item
	mc_field id "$year"
	mc_field name "TeX Live $year"
	mc_field category Developer
	mc_path_fields "$(mc_needs_admin "$dir")" "$dir"
	mc_field grade redownload
	mc_field default delete
	mc_field explain "An older TeX Live release from $year that MacTeX no longer uses. The current release keeps working, and the old one can be installed again from MacTeX."
	mc_end
done
