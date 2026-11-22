#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Rewrite a pg_dump --schema-only file so fact_records is HASH(scope_id).

Only three things change, every other statement (all 153 indexes, FKs,
triggers, other tables) passes through untouched, so the divided copy keeps
the production index set and the comparison isolates the partitioning:

  1. CREATE TABLE public.fact_records gets PARTITION BY HASH (scope_id)
     and the MODULUS/REMAINDER partitions right after it.
  2. PRIMARY KEY (fact_id) becomes PRIMARY KEY (scope_id, fact_id), the
     minimum a partitioned unique index allows.
  3. The two foreign keys lose ONLY (rejected on a partitioned table).
     Statements PostgreSQL still rejects are left in place so the restore
     log records each rejection.

Usage: python3 -I make_divided_schema.py schema_full.sql PARTS > divided.sql
"""
import re
import sys


def main() -> int:
    src, parts = sys.argv[1], int(sys.argv[2])
    text = open(src).read()
    # The production table carries per-table autovacuum storage options
    # (WITH (...)); a partitioned parent cannot hold them, so they move to
    # every partition, where the rows live.
    pat = re.compile(r"(CREATE TABLE public\.fact_records \(\n(?:(?!\n\)).)*\n\))(\nWITH \([^;]*\))?;\n", re.S)
    m = pat.search(text)
    if not m:
        sys.exit("fact_records CREATE TABLE not found")
    opts = m.group(2) or ""
    ddl = "".join(
        f"CREATE TABLE public.fact_records_p{i} PARTITION OF public.fact_records "
        f"FOR VALUES WITH (MODULUS {parts}, REMAINDER {i}){opts};\n" for i in range(parts))
    text = text[:m.start()] + m.group(1) + " PARTITION BY HASH (scope_id);\n" + ddl + text[m.end():]
    pk = "ADD CONSTRAINT fact_records_pkey PRIMARY KEY (fact_id)"
    if pk not in text:
        sys.exit("fact_records_pkey not found")
    text = text.replace(pk, "ADD CONSTRAINT fact_records_pkey PRIMARY KEY (scope_id, fact_id)", 1)
    # PostgreSQL rejects ALTER TABLE ONLY ... ADD FOREIGN KEY on a partitioned
    # table (found by the rehearsal restore); the FK must apply to all parts.
    text, n_fk = re.subn(
        r"ALTER TABLE ONLY public\.fact_records\n(\s+ADD CONSTRAINT \w+ FOREIGN KEY)",
        r"ALTER TABLE public.fact_records\n\1", text)
    if n_fk != 2:
        sys.exit(f"expected 2 fact_records foreign keys, rewrote {n_fk}")
    sys.stdout.write(text)
    return 0


if __name__ == "__main__":
    sys.exit(main())
