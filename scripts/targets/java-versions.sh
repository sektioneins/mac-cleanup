#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

# Resolved java.home of the java command, looked up once when a JDK is found.
java_home=
java_checked=0

# Prints the resolved java.home, or nothing when java is missing or prints none.
mc_java_home() {
	local home
	# -XX:-UsePerfData: no hsperfdata file is written during the scan.
	home=$(java -XX:-UsePerfData -XshowSettings:properties -version 2>&1 | sed -n 's/^[[:space:]]*java\.home = //p' | head -n 1)
	[ -n "$home" ] || return 0
	(cd -P "$home" 2>/dev/null && pwd -P)
}

for dir in "$MC_ROOT"/Library/Java/JavaVirtualMachines/*.jdk; do
	[ -d "$dir" ] || continue
	size=$(mc_size "$dir")
	[ "$size" -gt 0 ] || continue
	id=$(basename "$dir" .jdk)
	name=$id
	explain="The Java runtime in the folder $id.jdk. Tools and projects that need this Java version will fail until it is reinstalled."

	if [ "$java_checked" -eq 0 ]; then
		java_checked=1
		java_home=$(mc_java_home)
	fi
	if [ -n "$java_home" ]; then
		resolved=$(cd -P "$dir" 2>/dev/null && pwd -P)
		case "$java_home/" in
		"$resolved"/*)
			if [ -n "$resolved" ]; then
				name="$name (active)"
				explain="$explain This is the Java version the java command currently uses."
			fi
			;;
		esac
	fi

	mc_item
	mc_field id "$id"
	mc_field name "$name"
	mc_field category Developer
	mc_path_fields "$(mc_needs_admin "$dir")" "$dir"
	mc_field grade mayneed
	mc_field default trash
	mc_field explain "$explain"
	mc_end
done
