#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""The numbers block of report.sh: per run, the engaged-scrape consult p50, p95 and share within 100 ms, K, parked rows, superseded scopes, A drain after catch-up, host load; plus the claim-grant consult when a run has one.
A file rather than a heredoc: the heredoc budget gate.

Usage: report_numbers.py <results-root>
"""
import glob, json, os, sys
for path in sorted(glob.glob(os.path.join(sys.argv[1], "*-rep*", "result.json"))):
  r = json.load(open(path))
  a = r.get("consult", {}).get("A", {})
  rep = r["report"]
  print("%-12s ttl=%-3s consult(A) p50=%.1fms p95=%.1fms within100ms=%.2f n=%d | K_peak=%s parked_peak=%s woken=%s wait_rows=%s "
        "| superseded_scopes=%s paced_A=%s | A_done_after_catchup=%ss | load1 max=%s end=%s" % (
            os.path.basename(os.path.dirname(path)), r.get("ttl"), 1000 * a.get("p50_s", 0), 1000 * a.get("p95_s", 0),
            a.get("within_100ms", 0), a.get("count", 0), rep["k_peak"], rep["parked_rows_peak"], rep["woken_holding_wait"],
            rep["wait_table_rows_final"], r["paced"]["superseded_scopes"], r["paced"]["paced_items"].get("A", 0),
            r.get("a_drain", {}).get("after_catchup_s"), rep.get("load1_max"), rep.get("load1_end")))
  g = r.get("consult_claim_grant", {})
  if g.get("count"):
    print("%-12s claim-grant consult (collector_kind series, hosted collector): n=%d p50=%.1fms p95=%.1fms within100ms=%.2f" % (
        os.path.basename(os.path.dirname(path)), g["count"], 1000 * g["p50_s"], 1000 * g["p95_s"], g["within_100ms"]))
