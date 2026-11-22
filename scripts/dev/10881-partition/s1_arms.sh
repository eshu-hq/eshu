#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# The #10881 removal arms for one size, undivided and divided back to back so
# host-load drift hits both. Output: one line per arm in $WORKDIR/arms_$TAG.log.
# Usage: TAG=s1 bash s1_arms.sh UND_DB DIV_DB
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$here/common.sh"
und="$1"
div="$2"
log="$WORKDIR/arms_${TAG:-s1}.log"
: >"$log"
run() { echo "# load=$(cut -d' ' -f1-3 /proc/loadavg) $(date -u +%T)" | tee -a "$log"; "$@" 2>&1 | tee -a "$log"; }
# Rows of the scopes in the last partition of the divided table's smallest-scope slice
# are chosen by the caller in $SCOPE_FILE (scopes whose rows all live in one partition).
run bash "$here/removal_bench.sh" "$und" drain_asis
run bash "$here/removal_bench.sh" "$div" drain_scoped
NO_VACUUM=1 run bash "$here/removal_bench.sh" "$div" drain_asis
NO_VACUUM=1 run bash "$here/removal_bench.sh" "$und" cascade
NO_VACUUM=1 run bash "$here/removal_bench.sh" "$div" cascade
if [ -n "${SCOPE_FILE:-}" ]; then
  NO_VACUUM=1 run bash "$here/removal_bench.sh" "$und" scope_delete
  NO_VACUUM=1 run bash "$here/removal_bench.sh" "$div" scope_delete
  NO_VACUUM=1 run bash "$here/removal_bench.sh" "$div" truncate
fi
