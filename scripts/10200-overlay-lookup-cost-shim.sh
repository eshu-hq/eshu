#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# #10200 overlay-lookup-cost shim: theory-proof before any production change.
#
# Compares four shapes of `deltaOverlayTargetsQuery`
# (go/internal/storage/postgres/delta_overlay_query.go) on a 2,000-scope
# delta-active corpus with EXPLAIN (ANALYZE, BUFFERS) Execution Time p50s:
#   V0  shipped correlated jsonb_agg
#   V1  correlated array_agg piped through to_jsonb (identical JSON text)
#   V2  LATERAL join carrying jsonb_agg
#   V3  LATERAL join carrying array_agg piped through to_jsonb
#
# Method: 2 discarded warmup runs per variant, then 15 interleaved
# round-robin rounds (V0,V1,V2,V3 per round, 60 timed runs total) so a slow
# drift in background load cannot favor one variant. Row-set equivalence is
# proven with bidirectional EXCEPT before timing. Decision bar from the
# parent task: >=15% implement the winner, <10% close with evidence.
#
# Usage: bash scripts/10200-overlay-lookup-cost-shim.sh
# Re-runs are idempotent: the scratch database is dropped and recreated.
# The database is left in place after the run and named in stdout for reuse.
set -euo pipefail

DB="eshu_10200_shim"
PSQL="sudo -u postgres /usr/bin/psql"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }
load() { printf 'loadavg=%s active_pg=%s' "$(cat /proc/loadavg)" "$($PSQL -d "$DB" -t -A -c "SELECT count(*) FROM pg_stat_activity WHERE state='active' AND pid <> pg_backend_pid();")"; }

log "drop/create scratch database $DB (disposable; production stores untouched)"
$PSQL -d postgres -c "DROP DATABASE IF EXISTS $DB;" >/dev/null
$PSQL -d postgres -c "CREATE DATABASE $DB;" >/dev/null
log "start state: $(load)"

# Faithful minimal schema: the lookup touches only ingestion_scopes and
# scope_generations, so these two tables carry the production columns the
# query reads plus every production index on them (migrations 001, 002,
# 085, 091, 148, 210). Proof limit: no fact_records / work-item tables.
$PSQL -d "$DB" -v ON_ERROR_STOP=1 -q <<'EOF'
CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    scope_kind TEXT NOT NULL,
    source_system TEXT NOT NULL,
    source_key TEXT NOT NULL,
    parent_scope_id TEXT NULL,
    collector_kind TEXT NOT NULL,
    partition_key TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    active_generation_id TEXT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX ingestion_scopes_source_idx ON ingestion_scopes (source_system, scope_kind, partition_key, observed_at DESC);
CREATE INDEX ingestion_scopes_parent_idx ON ingestion_scopes (parent_scope_id, observed_at DESC);
CREATE UNIQUE INDEX ingestion_scopes_active_generation_idx ON ingestion_scopes (active_generation_id) WHERE active_generation_id IS NOT NULL;
CREATE TABLE scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    trigger_kind TEXT NOT NULL,
    freshness_hint TEXT NULL,
    source_commit_sha TEXT NULL,
    delta_baseline_commit_sha TEXT NULL,
    delta_union_base_commit_sha TEXT NULL,
    is_delta BOOLEAN NOT NULL DEFAULT false,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    activated_at TIMESTAMPTZ NULL,
    superseded_at TIMESTAMPTZ NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX scope_generations_scope_idx ON scope_generations (scope_id, status, ingested_at DESC);
CREATE INDEX scope_generations_scope_latest_lookup_idx ON scope_generations (scope_id, ingested_at DESC, generation_id DESC);
CREATE INDEX scope_generations_active_pending_activity_idx ON scope_generations (GREATEST(observed_at, ingested_at, COALESCE(activated_at, observed_at)) DESC) WHERE status IN ('pending', 'active');
CREATE UNIQUE INDEX scope_generations_active_scope_idx ON scope_generations (scope_id) WHERE status = 'active';
CREATE INDEX scope_generations_scope_generation_idx ON scope_generations (scope_id, generation_id) INCLUDE (ingested_at);
CREATE INDEX scope_generations_vulnerability_suppression_lineage_idx ON scope_generations (ingested_at DESC, generation_id DESC);
ALTER TABLE scope_generations ALTER COLUMN generation_id SET STATISTICS 1000;
ALTER TABLE scope_generations ALTER COLUMN scope_id SET STATISTICS 1000;
EOF

# Corpus: 2,000 delta-active scopes mirroring the #10094 guard shape (even
# claim scopes delta-active): one superseded full at c0 plus one active
# delta at c1 with baseline/union-base c0, so each scope has exactly one
# candidate base generation, as in the guard corpus.
$PSQL -d "$DB" -v ON_ERROR_STOP=1 -q <<'EOF'
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'scope-shim-' || lpad(g::text, 5, '0'), 'repository', 'git', 'k-shim', 'git', 'p1', now(), now(), 'active', 'gen-shim-' || lpad(g::text, 5, '0') || '-delta', '{}'::jsonb
FROM generate_series(0, 1999) AS g;
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, source_commit_sha, is_delta, observed_at, ingested_at, status, activated_at, payload)
SELECT 'gen-shim-' || lpad(g::text, 5, '0') || '-full', 'scope-shim-' || lpad(g::text, 5, '0'), 'sync', 'c0', false, now(), now(), 'superseded', now(), '{}'::jsonb
FROM generate_series(0, 1999) AS g;
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, source_commit_sha, is_delta, delta_baseline_commit_sha, delta_union_base_commit_sha, observed_at, ingested_at, status, activated_at, payload)
SELECT 'gen-shim-' || lpad(g::text, 5, '0') || '-delta', 'scope-shim-' || lpad(g::text, 5, '0'), 'sync', 'c1', true, 'c0', 'c0', now(), now(), 'active', now(), '{}'::jsonb
FROM generate_series(0, 1999) AS g;
ANALYZE ingestion_scopes;
ANALYZE scope_generations;
EOF
log "corpus: $($PSQL -d "$DB" -t -A -c "SELECT count(*) FROM ingestion_scopes;") scopes, $($PSQL -d "$DB" -t -A -c "SELECT count(*) FROM scope_generations;") generations"

OUTER_JOIN="FROM ingestion_scopes AS scope
JOIN scope_generations AS active
  ON active.scope_id = scope.scope_id
 AND active.generation_id = scope.active_generation_id"
OUTER_WHERE="WHERE active.is_delta = true
  AND active.status = 'active'"
OUTER_FROM="$OUTER_JOIN
$OUTER_WHERE"
BASE_PRED="base.scope_id = scope.scope_id
           AND base.is_delta = false
           AND base.activated_at IS NOT NULL
           AND base.source_commit_sha = COALESCE(NULLIF(TRIM(active.delta_union_base_commit_sha), ''), NULLIF(TRIM(active.delta_baseline_commit_sha), ''))"

cat > "$TMPDIR/v0.sql" <<EOF
SELECT scope.scope_id,
       active.generation_id,
       COALESCE(active.delta_baseline_commit_sha, ''),
       COALESCE(active.delta_union_base_commit_sha, ''),
       COALESCE((
         SELECT jsonb_agg(base.generation_id ORDER BY base.generation_id)
         FROM scope_generations AS base
         WHERE $BASE_PRED
       ), '[]'::jsonb)::text
$OUTER_FROM
ORDER BY scope.scope_id
EOF
cat > "$TMPDIR/v1.sql" <<EOF
SELECT scope.scope_id,
       active.generation_id,
       COALESCE(active.delta_baseline_commit_sha, ''),
       COALESCE(active.delta_union_base_commit_sha, ''),
       COALESCE((
         SELECT to_jsonb(array_agg(base.generation_id ORDER BY base.generation_id))
         FROM scope_generations AS base
         WHERE $BASE_PRED
       ), '[]'::jsonb)::text
$OUTER_FROM
ORDER BY scope.scope_id
EOF
cat > "$TMPDIR/v2.sql" <<EOF
SELECT scope.scope_id,
       active.generation_id,
       COALESCE(active.delta_baseline_commit_sha, ''),
       COALESCE(active.delta_union_base_commit_sha, ''),
       COALESCE(cand.candidates, '[]'::jsonb)::text
$OUTER_JOIN
LEFT JOIN LATERAL (
  SELECT jsonb_agg(base.generation_id ORDER BY base.generation_id) AS candidates
  FROM scope_generations AS base
  WHERE $BASE_PRED
) AS cand ON true
$OUTER_WHERE
ORDER BY scope.scope_id
EOF
cat > "$TMPDIR/v3.sql" <<EOF
SELECT scope.scope_id,
       active.generation_id,
       COALESCE(active.delta_baseline_commit_sha, ''),
       COALESCE(active.delta_union_base_commit_sha, ''),
       COALESCE(cand.candidates, '[]'::jsonb)::text
$OUTER_JOIN
LEFT JOIN LATERAL (
  SELECT to_jsonb(array_agg(base.generation_id ORDER BY base.generation_id)) AS candidates
  FROM scope_generations AS base
  WHERE $BASE_PRED
) AS cand ON true
$OUTER_WHERE
ORDER BY scope.scope_id
EOF

# Row-set equivalence: every variant must return the same 2,000 rows.
for v in v1 v2 v3; do
  d1=$($PSQL -d "$DB" -t -A -v ON_ERROR_STOP=1 -q \
    -c "SELECT count(*) FROM ((SELECT * FROM ($(cat "$TMPDIR/v0.sql")) AS a) EXCEPT (SELECT * FROM ($(cat "$TMPDIR/$v.sql")) AS b)) AS e1;")
  d2=$($PSQL -d "$DB" -t -A -v ON_ERROR_STOP=1 -q \
    -c "SELECT count(*) FROM ((SELECT * FROM ($(cat "$TMPDIR/$v.sql")) AS c) EXCEPT (SELECT * FROM ($(cat "$TMPDIR/v0.sql")) AS d)) AS e2;")
  diffs=$((d1 + d2))
  rows=$($PSQL -d "$DB" -t -A -q -c "SELECT count(*) FROM ($(cat "$TMPDIR/$v.sql")) AS t;")
  log "equivalence v0 vs $v: symmetric_diff=$diffs rows=$rows"
  if [ "$diffs" != "0" ] || [ "$rows" != "2000" ]; then log "EQUIVALENCE FAILED for $v"; exit 1; fi
done

explain_ms() {
  $PSQL -d "$DB" -t -A -q -c "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) $(cat "$TMPDIR/$1.sql")" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["Execution Time"])'
}

for v in v0 v1 v2 v3; do explain_ms "$v" >/dev/null; explain_ms "$v" >/dev/null; done
log "warmup done (2 discarded runs per variant)"

RESULTS="$TMPDIR/round-times.tsv"
: > "$RESULTS"
for round in $(seq 1 15); do
  for v in v0 v1 v2 v3; do
    ms="$(explain_ms "$v")"
    printf '%s\t%s\t%s\n' "$round" "$v" "$ms" >> "$RESULTS"
  done
  if [ $((round % 5)) -eq 0 ]; then log "round $round/15 done: $(load)"; fi
done

log "plan shape per variant (EXPLAIN, no ANALYZE):"
for v in v0 v1 v2 v3; do
  log "--- $v ---"
  $PSQL -d "$DB" -q -c "EXPLAIN $(cat "$TMPDIR/$v.sql")"
done

log "round times (round variant ms):"
cat "$RESULTS"
log "end state: $(load)"
python3 - "$RESULTS" <<'EOF'
import sys
from statistics import median
rows = [l.split() for l in open(sys.argv[1])]
by = {}
for r, v, ms in rows:
    by.setdefault(v, []).append(float(ms))
for v in sorted(by):
    s = sorted(by[v])
    print(f"{v}: n={len(s)} min={s[0]:.3f} p50={median(s):.3f} max={s[-1]:.3f} mean={sum(s)/len(s):.3f} ms | " + " ".join(f"{x:.3f}" for x in s))
best = min(by, key=lambda v: median(by[v]))
d = (median(by[best]) - median(by["v0"])) / median(by["v0"]) * 100
print(f"V-best={best} delta_vs_v0={d:.2f}%")
EOF
log "scratch database kept for reuse: $DB"
