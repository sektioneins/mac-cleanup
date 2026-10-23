#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

for dir in "$MC_ROOT"/Library/Java/JavaVirtualMachines/*.jdk; do
	[ -d "$dir" ] || continue
	size=$(mc_size "$dir")
	[ "$size" -gt 0 ] || continue
	name=$(basename "$dir" .jdk)

	mc_item
	mc_field id "$name"
	mc_field name "$name"
	mc_field category Developer
	mc_path_fields "$(mc_needs_admin "$dir")" "$dir"
	mc_field grade mayneed
	mc_field default trash
	mc_field explain "The Java runtime in the folder $name.jdk. Tools and projects that need this Java version will fail until it is reinstalled."
	mc_end
done
