#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"
# shellcheck source=../installed.sh
. "$(dirname "$0")/../installed.sh"
# shellcheck source=../autostart.sh
. "$(dirname "$0")/../autostart.sh"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/mc-login.XXXXXX") || exit 1
trap 'rm -rf "$tmp"' EXIT
mc_inventory "$tmp" || exit 1
mc_btm "$tmp/btm" || exit 1

uid=$(id -u)
gui="gui/$uid"

: >"$tmp/mine"
# The current user's records only. Of those, the ones that can be login items:
# apps that open at login (type app, enabled; the disabled app records are only
# parents of other records) and the agents and login items an app registers
# (not the legacy ones, which the launchd target lists).
MC_U=$uid awk -F"$_MC_US" '
$1 != ENVIRON["MC_U"] { next }
{
	print >> mine
	d = $4
	sub(/,.*/, "", d)
	f = $3
	gsub(/ /, "", f)
	f = "," f ","
	if ($2 == "app" && d == "enabled") print
	else if (($2 == "agent" || $2 == "login item") && index(f, ",legacy,") == 0) print
}' mine="$tmp/mine" "$tmp/btm" >"$tmp/cand"
[ -s "$tmp/cand" ] || exit 0

# Every source must be readable: a missing one would show running or enabled
# items as stopped or unowned.
mc_ps_snapshot "$tmp/ps" || exit 1
if awk -F"$_MC_US" '$2 != "app" { found = 1 } END { exit !found }' "$tmp/cand"; then
	if ! mc_launchd_domain_ok "$gui"; then
		echo "mac-cleanup: launchctl cannot print the domain $gui" >&2
		exit 1
	fi
	mc_disabled_labels "$gui" "$tmp/disabled" || exit 1
else
	: >"$tmp/disabled"
fi

# real PATH: where a path from BTM lies on disk (under MC_ROOT, except in HOME).
real() {
	case $1 in
	"$HOME"/*) printf '%s' "$1" ;;
	*) printf '%s%s' "$MC_ROOT" "$1" ;;
	esac
}

# lower STR: STR in lower case.
lower() {
	printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

# is_apple STR...: status 0 when one of the strings is an Apple id.
is_apple() {
	local s
	for s in "$@"; do
		case $(lower "$s") in
		com.apple.*) return 0 ;;
		esac
	done
	return 1
}

# Ids already used (the scanner rejects a script that prints one twice).
used=$'\n'

# claim_id ID [ALT]: set id to ID, or to ALT when ID is taken, followed by
# (2), (3) ... until it is not taken, and remember it.
claim_id() {
	local n=2
	id=$1
	case $used in
	*$'\n'"$id"$'\n'*) [ -z "${2:-}" ] || id=$2 ;;
	esac
	local base=$id
	while :; do
		case $used in
		*$'\n'"$id"$'\n'*) id="$base ($n)" ;;
		*) break ;;
		esac
		n=$((n + 1))
	done
	used="$used$id"$'\n'
}

while IFS=$_MC_US read -r -u 3 _ r_type _ r_disp r_name r_dev r_id r_url r_exe r_bundle r_parent _; do
	# Disposition: enabled/disabled, allowed/disallowed, notified.
	d1=${r_disp%%,*}
	d2=${r_disp#*,}
	d2=${d2# }
	d2=${d2%%,*}
	enabled=on
	{ [ "$d1" = disabled ] || [ "$d2" = disallowed ]; } && enabled=off

	owner="" orphan="" path="" label="" name="" id="" explain=""
	cmd_stop="" cmd_disable="" memory=0 state=stopped
	loaded=0

	if [ "$r_type" = app ]; then
		# An app that opens at login.
		[ -n "$r_url" ] || continue
		bundle=$(mc_url_path "$r_url")
		[ -n "$bundle" ] || continue
		ident=$(mc_btm_id "$r_id")
		is_apple "$r_bundle" "$ident" && continue
		case $bundle in
		/System/*) continue ;;
		esac
		name=$r_name
		[ -n "$name" ] || name=$(basename "$bundle" .app)
		owner=$r_dev
		path=$bundle
		# Ids have no double slash: the path comes without its first /.
		if [ -n "$r_bundle" ]; then
			claim_id "login/$r_bundle" "login/${bundle#/}"
		else
			claim_id "login/${bundle#/}"
		fi

		pids=$(mc_pids_under "$tmp/ps" "$bundle/")
		if [ -n "$pids" ]; then
			state=running
			# The pids are digits only.
			# shellcheck disable=SC2086
			memory=$(mc_tree_rss "$tmp/ps" "$tmp/claimed" $pids)
			if [ -n "$r_bundle" ]; then
				script="tell application id $(mc_as_quote "$r_bundle") to quit"
				cmd_stop="osascript -e $(mc_quote "$script")"
			fi
		fi
		script="tell application \"System Events\" to delete (every login item whose path is $(mc_as_quote "$bundle"))"
		cmd_disable="osascript -e $(mc_quote "$script")"

		if mc_program_missing "$bundle"; then
			orphan="program missing"
		fi
		if [ "$enabled" = off ] && [ -n "$cmd_stop" ]; then
			default=stop
		else
			default=disable
		fi
		if [ -n "$owner" ]; then
			who="Login item of $owner"
		else
			who="Login item of an unknown owner"
		fi
		explain="$who, opened at every login. Disable removes it from the login items; the app stays installed."
		[ -n "$cmd_stop" ] && explain="$explain Stop quits it for now."
	else
		# An agent or login item that an app registered. Its URL and
		# executable path lie in the parent app's bundle.
		p_type="" p_name="" p_dev="" p_url="" p_bundle="" p_team="" bpath=""
		if [ -n "$r_parent" ]; then
			prec=$(MC_ID=$r_parent awk -F"$_MC_US" '
				$7 == ENVIRON["MC_ID"] {
					if ($2 == "app") { app = $0; exit }
					if (other == "") other = $0
				}
				END { print (app != "" ? app : other) }' "$tmp/mine")
			if [ -n "$prec" ]; then
				IFS=$_MC_US read -r _ p_type _ _ p_name p_dev _ p_url _ p_bundle _ p_team <<<"$prec"
			fi
		fi
		if [ "$p_type" = app ] && [ -n "$p_url" ]; then
			bpath=$(mc_url_path "$p_url")
			case $bpath in
			/*) ;;
			*) bpath="" ;;
			esac
		fi
		rel=""
		[ -n "$r_url" ] && rel=$(mc_url_path "$r_url")

		# The launchd label.
		if [ "$r_type" = "login item" ]; then
			label=$(mc_btm_id "$r_id")
			[ -n "$label" ] || label=$r_bundle
		else
			if [ -n "$bpath" ] && [ -n "$rel" ]; then
				case $rel in
				/*) f=$rel ;;
				*) f=$bpath/$rel ;;
				esac
				label=$(plutil -extract Label raw -o - "$(real "$f")" 2>/dev/null) || label=""
			fi
			if [ -z "$label" ] && [ -n "$rel" ]; then
				label=$(basename "$rel" .plist)
			fi
			[ -n "$label" ] || label=$(mc_btm_id "$r_id")
		fi
		[ -n "$label" ] || continue
		is_apple "$label" "$r_bundle" "$(mc_btm_id "$r_id")" "$p_bundle" && continue
		case $bpath in
		/System/*) continue ;;
		esac

		name=$label
		if [ "$r_type" = "login item" ] && [ -n "$r_name" ]; then
			name=$r_name
		fi
		owner=$p_name
		[ -n "$owner" ] || owner=$r_dev
		[ -n "$owner" ] || owner=$p_dev
		[ -n "$owner" ] || [ "$p_type" != developer ] || owner=$r_parent
		path=$bpath
		claim_id "$gui/$label" "$gui/$label ($r_url)"

		# The program inside the parent bundle.
		program=""
		if [ "$r_type" = "login item" ]; then
			case $rel in
			"") ;;
			/*) program=$rel ;;
			*) [ -z "$bpath" ] || program=$bpath/$rel ;;
			esac
		else
			case $r_exe in
			"") ;;
			/*) program=$r_exe ;;
			*) [ -z "$bpath" ] || program=$bpath/$r_exe ;;
			esac
		fi

		# An app record whose bundle id and team no installed app claims
		# and whose bundle is not on disk.
		if [ "$p_type" = app ]; then
			mc_app_deleted "$tmp" "$p_bundle" "$p_team" "$bpath"
			case $? in
			0) orphan="app deleted" ;;
			2) exit 1 ;;
			esac
		fi
		if [ -z "$orphan" ] && [ -n "$program" ] && mc_program_missing "$program"; then
			orphan="program missing"
		fi

		# State and memory. The domain was checked above, so a job that
		# launchctl cannot print is not loaded.
		pid=""
		if job=$(mc_launchd_job "$gui/$label"); then
			loaded=1
			IFS=$'\t' read -r _ pid <<<"$job"
			case $pid in
			*[!0-9]*) pid="" ;;
			esac
		fi
		if [ -n "$pid" ]; then
			state=running
			memory=$(mc_tree_rss "$tmp/ps" "$tmp/claimed" "$pid")
		fi
		if MC_L=$label awk '$0 == ENVIRON["MC_L"] { found = 1 } END { exit !found }' "$tmp/disabled"; then
			enabled=off
		fi

		# Commands: the same as for launchd agents, without Trash.
		target=$(mc_service_target "$gui" "$label")
		boot="launchctl bootout $target"
		if [ "$loaded" = 1 ]; then
			cmd_stop=$boot
			cmd_disable="$boot && launchctl disable $target"
		elif [ "$enabled" = on ]; then
			cmd_disable="launchctl disable $target"
		fi
		if [ -n "$cmd_disable" ]; then
			default=disable
		elif [ -n "$cmd_stop" ]; then
			default=stop
		else
			default=""
		fi

		if [ "$r_type" = "login item" ]; then
			kind="Background login item"
		else
			kind="Background agent"
		fi
		if [ -n "$owner" ]; then
			who="$kind of $owner"
		else
			who="$kind of an unknown owner"
		fi
		if [ -n "$cmd_disable" ]; then
			what="Disable stops it and keeps it from starting again, but System Settings may still show it as allowed."
		else
			what="It is turned off and not running, but System Settings may still show it as allowed."
		fi
		explain="$who, started at every login. Program: ${program:-unknown}. $what"
	fi

	mc_item
	mc_field id "$id"
	mc_field name "$name"
	mc_field category "Login items"
	mc_field tab autostart
	[ -n "$path" ] && mc_field path "$path"
	mc_field memory "${memory:-0}"
	mc_field state "$state"
	mc_field enabled "$enabled"
	[ -n "$owner" ] && mc_field owner "$owner"
	[ -n "$orphan" ] && mc_field orphan "$orphan"
	mc_field admin 0
	[ -n "$cmd_stop" ] && mc_field cmd_stop "$cmd_stop"
	[ -n "$cmd_disable" ] && mc_field cmd_disable "$cmd_disable"
	[ -n "$default" ] && mc_field default "$default"
	mc_field explain "$explain"
	mc_end
done 3<"$tmp/cand"
exit 0
