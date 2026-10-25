#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"
# shellcheck source=../installed.sh
. "$(dirname "$0")/../installed.sh"
# shellcheck source=../autostart.sh
. "$(dirname "$0")/../autostart.sh"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/mc-launchd.XXXXXX") || exit 1
trap 'rm -rf "$tmp"' EXIT
mc_inventory "$tmp" || exit 1

uid=$(id -u)
gui="gui/$uid"

# The plists (regular files only) and the launchd domain each one loads into.
# Agents from /Library load into the user's domain, daemons into system.
plists=()
domains=()
collect() {
	local f
	for f in "$1"/*.plist; do
		{ [ -f "$f" ] && [ ! -L "$f" ]; } || continue
		plists[${#plists[@]}]=$f
		domains[${#domains[@]}]=$2
	done
}
collect "$HOME/Library/LaunchAgents" "$gui"
collect "$MC_ROOT/Library/LaunchAgents" "$gui"
collect "$MC_ROOT/Library/LaunchDaemons" system
[ "${#plists[@]}" -gt 0 ] || exit 0

# Every source must be readable: a missing one would show running or enabled
# items as stopped or unowned.
mc_ps_snapshot "$tmp/ps" || exit 1
mc_btm "$tmp/btm" || exit 1
checked=" "
i=0
while [ "$i" -lt "${#plists[@]}" ]; do
	d=${domains[$i]}
	i=$((i + 1))
	case $checked in
	*" $d "*) continue ;;
	esac
	checked="$checked$d "
	if ! mc_launchd_domain_ok "$d"; then
		echo "mac-cleanup: launchctl cannot print the domain $d" >&2
		exit 1
	fi
	mc_disabled_labels "$d" "$tmp/disabled.${d%%/*}" || exit 1
done

# The legacy BTM records (any user): decoded plist path, uid, disposition and
# parent identifier. Their URL is a file:// URL, percent-encoded.
while IFS=$_MC_US read -r r_uid r_type _ r_disp _ _ _ r_url _ _ r_parent _; do
	case $r_type in
	"legacy agent" | "legacy daemon") ;;
	*) continue ;;
	esac
	[ -n "$r_url" ] || continue
	printf '%s%s%s%s%s%s%s\n' "$(mc_url_path "$r_url")" "$_MC_US" "$r_uid" "$_MC_US" "$r_disp" "$_MC_US" "$r_parent"
done <"$tmp/btm" >"$tmp/legacy"

# Ids already used, so that one label in two folders gets two ids.
used=$'\n'
i=0
while [ "$i" -lt "${#plists[@]}" ]; do
	plist=${plists[$i]}
	domain=${domains[$i]}
	i=$((i + 1))

	# Without a readable Label the file name is only a guess at the label.
	guessed=0
	label=$(plutil -extract Label raw -o - "$plist" 2>/dev/null) || label=""
	if [ -z "$label" ]; then
		label=$(basename "$plist" .plist)
		guessed=1
	fi
	case $(printf '%s' "$label" | tr '[:upper:]' '[:lower:]') in
	com.apple.*) continue ;;
	esac
	program=$(plutil -extract Program raw -o - "$plist" 2>/dev/null) || program=""
	[ -n "$program" ] || program=$(plutil -extract ProgramArguments.0 raw -o - "$plist" 2>/dev/null) || program=""

	id="$domain/$label"
	case $used in
	*$'\n'"$id"$'\n'*) id="$id ($plist)" ;;
	esac
	used="$used$id"$'\n'

	# The plist's path as launchd and BTM name it.
	case $plist in
	"$HOME"/*) rel=$plist ;;
	*) rel=${plist#"$MC_ROOT"} ;;
	esac

	# State and memory. The domains were checked above, so a job that
	# launchctl cannot print is not loaded. A job that launchd loaded from
	# another plist with the same label is not this plist's: this one is
	# not loaded, and its label belongs to that other plist (foreign).
	# Without a path in launchctl's output it counts as this plist's.
	loaded=0
	foreign=0
	pid=""
	if job=$(mc_launchd_job "$domain/$label"); then
		rest=${job#*$'\t'}
		jpath=${rest#*$'\t'}
		if [ -z "$jpath" ] || [ "$jpath" = "$rel" ]; then
			loaded=1
			pid=${rest%%$'\t'*}
			case $pid in
			*[!0-9]*) pid="" ;;
			esac
		else
			foreign=1
		fi
	fi
	memory=0
	state=stopped
	if [ -n "$pid" ]; then
		state=running
		# Memory tie-break: a process is counted for the first plist that
		# claims it, in the order of the folders above (the user's agents,
		# then /Library agents, then daemons), so a job started by another
		# job counts for whichever of them comes first.
		memory=$(mc_tree_rss "$tmp/ps" "$tmp/claimed" "$pid")
	fi

	enabled=on
	if MC_L=$label awk '$0 == ENVIRON["MC_L"] { found = 1 } END { exit !found }' "$tmp/disabled.${domain%%/*}"; then
		enabled=off
	fi

	# The BTM record of this plist (the current user's first, else any),
	# then its parent record (the same user's as the record first, else
	# any).
	owner="" orphan="" p_type="" p_bundle="" p_team="" p_url=""
	rec=$(MC_P=$rel MC_UID=$uid awk -F"$_MC_US" '
		$1 == ENVIRON["MC_P"] {
			if ($2 == ENVIRON["MC_UID"]) { same = $0; exit }
			if (other == "") other = $0
		}
		END { print (same != "" ? same : other) }' "$tmp/legacy")
	if [ -n "$rec" ]; then
		IFS=$_MC_US read -r _ r_uid r_disp r_parent <<<"$rec"
		# Disposition: enabled/disabled, allowed/disallowed, notified.
		# Switched off in System Settings is disallowed.
		d1=${r_disp%%,*}
		d2=${r_disp#*,}
		d2=${d2# }
		d2=${d2%%,*}
		{ [ "$d1" = disabled ] || [ "$d2" = disallowed ]; } && enabled=off
		if [ -n "$r_parent" ]; then
			prec=$(MC_ID=$r_parent MC_UID=$r_uid awk -F"$_MC_US" '
				$7 == ENVIRON["MC_ID"] {
					if ($1 == ENVIRON["MC_UID"]) { same = $0; exit }
					if (other == "") other = $0
				}
				END { print (same != "" ? same : other) }' "$tmp/btm")
			if [ -n "$prec" ]; then
				IFS=$_MC_US read -r _ p_type _ _ p_name p_dev _ p_url _ p_bundle _ p_team <<<"$prec"
				owner=$p_name
				[ -n "$owner" ] || owner=$p_dev
				[ -n "$owner" ] || [ "$p_type" != developer ] || owner=$r_parent
			fi
		fi
	fi

	if mc_program_missing "$program"; then
		orphan="program missing"
	elif [ "$p_type" = app ] && [ -n "$p_bundle" ]; then
		# An app record whose bundle id and team no installed app claims
		# and whose bundle is not on disk.
		p_path=""
		if [ -n "$p_url" ]; then
			p_path=$(mc_url_path "$p_url")
			case $p_path in
			/*) ;;
			*) p_path="" ;;
			esac
		fi
		mc_app_deleted "$tmp" "$p_bundle" "$p_team" "$p_path"
		case $? in
		0) orphan="app deleted" ;;
		2) exit 1 ;;
		esac
	fi

	# Commands. Daemons run launchctl as root; agents never need it.
	sudo_=""
	if [ "$domain" = system ]; then
		sudo_="sudo "
	fi
	target=$(mc_service_target "$domain" "$label")
	boot="${sudo_}launchctl bootout $target"
	disable="${sudo_}launchctl disable $target"
	admin=$(mc_needs_admin "$plist")
	trash=$(mc_trash_cmd "$admin" "$plist")
	cmd_stop="" cmd_disable=""
	if [ "$loaded" = 1 ]; then
		cmd_stop=$boot
		cmd_disable="$boot && $disable"
		cmd_trash="$boot && $trash"
	else
		# A guessed label that is not loaded may not be the job's label
		# at all: disabling it could leave the real job running. A
		# foreign label is the other plist's: disabling it would keep
		# that job from starting too.
		[ "$enabled" = on ] && [ "$guessed" = 0 ] && [ "$foreign" = 0 ] && cmd_disable=$disable
		cmd_trash=$trash
	fi
	[ -n "$sudo_" ] && admin=1

	if [ "$orphan" = "program missing" ]; then
		default=trash
	elif [ -n "$cmd_disable" ]; then
		default=disable
	elif [ -n "$cmd_stop" ]; then
		default=stop
	else
		default=trash
	fi

	if [ "$domain" = system ]; then
		kind="Launch daemon"
		when="started at boot as root"
	else
		kind="Launch agent"
		when="started at every login"
	fi
	if [ -n "$owner" ]; then
		who="$kind of $owner"
	else
		who="$kind of an unknown owner"
	fi
	if [ -n "$cmd_disable" ]; then
		what="Disable stops it and keeps it from starting again; Trash also removes its plist."
	elif [ "$foreign" = 1 ]; then
		what="launchd runs this label from another plist, so Trash removes only this file."
	elif [ "$guessed" = 1 ] && [ "$loaded" = 0 ]; then
		what="Its plist can't be read, so the job may run under another name than the file's; Trash removes the plist."
	else
		what="It is turned off and not running; Trash removes its plist."
	fi

	mc_item
	mc_field id "$id"
	mc_field name "$label"
	mc_field category "Agents and daemons"
	mc_field tab autostart
	mc_field path "$plist"
	mc_field memory "${memory:-0}"
	mc_field state "$state"
	mc_field enabled "$enabled"
	[ -n "$owner" ] && mc_field owner "$owner"
	[ -n "$orphan" ] && mc_field orphan "$orphan"
	mc_field admin "$admin"
	[ -n "$cmd_stop" ] && mc_field cmd_stop "$cmd_stop"
	[ -n "$cmd_disable" ] && mc_field cmd_disable "$cmd_disable"
	mc_field cmd_trash "$cmd_trash"
	mc_field default "$default"
	mc_field explain "$who, $when. Program: ${program:-unknown}. $what"
	mc_end
done
exit 0
