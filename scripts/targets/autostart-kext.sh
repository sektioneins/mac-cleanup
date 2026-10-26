#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"
# shellcheck source=../installed.sh
. "$(dirname "$0")/../installed.sh"
# shellcheck source=../autostart.sh
. "$(dirname "$0")/../autostart.sh"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/mc-kext.XXXXXX") || exit 1
trap 'rm -rf "$tmp"' EXIT

# kmutil prints a notice on stderr even when it works, so only its exit status
# counts; the stderr is shown only when it fails.
if ! loaded=$(kmutil showloaded 2>"$tmp/kmutil.err"); then
	echo "mac-cleanup: could not list the loaded kernel extensions (kmutil failed): $(head -n 3 "$tmp/kmutil.err")" >&2
	exit 1
fi

# The lines after the header: bundle id (field 6) and Wired (field 5, hex).
# The fields are separated by blanks and their number varies, so only these
# two are read. Apple's extensions and repeated ids are left out.
printf '%s\n' "$loaded" | awk -v US="$_MC_US" '
	/^[ \t]*Index[ \t]+Refs[ \t]+Address/ { body = 1; next }
	!body || NF < 6 { next }
	{
		id = $6
		if (tolower(id) ~ /^com\.apple\./ || id in seen) next
		seen[id] = 1
		print id US $5
	}' >"$tmp/kexts"
[ -s "$tmp/kexts" ] || exit 0

mc_teams "$tmp" || exit 1

# The bundles in /Library/Extensions by bundle id: id, US, path.
for k in "$MC_ROOT/Library/Extensions"/*.kext; do
	[ -d "$k" ] || continue
	bid=$(plutil -extract CFBundleIdentifier raw -o - "$k/Contents/Info.plist" 2>/dev/null) || continue
	[ -n "$bid" ] || continue
	printf '%s%s%s\n' "$bid" "$_MC_US" "$k"
done >"$tmp/bundles"

while IFS=$_MC_US read -r id wired; do
	case $wired in
	0[xX][0-9a-fA-F]*) memory=$(printf '%d' "$wired") ;;
	*) memory=0 ;;
	esac

	bundle=$(MC_I=$id awk -F"$_MC_US" '$1 == ENVIRON["MC_I"] { print $2; exit }' "$tmp/bundles")

	# The team of the bundle's signature. An extension is orphaned only when
	# the team is known and none of the installed apps has it.
	orphan=""
	if [ -n "$bundle" ]; then
		team=$(codesign -dv "$bundle" 2>&1 | sed -n 's/^TeamIdentifier=//p' | head -n 1)
		case $team in
		"" | "not set") ;;
		*)
			grep -Fixq -- "$team" "$tmp/teams" || orphan="app deleted"
			;;
		esac
	fi

	mc_item
	mc_field id "kext/$id"
	mc_field name "$id"
	mc_field category "Kernel extensions"
	mc_field tab autostart
	[ -n "$bundle" ] && mc_field path "$bundle"
	mc_field memory "$memory"
	mc_field state loaded
	[ -n "$orphan" ] && mc_field orphan "$orphan"
	mc_field admin 1
	mc_field cmd_stop "sudo kmutil unload -b $(mc_quote "$id")"
	mc_field default stop
	mc_field explain "Third-party kernel extension that is loaded now. Stop unloads it until the next boot; macOS on Apple Silicon may refuse. Only the uninstaller of the app that installed it removes it for good."
	mc_end
done <"$tmp/kexts"
exit 0
