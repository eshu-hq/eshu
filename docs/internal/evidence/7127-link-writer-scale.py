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
    docs/internal/evidence/7127-link-writer-scale.py --container NAME --port PORT [--start-container]
        [--skip-load] [--rounds N] [--g7-only --valid-rounds 10 --deadline 8h]

The test binary is built once at start (go test -c), so later edits to the
tree cannot change what runs. --g7-only runs gate G7 alone and waits out a
loaded host: a round counts only when the host's 1-minute load at its start
is below the CPU count, and the run ends at --valid-rounds valid rounds or
after --deadline. That mode is meant to run unattended, e.g. overnight:

    docs/internal/evidence/7127-link-writer-scale.py --container eshu-7127-g7 --port 25472 \
        --start-container --g7-only --valid-rounds 10 --deadline 8h

Results go to 7127-link-writer-scale-results.json (7127-link-writer-g7-results.json
with --g7-only). Remove the container afterwards: docker rm -f -v NAME.
"""

import argparse
import json
import os
import re
import subprocess
import tempfile
import threading
import time

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
MIGRATIONS = os.path.join(ROOT, "go/internal/storage/postgres/migrations")
FIXTURE = os.path.join(ROOT, "docs/internal/evidence/7127-link-writer-fixture.sql")
RESULTS = os.path.join(ROOT, "docs/internal/evidence/7127-link-writer-scale-results.json")
G7_RESULTS = os.path.join(ROOT, "docs/internal/evidence/7127-link-writer-g7-results.json")
CONTAINER_ARGS = ["-e", "POSTGRES_PASSWORD=pw", "-e", "POSTGRES_DB=eshu", "--memory", "12g", "--shm-size", "2g",
                  "postgres:18-alpine", "-c", "shared_buffers=1GB", "-c", "work_mem=64MB",
                  "-c", "maintenance_work_mem=1GB", "-c", "effective_cache_size=6GB", "-c", "random_page_cost=1.1",
                  "-c", "synchronous_commit=off", "-c", "max_wal_size=8GB", "-c", "log_temp_files=0",
                  "-c", "track_io_timing=on"]
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


def start_container(name, port):
    subprocess.run(["docker", "run", "-d", "--name", name, "-p", f"127.0.0.1:{port}:5432"] + CONTAINER_ARGS,
                   check=True, stdout=subprocess.DEVNULL)
    # The image's entrypoint runs a temporary server for initdb and then
    # restarts; wait for the init to complete before trusting pg_isready.
    for _ in range(120):
        logs = subprocess.run(["docker", "logs", name], capture_output=True, text=True)
        if "PostgreSQL init process complete" in logs.stdout + logs.stderr and subprocess.run(
                ["docker", "exec", name, "pg_isready", "-U", "postgres"], stdout=subprocess.DEVNULL).returncode == 0:
            return
        time.sleep(1)
    raise SystemExit("container did not become ready")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--container", required=True)
    parser.add_argument("--port", required=True)
    parser.add_argument("--start-container", action="store_true")
    parser.add_argument("--skip-load", action="store_true")
    parser.add_argument("--rounds", default="6")
    parser.add_argument("--g7-only", action="store_true")
    parser.add_argument("--valid-rounds", default="10")
    parser.add_argument("--deadline", default="8h")
    args = parser.parse_args()
    env = dict(os.environ)
    env.pop("GOROOT", None)
    binary = os.path.join(tempfile.mkdtemp(prefix="cs7127-"), "links.test")
    subprocess.run(["go", "test", "-c", "-o", binary, "./internal/storage/postgres/freshness/links"],
                   cwd=os.path.join(ROOT, "go"), env=env, check=True)
    if args.start_container:
        start_container(args.container, args.port)
    result = {"fixture": None if args.skip_load else load(args.container), "events": []}
    sweeps, stop = [], threading.Event()
    sampler = threading.Thread(target=sample, args=(args.container, sweeps, stop), daemon=True)
    sampler.start()
    env.update(ESHU_CHANGED_SINCE_LINK_SCALE_DSN=(
        f"postgres://postgres:pw@127.0.0.1:{args.port}/{DB}?sslmode=disable"))
    if args.g7_only:
        env.update(ESHU_CHANGED_SINCE_LINK_SCALE_G7_ONLY="1", ESHU_CHANGED_SINCE_LINK_SCALE_ROUNDS="0",
                   ESHU_CHANGED_SINCE_LINK_SCALE_VALID_ROUNDS=args.valid_rounds,
                   ESHU_CHANGED_SINCE_LINK_SCALE_DEADLINE=args.deadline)
    else:
        env.update(ESHU_CHANGED_SINCE_LINK_SCALE_ROUNDS=args.rounds)
    test = subprocess.Popen([binary, "-test.run", "TestLinkScaleEvidence", "-test.count=1", "-test.v",
                             "-test.timeout", "12h"],
                            cwd=os.path.join(ROOT, "go/internal/storage/postgres/freshness/links"), env=env,
                            stdout=subprocess.PIPE, text=True)
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
    valid = [e["ratio"] for e in result["events"] if e.get("event") == "g7_round" and e.get("valid")]
    roots = [e["root_ratio"] for e in result["events"] if e.get("event") == "g7_round" and e.get("valid")]
    if valid:
        result["g7_valid_rounds"] = len(valid)
        result["g7_median_l1b_over_bare_b"] = sorted(valid)[len(valid) // 2] if len(valid) % 2 else \
            (sorted(valid)[len(valid) // 2 - 1] + sorted(valid)[len(valid) // 2]) / 2
        result["g7_max_root_over_bare_b"] = max(roots)
    out = G7_RESULTS if args.g7_only else RESULTS
    json.dump(result, open(out, "w"), indent=1, sort_keys=True)
    open(out, "a").write("\n")
    os.remove(binary)
    print("wrote", out, "go test rc", result["go_test_rc"])
    return result["go_test_rc"]


if __name__ == "__main__":
    raise SystemExit(main())
