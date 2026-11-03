# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Write-on-change winners resweep measurement shim for eshu-hq/eshu#9774.

Re-measures the FINAL shipped resweep SQL (rebuildSupplyChainImpactWinnersSQL)
against a disposable PostgreSQL 18.6, seeded with synthetic
reducer_supply_chain_impact_finding facts, and records row versions, WAL bytes
and exactness for the "before" (blanket upsert, what main did) and "after"
(write-on-change, shipped) statements over six ticks per phase.

Usage:
    uv run --with "psycopg[binary]" python 05-winners-write-on-change.py \
        postgres://eshu:change-me@127.0.0.1:15432/eshu?sslmode=disable \
        05-results.jsonl
"""

import json
import itertools
import os
import sys
import time

import psycopg

# supplyChainImpactWinnerDataColumns, in table order, from
# go/internal/storage/postgres/supply_chain_impact_canonical_winners_rebuild.go.
DATA_COLUMNS = [
    "winner_fact_id", "winner_scope_id", "finding_id", "priority_score",
    "source_count", "impact_status", "ecosystem", "severity_bucket", "repository_id",
    "cve_id", "advisory_id", "package_id", "subject_digest", "image_ref", "priority_bucket",
    "detection_profile", "observed_version", "match_reason", "suppression_state", "suppression_expires_at",
    "service_ids", "workload_ids", "environments",
]

HERE = os.path.dirname(os.path.abspath(__file__))

SCHEMA = """
CREATE TABLE IF NOT EXISTS ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    active_generation_id TEXT
);
CREATE TABLE IF NOT EXISTS scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT,
    status TEXT
);
CREATE TABLE IF NOT EXISTS fact_records (
    fact_id TEXT PRIMARY KEY,
    scope_id TEXT,
    generation_id TEXT,
    fact_kind TEXT,
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS shim_fact_kind_idx ON fact_records (fact_kind) WHERE is_tombstone = FALSE;
CREATE TABLE IF NOT EXISTS scanner_image_result_fact_classifications (
    finding_fact_id TEXT PRIMARY KEY,
    source_class TEXT,
    lifecycle TEXT
);
CREATE TABLE IF NOT EXISTS scanner_image_result_fact_associations (
    finding_fact_id TEXT PRIMARY KEY
);
-- migration 033_supply_chain_impact_canonical_winners.sql + 083 suppression_expires_at
CREATE TABLE IF NOT EXISTS supply_chain_impact_canonical_winners (
    canonical_key TEXT PRIMARY KEY,
    winner_fact_id TEXT NOT NULL,
    winner_scope_id TEXT NOT NULL DEFAULT '',
    finding_id TEXT NOT NULL,
    priority_score INTEGER NOT NULL DEFAULT 0,
    source_count INTEGER NOT NULL DEFAULT 1,
    impact_status TEXT NOT NULL DEFAULT '',
    ecosystem TEXT NOT NULL DEFAULT '',
    severity_bucket TEXT NOT NULL DEFAULT '',
    repository_id TEXT NOT NULL DEFAULT '',
    cve_id TEXT NOT NULL DEFAULT '',
    advisory_id TEXT NOT NULL DEFAULT '',
    package_id TEXT NOT NULL DEFAULT '',
    subject_digest TEXT NOT NULL DEFAULT '',
    image_ref TEXT NOT NULL DEFAULT '',
    priority_bucket TEXT NOT NULL DEFAULT '',
    detection_profile TEXT NOT NULL DEFAULT '',
    observed_version TEXT NOT NULL DEFAULT '',
    match_reason TEXT NOT NULL DEFAULT '',
    suppression_state TEXT NOT NULL DEFAULT 'active',
    suppression_expires_at TIMESTAMPTZ NULL,
    service_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    workload_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    environments JSONB NOT NULL DEFAULT '[]'::jsonb,
    materialized_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS shim_winners_finding_idx ON supply_chain_impact_canonical_winners (finding_id);
CREATE INDEX IF NOT EXISTS shim_winners_priority_idx ON supply_chain_impact_canonical_winners (priority_score DESC, finding_id);
CREATE INDEX IF NOT EXISTS shim_winners_status_idx ON supply_chain_impact_canonical_winners (impact_status, priority_score DESC, finding_id);
CREATE INDEX IF NOT EXISTS shim_winners_severity_idx ON supply_chain_impact_canonical_winners (severity_bucket, priority_score DESC, finding_id);
CREATE INDEX IF NOT EXISTS shim_winners_repository_idx ON supply_chain_impact_canonical_winners (repository_id, priority_score DESC, finding_id);
CREATE INDEX IF NOT EXISTS shim_winners_service_gin ON supply_chain_impact_canonical_winners USING GIN (service_ids);
CREATE INDEX IF NOT EXISTS shim_winners_workload_gin ON supply_chain_impact_canonical_winners USING GIN (workload_ids);
CREATE INDEX IF NOT EXISTS shim_winners_environment_gin ON supply_chain_impact_canonical_winners USING GIN (environments);
-- migration 034_supply_chain_impact_winners_materialization.sql
CREATE TABLE IF NOT EXISTS supply_chain_impact_winners_materialization (
    singleton SMALLINT PRIMARY KEY DEFAULT 1 CHECK (singleton = 1),
    materialized_at TIMESTAMPTZ NOT NULL
);
"""

TICKS = 6

# Monotonically increasing priority source for churn: every mutation moves a
# winner's priority_score to a fresh, strictly-higher value, so each churned
# tick is a real content change and none is swallowed by a repeated value.
_PRIORITY = itertools.count(5000)


def winner_select():
    with open(os.path.join(HERE, "05-winner-select.sql")) as fh:
        return fh.read()


def shipped_sql():
    with open(os.path.join(HERE, "05-winners-rebuild-shipped.sql")) as fh:
        return fh.read()


def main_sql():
    """The pre-#9774 blanket upsert: insert every winner every tick."""
    cols = ", ".join(DATA_COLUMNS)
    conflict = ",\n        ".join(f"{c} = EXCLUDED.{c}" for c in DATA_COLUMNS)
    conflict += ",\n        materialized_at = EXCLUDED.materialized_at"
    return f"""
WITH winners_now AS (
{winner_select().rstrip()}
),
upserted AS (
    INSERT INTO supply_chain_impact_canonical_winners (
        canonical_key, {cols}, materialized_at
    )
    SELECT
        canonical_key, {cols}, $1
    FROM winners_now
    ON CONFLICT (canonical_key) DO UPDATE SET
        {conflict}
),
deleted AS (
    DELETE FROM supply_chain_impact_canonical_winners w
    WHERE NOT EXISTS (SELECT 1 FROM winners_now n WHERE n.canonical_key = w.canonical_key)
)
INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at)
VALUES (1, $1)
ON CONFLICT (singleton) DO UPDATE SET materialized_at = EXCLUDED.materialized_at
"""


def seed(conn, winners, finding_filler=""):
    """Seed winners source facts with unique canonical keys (one per fact)."""
    with conn.cursor() as cur:
        cur.execute("INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES (%s, %s)",
                    ("vuln-intel://9774/source", "vuln-intel://9774/source/active"))
        cur.execute("INSERT INTO scope_generations (generation_id, scope_id, status) VALUES (%s, %s, %s)",
                    ("vuln-intel://9774/source/active", "vuln-intel://9774/source", "active"))
        now = "2026-11-01T00:00:00Z"
        rows = []
        for i in range(winners):
            payload = {
                "cve_id": f"CVE-2026-{i:05d}",
                "package_id": f"pkg:npm/pkg-{i}",
                "finding_id": f"finding:{i}",
                "impact_status": "affected_exact",
                "repository_id": "repo-shim",
                "ecosystem": "npm",
                "priority_score": str(10 + (i % 90)),
                "observed_version": "1.0.0",
                "service_ids": ["svc-a"],
                "workload_ids": ["wl-a"],
                "environments": ["prod"],
                "cvss_score": "7.5",
                "image_ref": "registry.example.com/app:1",
                "priority_bucket": "high",
                "detection_profile": "osv",
                "match_reason": "exact_version",
            }
            rows.append((f"fact-{i}", "vuln-intel://9774/source", "vuln-intel://9774/source/active",
                         "reducer_supply_chain_impact_finding", json.dumps(payload)))
        cur.executemany(
            "INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, payload) "
            "VALUES (%s, %s, %s, %s, %s::jsonb)", rows)
        cur.execute("ANALYZE fact_records")


def mutate(conn, changed):
    """Change `changed` finding priorities before a tick so their winners change."""
    with conn.cursor() as cur:
        for i in range(changed):
            fact_id = f"fact-{i}"
            priority = next(_PRIORITY)
            cur.execute(
                "UPDATE fact_records SET payload = jsonb_set(payload, '{priority_score}', %s::jsonb) "
                "WHERE fact_id = %s", (str(priority), fact_id))


def reset_phase(conn):
    with conn.cursor() as cur:
        cur.execute("TRUNCATE supply_chain_impact_canonical_winners, supply_chain_impact_winners_materialization")
        cur.execute("ALTER TABLE supply_chain_impact_canonical_winners SET (autovacuum_enabled = off)")
        cur.execute("VACUUM supply_chain_impact_canonical_winners")
        cur.execute("CHECKPOINT")
        cur.execute("SELECT pg_stat_reset()")


def run_tick(conn, sql, stamp):
    # The shipped SQL is written with Postgres-native $1 placeholders (it is
    # dumped verbatim from the Go constant); psycopg binds %(name)s, so map it
    # here. Escape the LIKE '%' literal first, then swap the placeholder, so the
    # named placeholder we introduce is not itself re-escaped. A named
    # placeholder is reused everywhere $1 appeared, matching Postgres semantics.
    sql = sql.replace("%", "%%").replace("$1", "%(stamp)s")
    with conn.cursor() as cur:
        started = time.monotonic()
        cur.execute(sql, {"stamp": stamp})
        elapsed = time.monotonic() - started
    return elapsed


def snapshot_xmin(conn):
    with conn.cursor() as cur:
        cur.execute("SELECT canonical_key, xmin::text FROM supply_chain_impact_canonical_winners")
        return dict(cur.fetchall())


def versions_written(conn, before):
    """Rows a tick versioned: changed xmin (update) plus new and removed keys."""
    after = snapshot_xmin(conn)
    changed = 0
    for key in after:
        if before.get(key) != after[key]:
            changed += 1
    for key in before:
        if key not in after:
            changed += 1
    return changed


def wal_delta_bytes(conn, before_lsn):
    # pg_stat_wal is cluster-wide and pg_stat_reset() does not zero it, so
    # measure WAL with the insert-LSN delta over the measured window instead.
    with conn.cursor() as cur:
        cur.execute("SELECT pg_wal_lsn_diff(pg_current_wal_insert_lsn(), %s)", (before_lsn,))
        (delta,) = cur.fetchone()
    return int(delta)


def winner_content(conn):
    with conn.cursor() as cur:
        cur.execute(
            "SELECT canonical_key, (to_jsonb(w) - 'materialized_at')::text "
            "FROM supply_chain_impact_canonical_winners w ORDER BY canonical_key")
        return cur.fetchall()


def run_phase(conn, sql, winners, changed_per_tick):
    reset_phase(conn)
    # Pre-populate the read model with one unmeasured resweep, so the measured
    # ticks are steady-state (an unchanged tick then writes zero row versions,
    # matching the shipped semantics under a already-populated table).
    run_tick(conn, sql, "2026-11-01T11:59:59Z")
    with conn.cursor() as cur:
        cur.execute("CHECKPOINT")
    # Warmup tick absorbs the full-page-write spike that the first UPDATE of
    # each page after a checkpoint incurs, so it is not charged to the window.
    run_tick(conn, sql, "2026-11-01T11:59:58Z")
    with conn.cursor() as cur:
        cur.execute("CHECKPOINT")
        cur.execute("SELECT pg_current_wal_insert_lsn()")
        (before_lsn,) = cur.fetchone()
    ticks = []
    versions = 0
    for t in range(TICKS):
        if changed_per_tick:
            mutate(conn, changed_per_tick)
        stamp = f"2026-11-01T12:00:{t:02d}Z"
        before = snapshot_xmin(conn)
        ms = run_tick(conn, sql, stamp)
        versions += versions_written(conn, before)
        ticks.append(ms)
    wal = wal_delta_bytes(conn, before_lsn)
    before = winner_content(conn)
    # Exactness: wipe and rebuild from scratch, compare content.
    with conn.cursor() as cur:
        cur.execute("DELETE FROM supply_chain_impact_canonical_winners")
    run_tick(conn, sql, "2026-11-01T12:00:59Z")
    fresh = winner_content(conn)
    differ = 0
    for a, b in zip(before, fresh):
        if a != b:
            differ += 1
    return {
        "row_versions": versions,
        "wal_bytes": wal,
        "wal_mb": round(wal / (1024 * 1024), 3),
        "winners": len(before),
        "exactness_rows_differ": differ,
        "tick_ms": [round(x * 1000, 1) for x in ticks],
    }


def main():
    dsn = sys.argv[1]
    out_path = sys.argv[2] if len(sys.argv) > 2 else os.path.join(HERE, "05-results.jsonl")
    winners = int(os.environ.get("SHIM_WINNERS", "29870"))
    changed = int(os.environ.get("SHIM_CHANGED", "30"))
    results = {"postgres": "18.6", "winners": winners, "changed_per_tick": changed, "ticks": TICKS,
               "phases": {}}
    with psycopg.connect(dsn, autocommit=True) as conn:
        with conn.cursor() as cur:
            cur.execute("DROP TABLE IF EXISTS supply_chain_impact_canonical_winners, "
                        "supply_chain_impact_winners_materialization, fact_records, "
                        "ingestion_scopes, scope_generations, "
                        "scanner_image_result_fact_classifications, "
                        "scanner_image_result_fact_associations CASCADE")
            cur.execute(SCHEMA)
        seed(conn, winners)
        results["phases"]["main_nothing_changed"] = run_phase(conn, main_sql(), winners, 0)
        results["phases"]["main_30_changed"] = run_phase(conn, main_sql(), winners, changed)
        results["phases"]["shipped_nothing_changed"] = run_phase(conn, shipped_sql(), winners, 0)
        results["phases"]["shipped_30_changed"] = run_phase(conn, shipped_sql(), winners, changed)
    with open(out_path, "w") as fh:
        json.dump(results, fh, indent=2)
        fh.write("\n")
    print(json.dumps(results, indent=2))


if __name__ == "__main__":
    main()
