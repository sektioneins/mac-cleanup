#!/bin/bash
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

dir="$HOME/Library/Application Support/Code/User/workspaceStorage"
[ -d "$dir" ] || exit 0
size=$(mc_size "$dir")
[ "$size" -gt 0 ] || exit 0

mc_item
mc_field id workspacestorage
mc_field name "VS Code workspace storage"
mc_field category Developer
mc_path_fields "$(mc_needs_admin "$dir")" "$dir"
mc_field grade regenerates
mc_field default delete
mc_warn_running 'Visual Studio Code.app/Contents/MacOS' 'VS Code'
mc_field explain "Per-project caches and state that VS Code keeps for folders you opened. VS Code recreates them when you open a project again, but unsaved editor state and some per-project extension data are lost."
mc_end
