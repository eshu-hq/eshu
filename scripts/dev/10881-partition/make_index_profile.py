#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Cut the fact_records secondary indexes of a pg_dump --schema-only file.

The production table carries 153 indexes, most of them read indexes that
#10880 moves off the table before any partitioning. This keeps the indexes
the write, retention and scope-keyed paths need (the "retained" profile) and
drops the rest, so a second size can be measured at the post-#10880 shape:

  fact_records_pkey (the primary key line is not an index statement and is
  kept as-is), fact_records_scope_generation_idx,
  fact_records_scope_generation_keyset_idx,
  fact_records_content_entity_key_idx, fact_records_file_key_idx
  (the two key indexes generationRetentionKeyIndexesQuery requires).

Usage: python3 -I make_index_profile.py schema.sql > schema_retained.sql
"""
import re
import sys

KEEP = {
    "fact_records_scope_generation_idx",
    "fact_records_scope_generation_keyset_idx",
    "fact_records_content_entity_key_idx",
    "fact_records_file_key_idx",
}
PAT = re.compile(r"CREATE (?:UNIQUE )?INDEX (\w+) ON public\.fact_records .*?;\n", re.S)


def main() -> int:
    text = open(sys.argv[1]).read()
    dropped = kept = 0

    def sub(m):
        nonlocal dropped, kept
        if m.group(1) in KEEP:
            kept += 1
            return m.group(0)
        dropped += 1
        return ""

    out = PAT.sub(sub, text)
    sys.stderr.write(f"kept={kept} dropped={dropped}\n")
    if kept != len(KEEP):
        sys.exit(f"expected {len(KEEP)} kept indexes, found {kept}")
    sys.stdout.write(out)
    return 0


if __name__ == "__main__":
    sys.exit(main())
