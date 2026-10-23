#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

# Installed images are read from disk: <sdk>/system-images/<api>/<tag>/<abi>
# with a package.xml or source.properties. sdkmanager is never run here (it
# starts Java and may go online); it only appears in the command text.
sdkroot="$HOME/Library/Android/sdk"
sdkmanager="$sdkroot/cmdline-tools/latest/bin/sdkmanager"
images="$sdkroot/system-images"
[ -x "$sdkmanager" ] || exit 0
[ -e "$images" ] || exit 0
if [ ! -d "$images" ] || [ ! -r "$images" ] || [ ! -x "$images" ]; then
	echo "$images is not readable" >&2
	exit 1
fi

for dir in "$images"/*/*/*; do
	[ -d "$dir" ] || continue
	[ -f "$dir/package.xml" ] || [ -f "$dir/source.properties" ] || continue
	abi=$(basename "$dir")
	tag=$(basename "$(dirname "$dir")")
	api=$(basename "$(dirname "$(dirname "$dir")")")
	pkg="system-images;$api;$tag;$abi"
	size=$(mc_size "$dir")
	[ "$size" -gt 0 ] || continue

	mc_item
	mc_field id "$pkg"
	mc_field name "Android image ${api#android-} $tag $abi"
	mc_field category Developer
	mc_field path "$dir"
	mc_field size "$size"
	# sdkmanager runs as you, never with sudo.
	mc_field admin 0
	mc_field cmd_delete "$(mc_quote "$sdkmanager") --uninstall $(mc_quote "$pkg")"
	mc_field grade redownload
	mc_field default delete
	for d in "$dir" "$(dirname "$dir")"; do
		[ -w "$d" ] || mc_field warn "$d is not writable by you"
	done
	mc_warn_running qemu-system 'Android emulator'
	mc_field explain "An Android emulator system image. Android Studio or sdkmanager can download it again, but emulators that use it will not start until you do."
	mc_end
done
