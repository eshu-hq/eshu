# #11430 write_findings p95 decomposition: canonical commit convicted, suppression diff and fan-out exonerated

Date: 2026-11-25. Lane: drive/11430-drain-bottleneck (leaf child).
Tree: branch `drive/11430-drain-bottleneck` (measurement only; no production code touched).
Context: parent lane measured `write_findings` p50 9.1s / p95 130.5s
(share 0.16/0.59 of handler) with cause NOT_CHECKED. This lane splits the
write path into canonical commit vs suppression diff vs fan-out stamp I/O.
STOP after the split: no theory shim, no implementation, no design doc.

## Verdict

CANONICAL COMMIT CONVICTED at ~100% of `write_findings` at p50 AND p95.
Suppression diff and fan-out stamp I/O are EXONERATED as write-tail causes.

- `write_findings` (fresh 6h QA window, n=176 succeeded):
  p50 5.2s, p95 264.8s, max 351.5s.
- Suppression diff per-intent duration (joined on `intent_id`):
  p50 0.000000s, p95 0.000001s, max 0.000006s. 172 passes
  `skipped_kind` with 0 statements; 4 passes emit no diff line at all
  (non-suppression kinds take the no-op span path). Contribution rounds
  to zero at every percentile.
- Remainder (`write_findings` minus diff) is identical to `write_findings`:
  p50 5.2s, p95 264.8s, max 351.5s. The remainder is the canonical commit
  path inside `runSupplyChainImpactTx` (admission, stored read, ~10k-row
  upsert, retraction, winners dirty capture, go-evidence recheck, COMMIT).
- Fan-out stamp I/O runs AFTER the commit and OUTSIDE the `write_findings`
  span (`impact.go:304` vs the timed span at `:280-287`), and carries no
  duration telemetry of its own (volume sub-signals only). Tails up to
  349s occur with `fanout_holders=0` / 0 holder statements, so the write
  tail is independent of fan-out volume: fanout>0 remainder p50 28.8s /
  p95 212.6s (n=82) vs fanout=0 remainder p50 1.3s / p95 265.1s (n=94).
- Row-volume gradient: `written_rows` p50 260 / p95 9996 / max 10335;
  bottom-written decile write p50 0.0s vs top-written decile 264.8s.
  Retraction is negligible in-window (nonzero in 18/176, max 588 rows).

## Code trace (write path phase ordering)

- `SupplyChainImpactHandler.Handle` (`go/internal/reducer/supplychain/core/impact.go`):
  `:280-287` starts `phaseStarted`, calls `writeFindingsWithSuppressionDiff`,
  records `timing.writeFindingsDuration` — the span covers the writer
  transaction ONLY. `:298` `enqueueAdvisoryGroupProjections` and `:304`
  `fanOutHolders` run after the write returns, untimed (only `:309-315`
  `emitCounters` is timed, as `emit_counters`).
- `writeFindingsWithSuppressionDiff` (`suppression_fanout_wire.go:445`):
  opens the diff span, calls `Writer.WriteSupplyChainImpactFindings`,
  emits the diff report. The diff `duration` comes from
  `runSuppressionDiff` (`:193-298`); a pass whose `ScopeFactKinds` lack
  the suppression kind returns `suppressionDiffSkipKind` at `:206-208`
  before any statement (172/176 in-window); KEV/EPSS/advisory passes take
  the no-op span (`:307-317`) and emit no diff line (4/176).
- `WriteSupplyChainImpactFindings` (`writer.go:65`): payload
  encode/marshal (in-memory, untimed), then `runSupplyChainImpactTx`
  (`writer_retract.go:39`): conflict-domain advisory lock -> suppression
  diff -> fencing admission -> stored-read -> `WriteSupplyChainImpactFindingRows`
  upsert -> winners dirty capture (`:193-202`) -> retraction (`:256-272`) ->
  pass-state record -> go-evidence lock+recheck (`:302-314`) -> COMMIT.
  The only tx-internal observation is the `RecordSupplyChainImpactWriteTx`
  histogram (`writer.go:122`, go-evidence wait included); no per-statement
  timing reaches the log line. `diagnostics.go:167-187` confirms the
  succeeded line has no write sub-split keys.
- Fan-out volume signals (`fanout_holders`, `fanout_holder_statements`,
  `fanout_holder_key_range_statements`, `fanout_stamp_widened`) ride the
  succeeded line but measure post-commit holder-reopen I/O, not the write.

## Commands and observed values

All reads read-only; log pulls from the QA replica; SQL via
`sh /tmp/qa-read.sh <file>` (15s statement timeout, read-only txn).

1. `kubectl -n eshu logs deploy/eshu-resolution-engine-vulnerability
   --since=6h > /tmp/sci_write_split.jsonl` (run under the QA kubectl
   context; 4637 lines, 8.78 MB):
   SCI succeeded 176 / deferred 7 / failed 1; suppression-diff lines 174;
   holder fan-out lines 82; winners resweep committed 0 / skipped 0
   (no resweep activity in this window, unlike the parent window).
2. Per-intent join over the 176 succeeded lines (p50 / p95 / max, seconds):

   | phase | p50 | p95 | max |
   |---|---|---|---|
   | write_findings | 5.182 | 264.768 | 351.456 |
   | suppression diff (same intents) | 0.000000 | 0.000001 | 0.000006 |
   | remainder = canonical commit path | 5.182 | 264.768 | 351.456 |
   | handler (for share) | 37.671 | 439.833 | 978.901 |

   Diff actions: `skipped_kind` 172 (0 statements, `none` baseline),
   no diff line 4. Remainder-vs-fanout: fanout>0 (n=82) p50 28.8 /
   p95 212.6; fanout=0 (n=94) p50 1.3 / p95 265.1.
   `written_rows` p50 260 / p95 9996 / max 10335; bottom-written decile
   write p50 0.0s vs top-written decile 264.8s.
3. Top-8 write tails (w=write_findings, rem=remainder, all sdiff<=6us):

   | w (s) | rem (s) | written | retracted | fanout_h/stmts | scope (truncated) |
   |---|---|---|---|---|---|
   | 265.1 | 265.1 | 9996 | 0 | 0/0 | osv/npm batch @aws-sdk/querystring-p... |
   | 265.2 | 265.2 | 9996 | 0 | 0/0 | osv/npm batch @aws-sdk/middleware-sd... |
   | 279.3 | 279.3 | 9996 | 0 | 0/0 | osv/npm batch @aws-sdk/types... |
   | 308.1 | 308.1 | 9996 | 0 | 0/0 | osv/npm batch @aws-sdk/s3-request-pr... |
   | 324.9 | 324.9 | 10335 | 0 | 462/1088 | osv/npm batch @aws-sdk/token-provide... |
   | 343.1 | 343.1 | 9996 | 0 | 0/0 | osv/npm batch @aws-sdk/util-body-len... |
   | 348.8 | 348.8 | 9996 | 0 | 0/0 | osv/npm batch @aws-sdk/signature-v4-... |
   | 351.5 | 351.5 | 9996 | 0 | 236/1072 | osv/npm batch @aws-sdk/middleware-re... |

   Median pass: w=5.22s, sdiff=0.0s, written=200, fanout 0/0.
4. QA replica SQL (both recorded as attempted, neither yielded a census):
   full-kind `fact_records` count for `reducer_supply_chain_impact_finding`
   + `vulnerability.suppression` hit the 15s statement timeout (table too
   large for an unindexed count in-budget); per-generation count for the
   max-write generation
   (`vulnerability_intelligence:15e49...81f38ed3d`) dropped the connection
   mid-scan. NOT retried: the writer's own per-pass `written_rows` is the
   direct volume measurement and needs no replica recount.

## Named dominant sub-phase

CANONICAL COMMIT PATH: ~100% of `write_findings` at p50 and p95
(5.2s / 264.8s), scaling with canonical upsert row volume (~10k rows at
the tail, ~28-38 rows/s implied). Suppression diff (~0s, 0 statements on
172/176 passes) and fan-out stamp I/O (post-commit, outside the span;
349s tails with zero fan-out volume) are exonerated.

## Not covered (explicit cannot-attribute)

- INSIDE-canonical split (row upsert vs retraction vs winners dirty
  capture vs go-evidence recheck vs COMMIT) is NOT attributable from
  current telemetry: no per-statement timing exists inside
  `commitSupplyChainImpactTx`, and the go-evidence lock wait lives only
  on the OTEL histogram, not on any log line. Retraction is bounded
  small in-window (18/176 nonzero, max 588), which leaves the ~10k-row
  upsert + COMMIT as the unseparated remainder, not a measurement.
- Theory that would explain the remainder (NAMED, not built, STOP):
  per-row canonical upsert throughput bound at ~10k-row batch scale.
  Proving measurement: per-statement wall timing inside
  `commitSupplyChainImpactRows` (upsert vs retraction vs winners-capture
  vs COMMIT) over one tail window, or a `pg_stat_statements` split of
  `WriteSupplyChainImpactFindingRows` vs the retraction CTE vs COMMIT on
  the QA replica during a tail pass. No shim, no implementation, no
  design doc per the STOP rule.
