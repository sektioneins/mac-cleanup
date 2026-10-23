#!/bin/bash
# Helper library for mac-cleanup target scripts (bash 3.2 compatible).
# Source it with: . "$(dirname "$0")/../lib.sh"
# Nothing here modifies the system; mutations exist only as command text.

# Targets prefix every system path with MC_ROOT so tests can redirect them.
MC_ROOT="${MC_ROOT:-}"

_MC_SQ="'"
_MC_BS=$'\\'

# mc_quote STR: print STR single-quoted for bash.
mc_quote() {
	printf "'%s'\n" "${1//$_MC_SQ/$_MC_SQ$_MC_BS$_MC_SQ$_MC_SQ}"
}

mc_item() { printf 'item\n'; }
mc_end() { printf 'end\n'; }

# mc_field KEY VALUE: print one record field with \, newline and TAB escaped.
mc_field() {
	local v=$2
	v=${v//$_MC_BS/$_MC_BS$_MC_BS}
	v=${v//$'\n'/${_MC_BS}n}
	v=${v//$'\t'/${_MC_BS}t}
	printf '%s\t%s\n' "$1" "$v"
}

# mc_size PATH...: total size in bytes; missing paths count 0.
mc_size() {
	local kb
	kb=$(du -sck "$@" 2>/dev/null | tail -n 1 | cut -f 1)
	echo $((${kb:-0} * 1024))
}

# mc_needs_admin PATH...: 1 if any path or its parent is not writable, else 0.
mc_needs_admin() {
	local p
	for p in "$@"; do
		if [ -e "$p" ] && [ ! -w "$p" ]; then
			echo 1
			return
		fi
		if [ ! -w "$(dirname "$p")" ]; then
			echo 1
			return
		fi
	done
	echo 0
}

# mc_quote_all PATH...: print the paths single-quoted, separated by spaces.
mc_quote_all() {
	local p out=""
	for p in "$@"; do
		out="$out $(mc_quote "$p")"
	done
	echo "${out# }"
}

# mc_rm_cmd ADMIN PATH...: command text that deletes the paths.
mc_rm_cmd() {
	local admin=$1 prefix=""
	shift
	[ "$admin" = 1 ] && prefix="sudo "
	echo "${prefix}rm -rf -- $(mc_quote_all "$@")"
}

# mc_trash_cmd ADMIN PATH...: command text that moves the paths to the Trash.
mc_trash_cmd() {
	local admin=$1 p out="" stamp
	shift
	if [ "$admin" != 1 ]; then
		echo "/usr/bin/trash $(mc_quote_all "$@")"
		return
	fi
	stamp=$(date +%Y%m%d-%H%M%S)
	for p in "$@"; do
		[ -n "$out" ] && out="$out && "
		out="${out}sudo mv -- $(mc_quote "$p") $(mc_quote "$HOME/.Trash/$(basename "$p") $stamp")"
	done
	echo "$out"
}

# mc_path_fields ADMIN PATH...: path, size, admin and both command fields.
mc_path_fields() {
	local admin=$1 p
	shift
	for p in "$@"; do
		mc_field path "$p"
	done
	mc_field size "$(mc_size "$@")"
	mc_field admin "$admin"
	mc_field cmd_delete "$(mc_rm_cmd "$admin" "$@")"
	mc_field cmd_trash "$(mc_trash_cmd "$admin" "$@")"
}

# mc_warn_running PATTERN LABEL: warn when a process matching PATTERN runs.
mc_warn_running() {
	local rc=0
	pgrep -f -- "$1" >/dev/null 2>&1 || rc=$?
	case $rc in
	0) mc_field warn "$2 is running: quit it first" ;;
	1) ;;
	*) mc_field warn "Could not check whether $2 is running" ;;
	esac
}
