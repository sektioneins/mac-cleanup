#!/bin/bash
# Shared source of the Autostart tab (the autostart- prefix): reads the
# Background Task Management records once per Autostart scan round and prints
# them in the line format of mc_btm_read. sfltool asks for the admin password,
# which is why the UI starts this round only when the tab is first opened. The
# scan passes the output to the targets in MC_SHARED_AUTOSTART_BTM, or the last
# stderr line in MC_SHARED_AUTOSTART_BTM_ERR when this fails. It only reads; it
# does not look at those variables itself, since it is their source.
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"
# shellcheck source=../autostart.sh
. "$(dirname "$0")/../autostart.sh"

mc_btm_read || exit 1
