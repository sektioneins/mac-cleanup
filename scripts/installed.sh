#!/bin/bash
# Inventory of installed apps for the leftover targets (bash 3.2 compatible).
# Source it after lib.sh with:
#   # shellcheck source=../installed.sh
#   . "$(dirname "$0")/../installed.sh"
# Nothing here modifies the system.

MC_ROOT="${MC_ROOT:-}"
MC_LSREGISTER="$MC_ROOT/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

# Field separator between the parts of a record (not whitespace, so empty
# fields survive `read`).
_MC_US=$'\037'

# _mc_registry_records DUMP: one line per registry record with the fields
# path, id, team, name, displayName, executable basename and unmounted flag.
_mc_registry_records() {
	awk -v US="$_MC_US" '
	function flush() {
		if (path != "" || id != "")
			print path US id US team US name US disp US exe US unm
		path = id = team = name = disp = exe = ""
		unm = 0
	}
	function val(s) { sub(/^[^:]*:[ ]*/, "", s); return s }
	/^-----------------------------/ { flush(); next }
	/^path:/ {
		if (path == "") { path = val($0); sub(/ \(0x[0-9a-fA-F]+\)$/, "", path) }
		next
	}
	/^identifier:/ { if (id == "") id = val($0); next }
	/^teamID:/ { if (team == "") team = val($0); next }
	/^name:/ { if (name == "") name = val($0); next }
	/^displayName:/ { if (disp == "") disp = val($0); next }
	/^executable:/ { if (exe == "") { exe = val($0); sub(/.*\//, "", exe) } next }
	/^mount state:/ { if (val($0) == "not mounted") unm = 1; next }
	END { flush() }
	' "$1"
}

# _mc_emit KIND VALUE: one tagged line for the file splitter.
_mc_emit() { printf '%s%s%s\n' "$1" "$_MC_US" "$2"; }

# _mc_plist_apps ERRFILE: tagged lines for the apps found by their Info.plist.
# Problems that could hide an installed id (unreadable folder, unparsable
# Info.plist) become `error` lines.
_mc_plist_apps() {
	local errf=$1 base app plist rel dirpart part key v file xml
	for base in "$MC_ROOT/Applications" "$HOME/Applications" "$MC_ROOT/System/Applications"; do
		[ -d "$base" ] || continue
		: >"$errf"
		find "$base" -maxdepth 3 -name '*.app' -prune -print 2>"$errf" | while IFS= read -r app; do
			file=${app##*/}
			_mc_emit names "${file%.app}"
			rel=${app#"$base"/}
			dirpart=$(dirname "$rel")
			if [ "$dirpart" != . ]; then
				while IFS= read -r part; do
					[ -n "$part" ] && _mc_emit names "${part%.localized}"
				done <<EOF
$(printf '%s\n' "$dirpart" | tr '/' '\n')
EOF
			fi
			plist="$app/Contents/Info.plist"
			if [ ! -f "$plist" ]; then
				if [ -d "$app/Contents" ] && [ ! -x "$app/Contents" ]; then
					_mc_emit error "cannot read $app"
				fi
				continue
			fi
			# plutil -lint accepts any text as an old-style plist, so check
			# that the file parses and that its top level is a dictionary.
			xml=$(plutil -convert xml1 -o - "$plist" 2>/dev/null) || xml=""
			case $xml in
			*"<dict>"* | *"<dict/>"*) ;;
			*)
				_mc_emit error "cannot parse $plist"
				continue
				;;
			esac
			# Keys that are absent are skipped: such a bundle has no id or name.
			for key in CFBundleIdentifier CFBundleName CFBundleDisplayName CFBundleExecutable; do
				v=$(plutil -extract "$key" raw -o - "$plist" 2>/dev/null) || continue
				if [ "$key" = CFBundleIdentifier ]; then
					_mc_emit ids "$v"
				else
					_mc_emit names "$v"
				fi
			done
		done
		[ -s "$errf" ] && _mc_emit error "cannot read $base"
	done
}

# _mc_registry_tagged DUMP: tagged lines from the registry records.
_mc_registry_tagged() {
	local path id team name disp exe unm installed lid
	_mc_registry_records "$1" | while IFS=$_MC_US read -r path id team name disp exe unm; do
		installed=0
		if [ "$unm" = 1 ]; then
			installed=1
		elif [ -n "$path" ] && [ -e "$path" ]; then
			installed=1
		fi
		if [ "$installed" = 0 ]; then
			[ -n "$id" ] && [ -n "$path" ] && _mc_emit stale "$id	$name"
			continue
		fi
		_mc_emit ids "$id"
		_mc_emit teams "$team"
		_mc_emit names "$name"
		_mc_emit names "$disp"
		_mc_emit names "$exe"
		lid=$(printf '%s' "$id" | tr '[:upper:]' '[:lower:]')
		case $lid in
		com.apple.*)
			_mc_emit apple "$name"
			_mc_emit apple "$disp"
			;;
		esac
	done
}

# _mc_apple_services: tagged lines for the system's own launch plists.
_mc_apple_services() {
	local d f x
	for d in "$MC_ROOT/System/Library/LaunchAgents" "$MC_ROOT/System/Library/LaunchDaemons"; do
		[ -d "$d" ] || continue
		for f in "$d"/com.apple.*.plist; do
			[ -e "$f" ] || continue
			x=${f##*/com.apple.}
			_mc_emit apple "${x%.plist}"
		done
	done
}

# mc_inventory DIR: write the installed-app lists ids, teams, names, vendors,
# apple and stale to DIR. Fails when the Launch Services registry can't be
# read.
mc_inventory() {
	local dir=$1 f
	if ! "$MC_LSREGISTER" -dump >"$dir/dump.raw" 2>/dev/null; then
		echo "mac-cleanup: could not read the Launch Services registry ($MC_LSREGISTER)" >&2
		rm -f "$dir/dump.raw"
		return 1
	fi
	if ! grep -q '^identifier:' "$dir/dump.raw"; then
		echo "mac-cleanup: the Launch Services registry has no usable records" >&2
		rm -f "$dir/dump.raw"
		return 1
	fi
	for f in ids teams names apple stale error; do : >"$dir/$f.raw"; done
	{
		_mc_registry_tagged "$dir/dump.raw"
		_mc_plist_apps "$dir/find.err"
		_mc_apple_services
	} | awk -F"$_MC_US" -v d="$dir" '{ print substr($0, length($1) + 2) >> (d "/" $1 ".raw") }'
	rm -f "$dir/find.err"
	if [ -s "$dir/error.raw" ]; then
		sed 's/^/mac-cleanup: /' "$dir/error.raw" >&2
		rm -f "$dir/dump.raw" "$dir"/{ids,teams,names,apple,stale,error}.raw
		return 1
	fi

	for f in ids names apple; do
		tr '[:upper:]' '[:lower:]' <"$dir/$f.raw" | grep -v '^$' | sort -u >"$dir/$f"
	done
	grep -v -e '^$' -e '^0000000000$' "$dir/teams.raw" | sort -u >"$dir/teams"
	awk -F'\t' 'BEGIN { OFS = "\t" } { $1 = tolower($1); print }' "$dir/stale.raw" >"$dir/stale"
	awk -F. 'NF >= 3 { print $2 }' "$dir/ids" | sort -u >"$dir/vendors"
	rm -f "$dir/dump.raw" "$dir"/{ids,teams,names,apple,stale,error}.raw
	return 0
}

# mc_is_bundle_id NAME: status 0 when NAME looks like a bundle identifier.
mc_is_bundle_id() {
	local re='^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+){2,}$'
	[[ $1 =~ $re ]]
}

# mc_unclaimed DIR: filter for `id<TAB>rest` lines; prints those whose id is
# not claimed by DIR/ids.
mc_unclaimed() {
	if [ ! -r "$1/ids" ]; then
		echo "mac-cleanup: no installed-app list in $1" >&2
		return 1
	fi
	awk -F'\t' -v idsfile="$1/ids" '
	BEGIN {
		while ((getline l < idsfile) > 0) {
			if (l == "") continue
			ids[l] = 1
			for (i = index(l, "."); i > 0; i = (j = index(rest, ".")) ? i + j : 0) {
				rest = substr(l, i + 1)
				pre[substr(l, 1, i - 1)] = 1
			}
		}
	}
	{
		id = tolower($1)
		claimed = (id in ids) || (id in pre)
		for (i = 1; !claimed && i <= length(id); i++)
			if (substr(id, i, 1) == "." && (substr(id, 1, i - 1) in ids)) claimed = 1
		if (!claimed) print
	}'
}

# mc_quiet DAYS PATH...: 0 when no regular file under the paths was accessed
# or modified within DAYS days, 1 when one was, 2 when find reported an error.
# Directory times are ignored; symlinks are not followed.
mc_quiet() {
	local days=$1 err found
	shift
	err=$(mktemp "${TMPDIR:-/tmp}/mc-quiet.XXXXXX") || return 2
	found=$(find "$@" -type f \( -atime "-$days" -o -mtime "-$days" \) -print -quit 2>"$err")
	if [ -s "$err" ]; then
		rm -f "$err"
		return 2
	fi
	rm -f "$err"
	[ -z "$found" ]
}

# mc_newest_mtime PATH...: newest modification time (Unix seconds) of the
# regular files under the paths, or nothing.
mc_newest_mtime() {
	find "$@" -type f -exec stat -f %m {} + 2>/dev/null | sort -n | tail -n 1
}

_MC_LSAPPINFO_DONE=0
_MC_LSAPPINFO_RC=0
_MC_LSAPPINFO_OUT=""

# mc_warn_bundles LABEL ID...: warn when an app with one of the bundle ids
# runs. lsappinfo is called once per script.
mc_warn_bundles() {
	local label=$1 id lid
	shift
	if [ "$_MC_LSAPPINFO_DONE" = 0 ]; then
		_MC_LSAPPINFO_DONE=1
		_MC_LSAPPINFO_RC=0
		_MC_LSAPPINFO_OUT=$(lsappinfo list 2>/dev/null) || _MC_LSAPPINFO_RC=$?
		_MC_LSAPPINFO_OUT=$(printf '%s' "$_MC_LSAPPINFO_OUT" | tr '[:upper:]' '[:lower:]')
	fi
	if [ "$_MC_LSAPPINFO_RC" != 0 ]; then
		mc_field warn "Could not check whether $label is running"
		return 0
	fi
	for id in "$@"; do
		lid=$(printf '%s' "$id" | tr '[:upper:]' '[:lower:]')
		case $_MC_LSAPPINFO_OUT in
		*"bundleid=\"$lid\""*)
			mc_field warn "$label is running: quit it first"
			return 0
			;;
		esac
	done
}
