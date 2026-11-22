#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Write the fixed removal list for an R1 run: superseded generation ids drawn
# only from scopes OTHER than the excluded giant scope(s), in generation_id
# order, until about FRACTION (default 0.20) of the table's rows are covered.
# The same file is passed to every cell, so all cells delete the same rows.
# Usage: bash pick_removal_list.sh DB OUTFILE GIANT_SCOPE [GIANT_SCOPE...]
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$here/common.sh"
db="$1"
out="$2"
shift 2
excl=$(printf "'%s'," "$@")
excl="${excl%,}"
lp "$db" -At -c "WITH gc AS (SELECT f.generation_id, count(*) n FROM fact_records f JOIN scope_generations g ON g.generation_id = f.generation_id WHERE g.status = 'superseded' AND g.scope_id NOT IN ($excl) GROUP BY 1), ordered AS (SELECT generation_id, n, sum(n) OVER (ORDER BY generation_id) cum FROM gc) SELECT generation_id FROM ordered WHERE cum - n < (SELECT sum(reltuples)::bigint FROM pg_class WHERE relname = 'fact_records' OR relname LIKE 'fact_records\_p%') * ${FRACTION:-0.20} ORDER BY generation_id" >"$out"
echo "removal list: $(wc -l <"$out") generations -> $out"
