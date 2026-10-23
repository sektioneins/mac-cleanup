#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"
# shellcheck source=../installed.sh
. "$(dirname "$0")/../installed.sh"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/mc-libunused.XXXXXX") || exit 1
trap 'rm -rf "$tmp"' EXIT
mc_inventory "$tmp" || exit 1

lib="$HOME/Library"
[ -d "$lib" ] || exit 0

# macOS's own folders directly in ~/Library.
standard=(Accessibility Accounts "Address Book Plug-Ins" AppleMediaServices "Application Scripts" "Application Support" Assistant Assistants Audio "Autosave Information" Biome Caches Calendars CallServices CloudStorage ColorPickers Colors ColorSync Components Compositions Contacts ContainerManager Containers Cookies CoreData CoreFollowUp "Daemon Containers" DataAccess DataDeliveryServices DES Developer Dictionaries Documentation DoNotDisturb DuetExpertCenter Family Favorites FileProvider Finance FinanceBackup FontCollections Fonts Frameworks FrontBoard GameKit "Group Containers" HomeKit HTTPStorages Icons IdentityServices Images "Input Methods" IntelligencePlatform Intents "Internet Plug-Ins" Keyboard "Keyboard Layouts" KeyboardServices Keychains LanguageModeling LaunchAgents LockdownMode Logs Mail "Mail Downloads" Maps MediaAnalysis Messages Metadata "Mobile Documents" MobileDevice News Passes "PDF Services" PersonalizationPortrait Personas Phones Photos PreferencePanes Preferences Printers PrivateCloudCompute PubSub Receipts "Recent Servers" Reminders ResponseKit Safari SafariSafeBrowsing SafariSandboxBroker "Saved Application State" "Screen Savers" Scripts Services Sharing Shortcuts Sounds Speech Spelling Spotlight Staging StatusKit StickiesDatabase Suggestions SyncedPreferences Translation Trial UIKitSystem UnifiedAssetFramework Voices Weather WebKit Workflows)
# macOS services without a matching launchd plist.
apple_fixed=(MobileSync CoreParsec ConfigurationProfiles DiskImages "Quick Look" TrustedPeersHelper AskPermission)

printf '%s\n' "${standard[@]}" >"$tmp/standard"
printf '%s\n' "${apple_fixed[@]}" >"$tmp/apple_fixed"

# Text of the sibling targets, to see which folders they already handle.
self=${0##*/}
: >"$tmp/siblings"
for f in "$(dirname "$0")"/*.sh; do
	case ${f##*/} in
	"$self" | app-leftovers.sh) continue ;;
	esac
	cat "$f" >>"$tmp/siblings"
done

# Candidates: kind TAB name TAB path.
for p in "$lib/Application Support"/* "$lib"/*; do
	[ -d "$p" ] && [ ! -L "$p" ] || continue
	name=${p##*/}
	# Names with a tab or newline would corrupt the TAB separated list.
	case $name in
	*$'\t'* | *$'\n'*) continue ;;
	esac
	mc_is_bundle_id "$name" && continue
	case $p in
	"$lib/Application Support"/*) kind=as ;;
	*) kind=lib ;;
	esac
	printf '%s\t%s\t%s\n' "$kind" "$name" "$p"
done >"$tmp/cand"

# One pass over all candidates against every exclusion.
awk -F'\t' -v tmp="$tmp" '
function load(file, set,   l) {
	while ((getline l < file) > 0) set[tolower(l)] = 1
	close(file)
}
BEGIN {
	load(tmp "/names", names); load(tmp "/vendors", names); load(tmp "/apple", names)
	load(tmp "/apple_fixed", names); load(tmp "/standard", std)
	while ((getline l < (tmp "/siblings")) > 0) text = text l "\n"
}
function covered(rel,   pre, i, c) {
	pre = "Library/" rel
	for (i = index(text, pre); i > 0; i = (c = index(substr(text, i + 1), pre)) ? i + c : 0) {
		c = substr(text, i + length(pre), 1)
		if (c == "/" || c == "\"" || c == "\047") return 1
	}
	return 0
}
{
	l = tolower($2)
	if (l in names) next
	if ($1 == "lib" && (l in std)) next
	if (covered($1 == "as" ? "Application Support/" $2 : $2)) next
	print
}' "$tmp/cand" >"$tmp/keep"

while IFS=$'\t' read -r kind name path; do
	mc_quiet 365 "$path" || continue
	newest=$(mc_newest_mtime "$path")
	[ -n "$newest" ] || continue
	when=$(date -j -r "$newest" '+%b %Y') || continue
	# shellcheck disable=SC2088
	if [ "$kind" = as ]; then
		where="~/Library/Application Support"
	else
		where="~/Library"
	fi
	fields=$(mc_path_fields "$(mc_needs_admin "$path")" "$path")
	size=$(printf '%s\n' "$fields" | awk -F'\t' '$1 == "size" { print $2 }')
	[ "${size:-0}" -gt 0 ] || continue

	mc_item
	mc_field id "$kind/$name"
	mc_field name "$name (last changed $when)"
	mc_field category Leftovers
	# shellcheck disable=SC2088
	mc_field view "~/Library leftovers"
	printf '%s\n' "$fields"
	mc_field grade mayneed
	mc_field default trash
	mc_field explain "The folder $name in $where. Not opened for over a year, last changed $when, and no installed app has this name. It does not come back by itself."
	mc_end
done <"$tmp/keep"
