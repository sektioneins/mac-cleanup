#!/bin/bash
# Shared source: reads the Background Task Management records once per scan
# round and prints them in the line format of mc_btm_read. The scan passes the
# output to the targets in MC_SHARED_BTM, or the last stderr line in
# MC_SHARED_BTM_ERR when this fails. It only reads; it does not look at those
# variables itself, since it is their source.
set -u
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"
# shellcheck source=../autostart.sh
. "$(dirname "$0")/../autostart.sh"

mc_btm_read || exit 1
