#!/usr/bin/env bash
# usage: row_equality.sh <dsn> [plain]
# Expands the precise predicate (build_v_reads.py) into row_equality.sql and runs it. With
# "plain" it first replaces the ordered index by the plain (canonical_key) partial index, so the
# probe_ordered rows are the plain-index result; restore with v_indexes.sql afterwards.
set -euo pipefail
dsn="$1"; mode="${2:-ordered}"; here="$(cd "$(dirname "$0")" && pwd)"
if [ "$mode" = plain ]; then
  pred="$(python3 -c "import sys; sys.path.insert(0,'$here'); import build_v_reads as b; print(b.precise(''))")"
  psql "$dsn" -X -q -c "DROP INDEX IF EXISTS supply_chain_impact_finding_supports_precise_key_ordered_idx" \
    -c "CREATE INDEX IF NOT EXISTS supply_chain_impact_finding_supports_precise_key_idx ON supply_chain_impact_finding_supports (canonical_key) WHERE $pred" \
    -c "ANALYZE supply_chain_impact_finding_supports"
fi
python3 - "$here" "$mode" >/tmp/b2a_row_equality.sql <<'PY'
import sys
sys.path.insert(0, sys.argv[1])
import build_v_reads as b
t = open(sys.argv[1] + "/row_equality.sql").read()
if sys.argv[2] == "plain":
    # the plain run compares its probe with the saved ordered result and does not overwrite it
    t = t.split("CREATE TEMP TABLE probe_join AS")[0]
    t += open(sys.argv[1] + "/row_equality_plain.sql").read()
print(t.replace(":PRECISE_S0", b.precise("s0")).replace(":PRECISE_S", b.precise("s")))
PY
psql "$dsn" -X -f /tmp/b2a_row_equality.sql
