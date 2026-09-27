#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Scale evidence driver for the #7127 PR-3a changed-since link writer.

Builds the 1.0x fixture in a disposable PostgreSQL 18 container, then runs
TestLinkScaleEvidence (go/internal/storage/postgres/freshness/links) against
it while sampling the anonymous RSS of every Postgres backend in the
container every 0.1 s. It writes the combined result to
docs/internal/evidence/7127-link-writer-scale-results.json.

Schema: migrations 001, 002, 003 (table and columns only) and 133 from the
repository, applied as shipped; after the load only the two fact_records
indexes the link statement can use (fact_records_scope_generation_idx and
fact_records_scope_generation_keyset_idx, copied from migrations 003 and 099)
are built. The other 61 fact_records indexes are left out to keep the load
tractable; gate G3's plan-shape test runs on the full bootstrap instead.

Usage (from the repository root):
    docs/internal/evidence/7127-link-writer-scale.py --container NAME --port PORT [--skip-load] [--rounds N]
"""

import argparse
import json
import os
import re
import subprocess
import threading
import time

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
MIGRATIONS = os.path.join(ROOT, "go/internal/storage/postgres/migrations")
FIXTURE = os.path.join(ROOT, "docs/internal/evidence/7127-link-writer-fixture.sql")
RESULTS = os.path.join(ROOT, "docs/internal/evidence/7127-link-writer-scale-results.json")
DB = "cs7127_scale"
INDEXES = [
    "CREATE INDEX IF NOT EXISTS fact_records_scope_generation_idx"
    " ON fact_records (scope_id, generation_id, fact_kind, observed_at DESC)",
    "CREATE INDEX IF NOT EXISTS fact_records_scope_generation_keyset_idx"
    " ON fact_records (scope_id, generation_id, observed_at, fact_id)",
]
SAMPLER = r'''while :; do echo SWEEP; for p in /proc/[0-9]*; do c=$(tr '\0' ' ' < $p/cmdline 2>/dev/null); case "$c" in "postgres: postgres cs7127_scale"*) echo "${p#/proc/} $(awk '/^RssAnon/{print $2}' $p/status 2>/dev/null)";; esac; done; sleep 0.1; done'''


def psql(container, sql, db=DB):
    """Runs sql through psql in the container and fails on any error."""
    subprocess.run(["docker", "exec", "-i", container, "psql", "-v", "ON_ERROR_STOP=1", "-q", "-U", "postgres",
                    "-d", db], input=sql, text=True, check=True)


def table_only(path):
    """Returns a migration without its CREATE INDEX statements."""
    statements = [s.strip() for s in open(path).read().split(";")]
    keep = [s for s in statements if s and not re.match(r"(?is)^(--[^\n]*\n\s*)*CREATE\s+(UNIQUE\s+)?INDEX", s)]
    return ";\n".join(keep) + ";\n"


def load(container):
    psql(container, f"DROP DATABASE IF EXISTS {DB} WITH (FORCE);\nCREATE DATABASE {DB};\n", db="postgres")
    for name in ("001_ingestion_scopes.sql", "002_scope_generations.sql"):
        psql(container, open(os.path.join(MIGRATIONS, name)).read())
    psql(container, table_only(os.path.join(MIGRATIONS, "003_fact_records.sql")))
    psql(container, open(os.path.join(MIGRATIONS, "133_changed_since_link_ledger.sql")).read())
    started = time.time()
    psql(container, open(FIXTURE).read())
    loaded = time.time()
    psql(container, ";\n".join(INDEXES) + ";\nANALYZE;\n")
    return {"load_seconds": round(loaded - started, 1), "index_seconds": round(time.time() - loaded, 1)}


def sample(container, sweeps, stop):
    """Collects (time, {pid: RssAnon kB}) sweeps until stop is set."""
    proc = subprocess.Popen(["docker", "exec", container, "sh", "-c", SAMPLER], stdout=subprocess.PIPE, text=True)
    current = None
    for line in proc.stdout:
        if stop.is_set():
            break
        line = line.strip()
        if line == "SWEEP":
            if current is not None:
                sweeps.append(current)
            current = (time.time(), {})
        elif current is not None:
            parts = line.split()
            if len(parts) == 2 and parts[1].isdigit():
                current[1][parts[0]] = int(parts[1])
    proc.terminate()


def window_peak(sweeps, start, end):
    """Peak single-backend and summed RssAnon (kB) over backends above 20 MB."""
    single, total = 0, 0
    for ts, rss in sweeps:
        if start <= ts <= end:
            busy = [kb for kb in rss.values() if kb > 20000]
            single = max([single] + busy)
            total = max(total, sum(busy))
    return single, total


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--container", required=True)
    parser.add_argument("--port", required=True)
    parser.add_argument("--skip-load", action="store_true")
    parser.add_argument("--rounds", default="6")
    args = parser.parse_args()
    result = {"fixture": None if args.skip_load else load(args.container), "events": []}
    sweeps, stop = [], threading.Event()
    sampler = threading.Thread(target=sample, args=(args.container, sweeps, stop), daemon=True)
    sampler.start()
    env = dict(os.environ, ESHU_CHANGED_SINCE_LINK_SCALE_DSN=(
        f"postgres://postgres:pw@127.0.0.1:{args.port}/{DB}?sslmode=disable"),
        ESHU_CHANGED_SINCE_LINK_SCALE_ROUNDS=args.rounds)
    env.pop("GOROOT", None)
    test = subprocess.Popen(["go", "test", "./internal/storage/postgres/freshness/links", "-run",
                             "TestLinkScaleEvidence", "-count=1", "-v", "-timeout", "120m"],
                            cwd=os.path.join(ROOT, "go"), env=env, stdout=subprocess.PIPE, text=True)
    starts = {}
    for line in test.stdout:
        print(line, end="", flush=True)
        if not line.startswith("EVIDENCE "):
            continue
        event = json.loads(line[len("EVIDENCE "):])
        if event["event"] == "window_start":
            starts[event["n"]] = event["unix"]
        if event["event"] == "window_end":
            single, total = window_peak(sweeps, starts[event["n"]], event["unix"])
            event["peak_backend_rssanon_kb"] = single
            event["peak_summed_rssanon_kb"] = total
        result["events"].append(event)
    result["go_test_rc"] = test.wait()
    stop.set()
    json.dump(result, open(RESULTS, "w"), indent=1, sort_keys=True)
    open(RESULTS, "a").write("\n")
    print("wrote", RESULTS, "go test rc", result["go_test_rc"])
    return result["go_test_rc"]


if __name__ == "__main__":
    raise SystemExit(main())
