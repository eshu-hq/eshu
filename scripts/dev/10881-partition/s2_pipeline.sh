#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# The #10881 larger-size run in one database that holds the undivided table
# (public.fact_records, retained index profile) loaded by load_pieces.sh.
# It drives migration_rehearsal.sh through expand, build, backfill and verify,
# captures the five-load plans on both shapes, rehearses cutover and way back
# with a simulated delta, then runs the removal arms in place (undivided first,
# then divided) so both shapes start from identical rows.
# Usage: GIANT_SCOPE=id MID_SCOPE=id bash s2_pipeline.sh DB
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$here/common.sh"
db="$1"
log="$WORKDIR/s2_pipeline.log"
step() {
  echo "# load=$(cut -d' ' -f1-3 /proc/loadavg) io_psi=$(awk '/some/{print $2}' /proc/pressure/io) $(date -u +%T) $*" | tee -a "$log"
  "$@" 2>&1 | tee -a "$log"
}
: >"$log"
lp "$db" -q -c "ANALYZE public.fact_records"
step bash "$here/migration_rehearsal.sh" "$db" expand
step bash "$here/migration_rehearsal.sh" "$db" build
step bash "$here/migration_rehearsal.sh" "$db" backfill
step bash "$here/migration_rehearsal.sh" "$db" verify
step bash "$here/migration_rehearsal.sh" "$db" mixed


# Simulated delta: rows written after the backfill, on the old table.
lp "$db" -q -c "INSERT INTO public.fact_records SELECT 'delta-' || fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind, fencing_token, source_confidence, source_system, source_fact_key, source_uri, source_record_id, observed_at, now(), is_tombstone, payload FROM public.fact_records WHERE scope_id = '$GIANT_SCOPE' LIMIT ${DELTA_ROWS:-50000}"
step bash "$here/migration_rehearsal.sh" "$db" cutover
# Writes that land on the new table before anyone rolls back.
lp "$db" -q -c "INSERT INTO public.fact_records SELECT 'post-' || fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind, fencing_token, source_confidence, source_system, source_fact_key, source_uri, source_record_id, observed_at, now(), is_tombstone, payload FROM public.fact_records WHERE scope_id = '$GIANT_SCOPE' LIMIT ${POST_ROWS:-20000}"
step bash "$here/migration_rehearsal.sh" "$db" wayback
step bash "$here/migration_rehearsal.sh" "$db" verify

# Removal arms, in place. Undivided first; the divided table has never seen a delete.
export CLONE=0 WORKDIR
SCHEMA=public step bash "$here/removal_bench.sh" "$db" drain_asis
SCHEMA=d step bash "$here/removal_bench.sh" "$db" drain_scoped

# Plans last: they are the cheapest and the arms have already shrunk both tables by the same rows.
LP_PGOPTIONS="-c search_path=public" step bash "$here/run_explain.sh" "$db" 0 "$WORKDIR/explain_${db}_undivided.txt"
LP_PGOPTIONS="-c search_path=d,public" step bash "$here/run_explain.sh" "$db" 1 "$WORKDIR/explain_${db}_divided.txt"
