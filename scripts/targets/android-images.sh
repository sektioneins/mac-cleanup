#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

sdkroot="$HOME/Library/Android/sdk"
sdkmanager="$sdkroot/cmdline-tools/latest/bin/sdkmanager"
[ -x "$sdkmanager" ] || exit 0
listing=$("$sdkmanager" --list_installed 2>/dev/null) || exit 0

while IFS='|' read -r pkg _ _ loc; do
	pkg=${pkg#"${pkg%%[![:space:]]*}"}
	pkg=${pkg%"${pkg##*[![:space:]]}"}
	case $pkg in
	system-images\;*) ;;
	*) continue ;;
	esac
	loc=${loc#"${loc%%[![:space:]]*}"}
	loc=${loc%"${loc##*[![:space:]]}"}
	[ -n "$loc" ] || continue
	dir="$sdkroot/$loc"
	[ -d "$dir" ] || continue
	size=$(mc_size "$dir")
	[ "$size" -gt 0 ] || continue

	IFS=';' read -r _ api tag abi <<<"$pkg"
	mc_item
	mc_field id "$pkg"
	mc_field name "Android image ${api#android-} $tag $abi"
	mc_field category Developer
	mc_field path "$dir"
	mc_field size "$size"
	mc_field admin "$(mc_needs_admin "$dir")"
	mc_field cmd_delete "$(mc_quote "$sdkmanager") --uninstall $(mc_quote "$pkg")"
	mc_field grade redownload
	mc_field default delete
	mc_warn_running qemu-system 'Android emulator'
	mc_field explain "An Android emulator system image. Android Studio or sdkmanager can download it again, but emulators that use it will not start until you do."
	mc_end
done <<<"$listing"
