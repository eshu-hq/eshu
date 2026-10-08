#!/usr/bin/env bash
# #7637 theory shim runner. Seeds 900x25, then counts + EXPLAINs old vs floored
# listings. Shared fragments are extracted mechanically from Go source; only
# the one-line domain predicate is emitted (printf).
set -euo pipefail
# Usage: PGHOST=... PGPORT=... PGUSER=... PGPASSWORD=... PGDATABASE=... bash 7637-reopen-bound-shim.sh [repo-root] [old-ref]
# The OLD listing text is extracted from old-ref (default: the pre-fix base),
# the NEW text from the working tree, so the comparison re-runs after the fix.
# Seeds a 900x25 store in schema shim7637 of the target database and writes
# counts + EXPLAIN (ANALYZE, BUFFERS) for the old vs floored listings to stdout.
REPO_ROOT="${1:-$(git rev-parse --show-toplevel)}"
OLD_REF="${2:-5fcc12a5f3584cb47f485cf45027dada17efac8e}"
SEED="$(cd "$(dirname "$0")" && pwd)/7637-reopen-bound-seed.sql"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export PGHOST="${PGHOST:-localhost}" PGPORT="${PGPORT:-5432}" PGUSER="${PGUSER:-eshu}" PGPASSWORD="${PGPASSWORD:-change-me}" PGDATABASE="${PGDATABASE:-eshu}"

extract_const() { # $1=file $2=constname $3=outfile
  awk -v name="$2" '
    $0 ~ "^const "name" = `" {capture=1; sub("^const "name" = `", ""); if (length($0) > 0) print; next}
    capture && /^`$/ {capture=0; next}
    capture {print}
  ' "$1" > "$3"
}

git -C "$REPO_ROOT" show "$OLD_REF:go/internal/storage/postgres/ingestion_queries.go" | extract_const /dev/stdin \
  listSucceededDeploymentMappingWorkItemsQuery $TMP/old-dm.sql
# Floored shape = the shared scopeReplayFloor fragments reassembled exactly as
# the relationship query const composes them (CTE + refs select + joins +
# domain line + bound). Every shared fragment is extracted; only the one-line
# domain predicate below is emitted (printf), so a Go change to that line
# needs a matching shim update.
CORR="$REPO_ROOT/go/internal/storage/postgres/ingestion_reopen_correlation.go"
extract_const "$CORR" scopeReplayFloorCTE $TMP/frag-cte.sql
extract_const "$CORR" scopeReplayFloorSelectWorkItemRefs $TMP/frag-select.sql
extract_const "$CORR" scopeReplayFloorJoinsAndStage $TMP/frag-joins.sql
extract_const "$CORR" scopeReplayFloorBound $TMP/frag-bound.sql
{ cat $TMP/frag-cte.sql $TMP/frag-select.sql $TMP/frag-joins.sql
  printf "  AND work.domain = 'deployment_mapping'\n"
  cat $TMP/frag-bound.sql
} > $TMP/new-dm.sql

psql -v ON_ERROR_STOP=1 -q <<'EOF'
DROP SCHEMA IF EXISTS shim7637 CASCADE;
CREATE SCHEMA shim7637;
SET search_path TO shim7637;
CREATE TABLE ingestion_scopes(scope_id TEXT PRIMARY KEY, active_generation_id TEXT NULL);
CREATE TABLE scope_generations(generation_id TEXT PRIMARY KEY, scope_id TEXT NOT NULL, ingested_at TIMESTAMPTZ NOT NULL, status TEXT NOT NULL DEFAULT 'pending');
CREATE TABLE fact_work_items(work_item_id TEXT PRIMARY KEY, scope_id TEXT NOT NULL, generation_id TEXT NOT NULL, stage TEXT NOT NULL, domain TEXT NOT NULL, status TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL);
-- Production queue index shape (stage, domain, status, ... updated_at DESC).
CREATE INDEX fact_work_items_stage_domain_status_idx ON fact_work_items (stage, domain, status, updated_at DESC);
CREATE INDEX scope_generations_scope_latest_idx ON scope_generations (scope_id, ingested_at DESC, generation_id DESC);
EOF

psql -v ON_ERROR_STOP=1 -q -f "$SEED"

run_q() { # $1=label $2=file $3=explain(0/1)
  echo "===== $1 ====="
  {
    if [ "$3" = 1 ]; then
      echo -n "EXPLAIN (ANALYZE, BUFFERS) "
      cat "$2"
      echo ";"
    else
      echo -n "SELECT count(*) AS ${1%% *}_rows FROM ("
      cat "$2" | tr '\n' ' '
      echo ") AS q;"
    fi
  } > $TMP/run.sql
  psql -v ON_ERROR_STOP=1 -q -c "SET search_path TO shim7637" -f $TMP/run.sql
}

run_q old_dm_count $TMP/old-dm.sql 0
run_q new_dm_count $TMP/new-dm.sql 0
run_q old_dm_plan $TMP/old-dm.sql 1
run_q new_dm_plan $TMP/new-dm.sql 1
# Second EXPLAIN of each arm back-to-back (same-host A/B; wall is noisy here).
run_q old_dm_plan_r2 $TMP/old-dm.sql 1
run_q new_dm_plan_r2 $TMP/new-dm.sql 1
echo "shim7637 complete" >&2
