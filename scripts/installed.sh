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

# Separator between the application-groups values of one record (they hold no
# tab, US or GS).
_MC_GS=$'\035'

# _mc_registry_records DUMP: one line per registry record with the fields
# path, id, team, name, displayName, executable basename, unmounted flag,
# apple flag (the id starts with com.apple., case-insensitive) and the
# application-groups entitlement values joined by GS.
_mc_registry_records() {
	awk -v US="$_MC_US" -v GS="$_MC_GS" '
	function flush() {
		if (path != "" || id != "")
			print path US id US team US name US disp US exe US unm US (index(tolower(id), "com.apple.") == 1 ? 1 : 0) US groups
		path = id = team = name = disp = exe = groups = ""
		unm = 0
		ingrp = 0
	}
	function val(s) { sub(/^[^:]*:[ ]*/, "", s); return s }
	# addvals S: add the comma separated values of S to groups, unquoted.
	function addvals(s,    n, parts, i, v) {
		n = split(s, parts, ",")
		for (i = 1; i <= n; i++) {
			v = parts[i]
			gsub(/^[ \t\r]+|[ \t\r]+$/, "", v)
			gsub(/^"|"$/, "", v)
			if (v != "") groups = groups (groups == "" ? "" : GS) v
		}
	}
	/^-----------------------------/ { flush(); next }
	ingrp {
		if ($0 ~ /^[ \t]*\);/) { ingrp = 0; next }
		addvals($0)
		next
	}
	/^[ \t]+"?com\.apple\.security\.application-groups"?[ \t]*=/ {
		s = $0
		sub(/^[^(]*\(/, "", s)
		if (s ~ /\);/) { sub(/\);.*/, "", s); addvals(s) }
		else { addvals(s); ingrp = 1 }
		next
	}
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

# _mc_plist_xml PLIST: set _MC_XML to the plist as XML. Status 1 when it does
# not parse or its top level is not a dictionary (plutil -lint accepts any
# text as an old-style plist, so the dictionary is checked as well).
_mc_plist_xml() {
	_MC_XML=$(plutil -convert xml1 -o - "$1" 2>/dev/null) || _MC_XML=""
	case $_MC_XML in
	*"<dict>"* | *"<dict/>"*) return 0 ;;
	esac
	return 1
}

# _mc_plist_apps ERRFILE: tagged lines for the apps found by their Info.plist.
# Problems that could hide an installed id (unreadable folder, unparsable
# Info.plist) become `error` lines.
_mc_plist_apps() {
	local errf=$1 base app plist rel dirpart part key v file
	for base in "$MC_ROOT/Applications" "$HOME/Applications" "$MC_ROOT/System/Applications"; do
		[ -d "$base" ] || continue
		: >"$errf"
		find "$base" -maxdepth 3 -name '*.app' -prune -print 2>"$errf" | while IFS= read -r app; do
			_mc_emit apppath "$app"
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
			if ! _mc_plist_xml "$plist"; then
				_mc_emit error "cannot parse $plist"
				continue
			fi
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
	local path id team name disp exe unm apple groups installed g
	local -a garr
	_mc_registry_records "$1" | while IFS=$_MC_US read -r path id team name disp exe unm apple groups; do
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
		_mc_emit installed 1
		# Apple's own apps are not scanned for nested bundles (their ids are
		# dropped anyway).
		case $path in
		"$MC_ROOT/System/"*) ;;
		*.app) [ "$apple" = 1 ] || { [ -d "$path" ] && _mc_emit regpath "$path$_MC_US$id"; } ;;
		esac
		_mc_emit ids "$id"
		_mc_emit teams "$team"
		_mc_emit names "$name"
		_mc_emit names "$disp"
		_mc_emit names "$exe"
		if [ "$apple" = 1 ]; then
			_mc_emit apple "$name"
			_mc_emit apple "$disp"
		fi
		if [ -n "$groups" ]; then
			IFS=$_MC_GS read -r -a garr <<<"$groups"
			for g in "${garr[@]}"; do
				_mc_emit groups "$g"
			done
		fi
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

# _mc_protected_dir APP: status 0 when APP lies in a place a scan could raise a
# privacy prompt or wake a slow disk.
_mc_protected_dir() {
	case $1 in
	"$HOME/Desktop/"* | "$HOME/Documents/"* | "$HOME/Downloads/"* | "$MC_ROOT/Volumes/"*) return 0 ;;
	esac
	return 1
}

# _mc_vendorclaim ID: tagged `vendorclaim` line with the first two labels of
# ID, lowercase. Nothing for an id with fewer than two labels.
_mc_vendorclaim() {
	local lid first rest
	lid=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
	case $lid in
	*.*)
		first=${lid%%.*}
		rest=${lid#*.}
		_mc_emit vendorclaim "$first.${rest%%.*}"
		;;
	esac
}

# _mc_nested_ids APPLIST STRICT: tagged lines for the bundle ids of the .xpc,
# .appex and .app bundles nested in the installed apps listed in APPLIST (one
# `path` per line, or `path<US>id` for the registry's apps). With STRICT=1 a
# folder that can't be read is an error. The registry's apps (STRICT=0) can sit
# in protected places like ~/Documents; they are not scanned there, and when
# their Contents folder is missing or can't be read, or find reports an error,
# they claim the vendor of their own id (`vendorclaim`) instead, because their
# nested bundles may have been missed. LaunchServices does not register
# nested bundles, but a sandboxed one has its own ~/Library/Containers folder.
# Two plutil calls per bundle; a bundle without an Info.plist or without an id
# is skipped, an unparsable Info.plist is an error.
_mc_nested_ids() {
	local app id b plist err v
	if ! err=$(mktemp "${TMPDIR:-/tmp}/mc-nested.XXXXXX"); then
		_mc_emit error "cannot create a temporary file"
		return 1
	fi
	sort -u "$1" | while IFS=$_MC_US read -r app id; do
		if [ "$2" != 1 ] && _mc_protected_dir "$app"; then
			_mc_vendorclaim "$id"
			continue
		fi
		if [ ! -d "$app/Contents" ]; then
			[ "$2" = 1 ] || _mc_vendorclaim "$id"
			continue
		fi
		: >"$err"
		find "$app/Contents" -maxdepth 7 -type d \( -name '*.xpc' -o -name '*.appex' -o -name '*.app' \) -print 2>"$err" |
			while IFS= read -r b; do
				plist="$b/Contents/Info.plist"
				[ -f "$plist" ] || plist="$b/Info.plist"
				[ -f "$plist" ] || continue
				if ! _mc_plist_xml "$plist"; then
					_mc_emit error "cannot parse $plist"
					continue
				fi
				v=$(plutil -extract CFBundleIdentifier raw -o - "$plist" 2>/dev/null) && _mc_emit ids "$v"
			done
		if [ -s "$err" ]; then
			if [ "$2" = 1 ]; then
				_mc_emit error "cannot read $app"
			else
				_mc_vendorclaim "$id"
			fi
		fi
	done
	rm -f "$err"
}

# _mc_split DIR: file the tagged lines from stdin into DIR/<tag>.raw.
_mc_split() {
	awk -F"$_MC_US" -v d="$1" '{ print substr($0, length($1) + 2) >> (d "/" $1 ".raw") }'
}

# mc_inventory DIR: write the installed-app lists ids, teams, names, vendors,
# apple, stale, groups (application-groups entitlements of installed apps) and
# vendorclaim (vendor pairs of registry apps whose nested bundles could not be
# checked; sorted, unique, possibly empty) to DIR. Fails when the Launch Services registry can't be
# read.
mc_inventory() {
	local dir=$1 f
	if ! "$MC_LSREGISTER" -dump >"$dir/dump.raw" 2>/dev/null; then
		echo "mac-cleanup: could not read the Launch Services registry ($MC_LSREGISTER)" >&2
		rm -f "$dir/dump.raw"
		return 1
	fi
	for f in ids teams names apple stale error apppath regpath installed groups vendorclaim; do : >"$dir/$f.raw"; done
	{
		_mc_registry_tagged "$dir/dump.raw"
		_mc_plist_apps "$dir/find.err"
		_mc_apple_services
	} | _mc_split "$dir"
	rm -f "$dir/find.err"
	# Apple's own apps under System/Applications are not scanned.
	grep -v -F -e "$MC_ROOT/System/Applications/" "$dir/apppath.raw" >"$dir/apppath.scan"
	_mc_nested_ids "$dir/apppath.scan" 1 | _mc_split "$dir"
	rm -f "$dir/apppath.scan"
	# Apps already scanned strictly are not scanned again.
	awk -F"$_MC_US" 'NR == FNR { a[$0] = 1; next } !($1 in a)' "$dir/apppath.raw" "$dir/regpath.raw" | sort -u >"$dir/regpath.new"
	_mc_nested_ids "$dir/regpath.new" 0 | _mc_split "$dir"
	rm -f "$dir/regpath.new"
	if [ -s "$dir/error.raw" ]; then
		sed 's/^/mac-cleanup: /' "$dir/error.raw" >&2
		rm -f "$dir/dump.raw" "$dir"/{ids,teams,names,apple,stale,error,apppath,regpath,installed,groups,vendorclaim}.raw
		return 1
	fi
	if [ ! -s "$dir/installed.raw" ]; then
		echo "mac-cleanup: the Launch Services registry has no installed app records" >&2
		rm -f "$dir/dump.raw" "$dir"/{ids,teams,names,apple,stale,error,apppath,regpath,installed,groups,vendorclaim}.raw
		return 1
	fi

	for f in ids names apple; do
		tr '[:upper:]' '[:lower:]' <"$dir/$f.raw" | grep -v '^$' | sort -u >"$dir/$f"
	done
	grep -v -e '^$' -e '^0000000000$' "$dir/teams.raw" | sort -u >"$dir/teams"
	awk -F'\t' 'BEGIN { OFS = "\t" } { $1 = tolower($1); print }' "$dir/stale.raw" >"$dir/stale"
	for f in groups vendorclaim; do
		tr '[:upper:]' '[:lower:]' <"$dir/$f.raw" | grep -v '^$' | sort -u >"$dir/$f"
	done
	awk -F. 'NF >= 3 { print $2 }' "$dir/ids" | sort -u >"$dir/vendors"
	rm -f "$dir/dump.raw" "$dir"/{ids,teams,names,apple,stale,error,apppath,regpath,installed,groups,vendorclaim}.raw
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
