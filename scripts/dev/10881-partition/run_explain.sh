#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Run explain_loads.sql against one rehearsal database and keep the raw plans.
# Usage: GIANT_SCOPE=id MID_SCOPE=id bash run_explain.sh DB divided(0|1) OUTFILE
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$here/common.sh"
db="$1"
divided="$2"
out="$3"
{
  echo "# database=$db divided=$divided host_load=$(cut -d' ' -f1-3 /proc/loadavg) $(date -u +%FT%TZ)"
  lp "$db" -q -v divided="$divided" -v giant_scope="${GIANT_SCOPE:?}" -v mid_scope="${MID_SCOPE:?}" -f - <"$here/explain_loads.sql"
} >"$out" 2>&1
echo "wrote $out ($(wc -l <"$out") lines)"
