#!/bin/bash
# Helpers for the autostart targets (bash 3.2 compatible).
# Source it after lib.sh with:
#   # shellcheck source=../autostart.sh
#   . "$(dirname "$0")/../autostart.sh"
# Everything here only reads (ps, launchctl print, sfltool dumpbtm); the tools
# are called by name so tests can put stubs first on PATH. The only files
# written are the ones a function is told to write.

# Field separator between the parts of a record (not whitespace, so empty
# fields survive `read`). installed.sh sets the same value.
_MC_US=${_MC_US:-$'\037'}

# mc_ps_snapshot FILE: write the process table to FILE, one line per process:
# pid, ppid, rss (KiB) and the command path (which can contain spaces).
mc_ps_snapshot() {
	if ! ps -axo pid=,ppid=,rss=,comm= >"$1" 2>/dev/null; then
		echo "mac-cleanup: could not list the running processes (ps failed)" >&2
		return 1
	fi
	if [ ! -s "$1" ]; then
		echo "mac-cleanup: could not list the running processes (ps printed nothing)" >&2
		return 1
	fi
}

# mc_tree_rss FILE CLAIMED PID...: print the bytes of resident memory of the
# PIDs and all their descendants (FILE is a snapshot from mc_ps_snapshot).
# Pids listed in the file CLAIMED are not counted again; every pid counted
# here is appended to CLAIMED, so callers that share it count each pid once.
# A claimed pid's descendants are still visited. A pid missing from the
# snapshot counts as nothing, and so does a root that is not a pid above 1.
# Loops in the parent links end the walk.
mc_tree_rss() {
	local snap=$1 claimed=$2
	shift 2
	: >>"$claimed"
	MC_CLAIMED=$claimed awk -v roots="$*" '
	BEGIN {
		claimed = ENVIRON["MC_CLAIMED"]
		while ((getline l < claimed) > 0) done[l + 0] = 1
		close(claimed)
	}
	{
		pid = $1 + 0
		rss[pid] = $3 + 0
		kids[$2 + 0] = kids[$2 + 0] " " pid
	}
	END {
		n = split(roots, start, " ")
		for (i = 1; i <= n; i++) {
			# Only real pids are roots: not 0, not launchd (1), not text.
			if (start[i] !~ /^[0-9]+$/ || start[i] + 0 <= 1) continue
			queue[++tail] = start[i] + 0
		}
		sum = 0
		while (head < tail) {
			p = queue[++head]
			if (p in seen) continue
			seen[p] = 1
			if (!(p in rss)) continue
			if (!(p in done)) {
				done[p] = 1
				sum += rss[p]
				print p >> claimed
			}
			m = split(kids[p], ch, " ")
			for (j = 1; j <= m; j++) queue[++tail] = ch[j] + 0
		}
		printf "%.0f\n", sum * 1024
	}' "$snap"
}

# mc_pids_matching FILE TEXT: print the pids whose command path contains TEXT
# (a literal string, such as /Example.app/).
mc_pids_matching() {
	MC_TEXT=$2 awk '
	{
		pid = $1
		sub(/^[ \t]*[0-9]+[ \t]+[0-9]+[ \t]+[0-9]+[ \t]+/, "")
		if (index($0, ENVIRON["MC_TEXT"]) > 0) print pid
	}' "$1"
}

# mc_launchd_job TARGET: for a TARGET like gui/501/label or system/label, print
# `state<TAB>pid` (the pid is empty when the job has none) from launchctl
# print. Only the job's own state and pid lines are read, not those of the
# blocks nested in it. Status 1 when the service is not loaded.
mc_launchd_job() {
	local out
	out=$(launchctl print "$1" 2>/dev/null) || return 1
	printf '%s\n' "$out" | awk '
	/^\tstate = / { if (state == "") state = substr($0, 10) }
	/^\tpid = / { if (pid == "") pid = substr($0, 8) }
	END { printf "%s\t%s\n", (state == "" ? "unknown" : state), pid }'
}

# mc_launchd_domain_ok DOMAIN: status 0 when launchctl can print DOMAIN
# (gui/501 or system).
mc_launchd_domain_ok() {
	launchctl print "$1" >/dev/null 2>&1
}

# mc_disabled_labels DOMAIN FILE: write the labels that launchctl
# print-disabled reports as disabled in DOMAIN to FILE, one per line. Status 1
# when launchctl fails.
mc_disabled_labels() {
	local out
	if ! out=$(launchctl print-disabled "$1" 2>/dev/null); then
		echo "mac-cleanup: could not read the disabled services of $1 (launchctl failed)" >&2
		return 1
	fi
	printf '%s\n' "$out" | awk '
	/^[ \t]*"[^"]*"[ \t]*=>[ \t]*disabled[ \t]*$/ {
		s = $0
		sub(/^[ \t]*"/, "", s)
		sub(/".*$/, "", s)
		print s
	}' >"$2"
}

# mc_btm FILE: write the Background Task Management records (sfltool dumpbtm)
# to FILE, one line per record with the fields below joined by $_MC_US:
#   uid type flags disposition name developer identifier url exepath bundleid
#   parentid teamid
# The uid comes from the section header (it can be negative); type is the word
# before ` (0x…)`; flags and disposition are the bracket contents without the
# spaces around them; `(null)` is an empty field. Identifiers keep their
# number prefix (see mc_btm_id) and URLs stay percent-encoded. All sections
# are written; callers filter by uid. Status 1 when sfltool fails or prints no
# section.
mc_btm() {
	local out
	if ! out=$(sfltool dumpbtm 2>/dev/null); then
		echo "mac-cleanup: could not read the background task records (sfltool dumpbtm failed)" >&2
		return 1
	fi
	case $out in
	*"Records for UID "*) ;;
	*)
		echo "mac-cleanup: sfltool dumpbtm printed no background task records" >&2
		return 1
		;;
	esac
	printf '%s\n' "$out" | awk -v US="$_MC_US" '
	function trim(s) { gsub(/^[ \t\r]+|[ \t\r]+$/, "", s); return s }
	function flush() {
		if (inrec)
			print uid US type US flags US disp US name US dev US id US url US exe US bundle US parent US team
		inrec = 0
		type = flags = disp = name = dev = id = url = exe = bundle = parent = team = ""
	}
	/^ Records for UID / { flush(); uid = $4; next }
	/^ #[0-9]+:[ \t\r]*$/ { flush(); inrec = 1; next }
	!inrec { next }
	# The list of embedded ids ("    #1: id") is not a record.
	/^[ \t]*#[0-9]+:/ { next }
	/^[ \t]+[A-Za-z][A-Za-z ]*:/ {
		i = index($0, ":")
		key = trim(substr($0, 1, i - 1))
		val = trim(substr($0, i + 1))
		if (val == "(null)") val = ""
		if (key == "Name") name = val
		else if (key == "Developer Name") dev = val
		else if (key == "Team Identifier") team = val
		else if (key == "Type") { sub(/ \(0x[0-9a-fA-F]+\)$/, "", val); type = val }
		else if (key == "Flags" || key == "Disposition") {
			sub(/^\[/, "", val)
			sub(/\].*$/, "", val)
			if (key == "Flags") flags = trim(val)
			else disp = trim(val)
		}
		else if (key == "Identifier") id = val
		else if (key == "URL") url = val
		else if (key == "Executable Path") exe = val
		else if (key == "Bundle Identifier") bundle = val
		else if (key == "Parent Identifier") parent = val
	}
	END { flush() }' >"$1"
}

# mc_btm_id ID: print ID without the number prefix of BTM identifiers
# (2.com.example.app becomes com.example.app).
mc_btm_id() {
	printf '%s\n' "$1" | awk '{ sub(/^[0-9]+\./, ""); print }'
}

# mc_url_path URL: print the path of a file:// URL with %XX decoded and the
# trailing / removed. A URL without file:// (the relative URLs of login items
# and agents) is decoded the same way.
mc_url_path() {
	local s=$1 bs=$'\\'
	case $s in
	file://localhost/*) s=${s#file://localhost} ;;
	file://*) s=${s#file://} ;;
	esac
	# printf %b decodes \xHH to the byte; literal backslashes are doubled first.
	s=${s//"$bs"/"$bs$bs"}
	s=${s//"%"/"${bs}x"}
	s=$(printf '%b' "$s")
	[ "${#s}" -gt 1 ] && s=${s%/}
	printf '%s\n' "$s"
}

# mc_as_quote STR: print STR as an AppleScript string literal.
mc_as_quote() {
	local s=$1 bs=$'\\' dq='"'
	s=${s//"$bs"/"$bs$bs"}
	s=${s//"$dq"/"$bs$dq"}
	printf '"%s"\n' "$s"
}

# mc_service_target DOMAIN LABEL: print DOMAIN/LABEL quoted for bash.
mc_service_target() {
	mc_quote "$1/$2"
}
