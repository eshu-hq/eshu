#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Total the VACUUM (VERBOSE) log of one arm across every relation it vacuumed.

Prints heap pages scanned and removed, index pages in total, index scans,
and buffer hits, misses and dirtied, summed over the table or all its parts.
Usage: python3 -I parse_vacuum.py vacuum.log
"""
import re
import sys

text = open(sys.argv[1]).read()
tot = {"tables": 0, "pages_removed": 0, "pages_remain": 0, "pages_scanned": 0, "index_scans": 0,
       "index_pages_total": 0, "index_pages_newly_deleted": 0, "buf_hits": 0, "buf_misses": 0,
       "buf_dirtied": 0, "tuples_removed": 0}
for m in re.finditer(r"finished vacuuming \"[^\"]+\": index scans: (\d+)", text):
    tot["tables"] += 1
    tot["index_scans"] += int(m.group(1))
for m in re.finditer(r"pages: (\d+) removed, (\d+) remain, (\d+) scanned", text):
    tot["pages_removed"] += int(m.group(1))
    tot["pages_remain"] += int(m.group(2))
    tot["pages_scanned"] += int(m.group(3))
for m in re.finditer(r"tuples: (\d+) removed", text):
    tot["tuples_removed"] += int(m.group(1))
for m in re.finditer(r"index \"[^\"]+\": pages: (\d+) in total, (\d+) newly deleted", text):
    tot["index_pages_total"] += int(m.group(1))
    tot["index_pages_newly_deleted"] += int(m.group(2))
for m in re.finditer(r"buffer usage: (\d+) hits, (\d+) misses, (\d+) dirtied", text):
    tot["buf_hits"] += int(m.group(1))
    tot["buf_misses"] += int(m.group(2))
    tot["buf_dirtied"] += int(m.group(3))
print(" ".join(f"{k}={v}" for k, v in tot.items()))
