#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Stream whole scopes from the read replica into compressed COPY files.

READ-ONLY: every statement is a SELECT/COPY TO STDOUT inside a session forced
to default_transaction_read_only=on. No temp tables, no writes. Pages are
small keyset pages over (scope_id, generation_id, observed_at, fact_id), the
shape of fact_records_scope_generation_keyset_idx, so each statement stays
well under the replica's max_standby_streaming_delay and a recovery conflict
only costs one page (retried).

Reads REPLICA_DSN from the environment (never stored). Usage:
  REPLICA_DSN=... python3 -I pull_sample.py WORKDIR PIECE [--workers 3]
where PIECE names WORKDIR/PIECE.scopes (one scope id per line) and the
outputs are WORKDIR/PIECE/{ingestion_scopes,scope_generations}.tsv.zst and
WORKDIR/PIECE/facts/NNNNN.tsv.zst (zstd frames concatenate cleanly).
"""
import argparse
import concurrent.futures as cf
import csv
import os
import subprocess
import sys
import time

COLS = ("fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, "
        "collector_kind, fencing_token, source_confidence, source_system, source_fact_key, "
        "source_uri, source_record_id, observed_at, ingested_at, is_tombstone, payload")
PAGE_ROWS = 50_000
SMALL_BATCH_ROWS = 100_000


def q(s: str) -> str:
    return "'" + s.replace("'", "''") + "'"


def psql(sql: str, out_path=None, tuples=False, retries=6):
    """Run one read-only statement; stdout goes to out_path (zstd) or is returned."""
    env = dict(os.environ)
    env["PGOPTIONS"] = "-c default_transaction_read_only=on -c statement_timeout=240000"
    # The statement goes in on stdin: a scope list can exceed the 128 KiB
    # single-argument limit of exec.
    cmd = ["psql", env["REPLICA_DSN"], "-X", "-q", "-A", "-v", "ON_ERROR_STOP=1"]
    if tuples:
        cmd += ["-t", "-F", "\t"]
    cmd += ["-f", "-"]
    last = ""
    for attempt in range(retries):
        tmp = (out_path + ".part") if out_path else None
        try:
            if out_path:
                with open(tmp, "wb") as fh:
                    zs = subprocess.Popen(["zstd", "-3", "-T1", "-q", "-c"], stdin=subprocess.PIPE, stdout=fh)
                    p = subprocess.Popen(cmd, env=env, stdin=subprocess.PIPE, stdout=zs.stdin,
                                         stderr=subprocess.PIPE)
                    err = p.communicate(sql.encode())[1]
                    zs.stdin.close()
                    zs.wait()
                if p.returncode == 0 and zs.returncode == 0:
                    os.replace(tmp, out_path)
                    return None
            else:
                p = subprocess.run(cmd, env=env, input=sql, capture_output=True, text=True)
                if p.returncode == 0:
                    return p.stdout
                err = p.stderr.encode()
            last = err.decode(errors="replace")[:300]
        except Exception as exc:  # noqa: BLE001 - retry any transport failure
            last = repr(exc)
        time.sleep(2 + attempt * 3)
    raise RuntimeError(f"statement failed after {retries} tries: {last}")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("workdir")
    ap.add_argument("piece")
    ap.add_argument("--workers", type=int, default=3)
    a = ap.parse_args()
    scopes = [l.strip() for l in open(f"{a.workdir}/{a.piece}.scopes") if l.strip()]
    counts = {r[0]: int(r[2]) for r in csv.reader(open(f"{a.workdir}/scope_counts.csv"))}
    out = f"{a.workdir}/{a.piece}"
    os.makedirs(f"{out}/facts", exist_ok=True)
    arr = "ARRAY[" + ",".join(q(s) for s in scopes) + "]::text[]"
    psql(f"COPY (SELECT * FROM ingestion_scopes WHERE scope_id = ANY({arr})) TO STDOUT",
         f"{out}/ingestion_scopes.tsv.zst")
    psql(f"COPY (SELECT * FROM scope_generations WHERE scope_id = ANY({arr})) TO STDOUT",
         f"{out}/scope_generations.tsv.zst")
    # Plan the fact pulls: small scopes batch together; big scopes page per generation.
    jobs, batch, batch_rows = [], [], 0
    for s in sorted(scopes, key=lambda x: counts.get(x, 0)):
        n = counts.get(s, 0)
        if n > SMALL_BATCH_ROWS:
            gens = psql(f"SELECT generation_id FROM scope_generations WHERE scope_id = {q(s)}", tuples=True).split()
            for g in gens:
                bounds = psql(
                    "SELECT observed_at::text, fact_id FROM (SELECT observed_at, fact_id, "
                    "row_number() OVER (ORDER BY observed_at, fact_id) rn FROM fact_records "
                    f"WHERE scope_id = {q(s)} AND generation_id = {q(g)}) t WHERE rn % {PAGE_ROWS} = 0 "
                    "ORDER BY rn", tuples=True)
                prev = None
                for line in [l for l in bounds.split("\n") if l.strip()] + [None]:
                    hi = tuple(line.split("\t")) if line else None
                    cond = f"scope_id = {q(s)} AND generation_id = {q(g)}"
                    if prev:
                        cond += f" AND (observed_at, fact_id) > ({q(prev[0])}::timestamptz, {q(prev[1])})"
                    if hi:
                        cond += f" AND (observed_at, fact_id) <= ({q(hi[0])}::timestamptz, {q(hi[1])})"
                    jobs.append(cond)
                    prev = hi
        else:
            batch.append(s)
            batch_rows += n
            if batch_rows >= SMALL_BATCH_ROWS:
                jobs.append("scope_id = ANY(ARRAY[" + ",".join(q(x) for x in batch) + "]::text[])")
                batch, batch_rows = [], 0
    if batch:
        jobs.append("scope_id = ANY(ARRAY[" + ",".join(q(x) for x in batch) + "]::text[])")

    def run(idx_cond):
        i, cond = idx_cond
        path = f"{out}/facts/{i:05d}.tsv.zst"
        if os.path.exists(path):
            return i
        psql(f"COPY (SELECT {COLS} FROM fact_records WHERE {cond}) TO STDOUT", path)
        return i

    t0 = time.time()
    done = 0
    with cf.ThreadPoolExecutor(max_workers=a.workers) as ex:
        for _ in ex.map(run, list(enumerate(jobs))):
            done += 1
            if done % 10 == 0 or done == len(jobs):
                print(f"{a.piece}: {done}/{len(jobs)} pages {time.time()-t0:.0f}s", flush=True)
    print(f"{a.piece}: pulled {len(jobs)} files in {time.time()-t0:.0f}s")
    return 0


if __name__ == "__main__":
    sys.exit(main())
