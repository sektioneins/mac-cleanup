#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"
# shellcheck source=../installed.sh
. "$(dirname "$0")/../installed.sh"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/mc-leftovers.XXXXXX") || exit 1
trap 'rm -rf "$tmp"' EXIT
mc_inventory "$tmp" || exit 1

lib="$HOME/Library"
[ -d "$lib" ] || exit 0

team_re='^[A-Z0-9]{10}\.'

# The first two labels (vendor) of every installed id, lowercase.
awk -F. 'NF >= 2 { print $1 "." $2 }' "$tmp/ids" | sort -u >"$tmp/vendorpairs"

# add ID PATH [TEAM]: print a candidate line unless it can't be a deleted
# app's data.
add() {
	local id=$1 path=$2 team=${3:-} lid first rest
	# A tab or newline would corrupt the TAB separated lists.
	case $path in
	*$'\t'* | *$'\n'*) return 0 ;;
	esac
	mc_is_bundle_id "$id" || return 0
	lid=$(printf '%s' "$id" | tr '[:upper:]' '[:lower:]')
	case $lid in
	com.apple.* | is.workflow.*) return 0 ;;
	esac
	# A vendor with an app whose nested bundles could not be checked.
	if [ -s "$tmp/vendorclaim" ]; then
		first=${lid%%.*}
		rest=${lid#*.}
		grep -qxF -- "$first.${rest%%.*}" "$tmp/vendorclaim" && return 0
	fi
	if [ -n "$team" ] && grep -qx -- "$team" "$tmp/teams"; then
		return 0
	fi
	printf '%s\t%s\n' "$id" "$path"
}

candidates() {
	local p name id team pair
	for p in "$lib"/Containers/* "$lib"/WebKit/* "$lib"/Application\ Support/*; do
		[ -d "$p" ] && [ ! -L "$p" ] && add "${p##*/}" "$p"
	done
	for p in "$lib/Group Containers"/*; do
		[ -d "$p" ] && [ ! -L "$p" ] || continue
		id=${p##*/}
		mc_is_bundle_id "$id" || continue
		# An application-groups entitlement of an installed app claims the
		# whole folder name.
		grep -qixF -- "$id" "$tmp/groups" && continue
		team=""
		while :; do
			if [[ $id =~ $team_re ]]; then
				team=${id%%.*}
				id=${id#*.}
			elif [[ $id == group.* ]]; then
				id=${id#group.}
			elif [[ $id == groups.* ]]; then
				id=${id#groups.}
			else
				break
			fi
		done
		# Without a team prefix, a group of an installed app's vendor is
		# claimed.
		if [ -z "$team" ]; then
			pair=$(printf '%s' "$id" | awk -F. '{ print tolower($1 "." $2) }')
			grep -qxF -- "$pair" "$tmp/vendorpairs" && continue
		fi
		add "$id" "$p" "$team"
	done
	for p in "$lib"/Preferences/*.plist; do
		[ -f "$p" ] && [ ! -L "$p" ] || continue
		name=${p##*/}
		add "${name%.plist}" "$p"
	done
	for p in "$lib"/Preferences/ByHost/*.plist; do
		[ -f "$p" ] && [ ! -L "$p" ] || continue
		name=${p##*/}
		name=${name%.plist}
		# Both the file name and the id before the host part must be ids.
		mc_is_bundle_id "$name" || continue
		add "${name%.*}" "$p"
	done
	for p in "$lib"/Saved\ Application\ State/*.savedState; do
		[ -d "$p" ] && [ ! -L "$p" ] || continue
		name=${p##*/}
		add "${name%.savedState}" "$p"
	done
	for p in "$lib"/HTTPStorages/*; do
		[ ! -L "$p" ] || continue
		name=${p##*/}
		if [ -d "$p" ]; then
			add "$name" "$p"
		elif [ -f "$p" ] && [ "${name%.binarycookies}" != "$name" ]; then
			add "${name%.binarycookies}" "$p"
		fi
	done
}

candidates >"$tmp/cand"
mc_unclaimed "$tmp" <"$tmp/cand" >"$tmp/unclaimed" || exit 1

# One line per group: root id, the ids (space separated), then the paths, all
# TAB separated. Shorter ids come first, so they become the roots.
awk -F'\t' '{ print length($1) "\t" $0 }' "$tmp/unclaimed" | sort -n -s |
	cut -f 2- |
	awk -F'\t' '
	{
		lid = tolower($1)
		root = ""
		for (i = 1; i <= n; i++)
			if (lid == roots[i] || index(lid, roots[i] ".") == 1) { root = roots[i]; break }
		if (root == "") { roots[++n] = lid; root = lid; ids[root] = "" ; paths[root] = "" }
		ids[root] = ids[root] (ids[root] == "" ? "" : " ") $1
		paths[root] = paths[root] "\t" $2
	}
	END { for (i = 1; i <= n; i++) print roots[i] "\t" ids[roots[i]] paths[roots[i]] }
	' >"$tmp/rootgroups"

while IFS=$'\t' read -r -a f; do
	root=${f[0]}
	ids=${f[1]}
	paths=("${f[@]:2}")
	mc_quiet 90 "${paths[@]}" || continue
	[ "$(mc_size "${paths[@]}")" -gt 0 ] || continue
	display=$(awk -F'\t' -v id="$root" '$1 == id { print $2; exit }' "$tmp/stale")
	[ -n "$display" ] || display=$root

	mc_item
	mc_field id "$root"
	mc_field name "$display leftovers"
	mc_field category Leftovers
	# shellcheck disable=SC2088
	mc_field view "~/Library leftovers"
	mc_path_fields "$(mc_needs_admin "${paths[@]}")" "${paths[@]}"
	# shellcheck disable=SC2086
	mc_warn_bundles "$display" $ids
	mc_field grade mayneed
	mc_field default trash
	mc_field explain "Data in ~/Library under the id $root, which no installed app claims. Nothing in it was read or changed in the last 90 days. It does not come back, and without the app it is not needed."
	mc_end
done <"$tmp/rootgroups"
