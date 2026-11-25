# #11430 SCI handler service-time decomposition: holder reads dominate p50, write_findings dominates the tail

Date: 2026-11-25. Lane: drive/11430-drain-bottleneck (leaf child).
Tree: branch `drive/11430-drain-bottleneck` (measurement only; no production code touched).
Context: arbiter killed partitioned parallelism; primary lever is SCI handler
service-time decomposition (holder reads plus winners tick). Claim-vs-complete
gap reads as claim-then-defer/reopen churn. This lane is MEASUREMENT ONLY:
per-intent sub-duration cut, holder Postgres reads vs gate wait vs write vs
winners phases at p50/p95. STOP after the decomposition: no theory shim, no
implementation, no design doc.

## Verdict

HOLDER READS CONVICTED at p50; WRITE_FINDINGS dominates the p95 tail. No flat
split: the two phases trade the lead across percentiles.

- Handler service time (`handler_duration_seconds`, n=158 succeeded):
  p50 50.8s, p95 283.0s, max 822.4s.
- Holder Postgres reads (sum of the 11 `load_*` sub-durations, all Postgres
  fact loads in `loadSupplyChainImpactEvidence`): p50 29.2s (0.65 share of
  handler), p95 96.5s (0.99 share). Dominant at p50.
- `write_findings` (durable findings commit via
  `WriteSupplyChainImpactFindings`; graph/AdvisoryGroup projection reconciles
  downstream from committed winners, not inside this span): p50 9.1s (0.16
  share), p95 130.5s (0.59 share), max 247.9s. Dominant at p95.
- Inside holder reads, `load_active_evidence` is ~79% of the p50:
  p50 23.0s, p95 77.4s, max 277.0s. Next: `load_manifest_dependencies`
  p50 1.0s / p95 18.3s / max 365.4s (fattest single outlier),
  `load_repository_facts` p50 1.8s / p95 4.1s,
  `load_scope_facts` p50 0.4s / p95 3.3s. All other loads round to ~0 at p50.
- Gate wait: 4 deferred of 162 SCI executions (2.5%), all
  `cross_scope_producer_not_ready` (`ci_cd_run_correlation` inactive for the
  consumer scope), failing inside `load_authorized`, 9.9-25.4s wasted each.
  No `supports`/`attachment` gate defers observed in-window. Gate wait is NOT
  the service-time story; queue wait is the end-to-end story (below).
- Winners tick: 1 keyed resweep committed in 6h (25.9s for 200 identifiers,
  454 identifiers / 232 keys touched, 200 updated + 32 deleted) against
  `remaining: 54558`; 53 resweeps skipped (lock in flight). At one ~26s / 200-id
  commit per 6h the 54k remainder never drains. Downstream effect is direct:
  `advisory_group_projection` failed 158/158 in-window, 100%
  `advisory_group_winners_watermark_stale` (+189 deferred).
- Queue wait (`queue_wait_seconds` on succeeded SCI): p50 113644s (~31.6h),
  p95 113845s. End-to-end latency is backlog age, not service time; the QA
  census below agrees (pending 8355 rows, all attempt_count 0, avg age 219h).

## Code trace (sci handler path)

- `SupplyChainImpactHandler.Handle`
  (`go/internal/reducer/supplychain/core/impact.go`): gate
  (`beforeEvidenceLoad`, `attachment_wait.go:153`: supports-readiness gate,
  #9874 manifest marker, #11105 attachment wait) -> fencing token ->
  `loadSupplyChainImpactEvidence` (`evidence_load.go:44`: the 11 timed
  `load_*` Postgres phases) -> `buildAuthorizedSupplyChainImpactFindings`
  (`build_findings`) -> suppressions (`evaluate_suppressions`) ->
  `writeFindingsWithSuppressionDiff` (`suppression_fanout_wire.go:445`:
  `Writer.WriteSupplyChainImpactFindings`, timed as `write_findings`) ->
  `enqueueAdvisoryGroupProjections` (own domain, after commit) ->
  `fanOutHolders` (post-write holder reopen, fails the pass on read/reopen
  error) -> `emitCounters`.
- Timing struct + keys: `supplyChainImpactTiming` /
  `supplyChainImpactSubDurations` (`diagnostics.go:19,167`). Succeeded-line
  log attributes: `sub_duration_<key>_seconds` + `handler_duration_seconds` +
  `queue_wait_seconds` (`service_observability.go:76`).
- Winners: keyed resweep outside the resweep lock (`winners_keyed.go:13,66`);
  maintainer tick path `winners_keyed_maintainer_test.go:75`
  (full resweep once, keyed ticks after). 30s resweep cadence per
  `HOLDER_FANOUT.md`; observed throughput collapses against the 54k remainder.

## Commands and observed values

All reads read-only; kubectl log pulls from the QA replica;
SQL via `sh /tmp/qa-read.sh <file>` (15s statement timeout, read-only txn).

1. `kubectl -n eshu logs
   deploy/eshu-resolution-engine-vulnerability --since=6h > /tmp/sci_all.jsonl`
   (6.7 MB single window; general engine has no SCI lines, vulnerability
   engine owns the domain):
   SCI succeeded 158 / deferred 4 / failed 0;
   advisory_group_projection failed 158 / deferred 189;
   winners "resweep skipped: another resweep is in flight" 53,
   "winners resweep committed" 1.
2. Per-intent cut over the 158 succeeded lines (p50 / p95 / max, seconds):

   | phase | p50 | p95 | max |
   |---|---|---|---|
   | handler | 50.839 | 283.044 | 822.448 |
   | holder reads (11 load_*) | 29.248 | 96.546 | 417.241 |
   | write_findings | 9.133 | 130.527 | 247.903 |
   | build_findings | 0.189 | 2.042 | 4.479 |
   | security_alert_scoping | 0.000 | 0.024 | 0.146 |
   | evaluate_suppressions | 0.000 | 0.002 | 0.003 |
   | emit_counters | 0.000 | 0.001 | 0.002 |
   | queue_wait (end-to-end, not service) | 113644.118 | 113844.530 | 113889.770 |

   Per-load detail: load_active_evidence 23.009 / 77.411 / 276.974;
   load_manifest_dependencies 0.953 / 18.303 / 365.400;
   load_repository_facts 1.763 / 4.131 / 31.961;
   load_scope_facts 0.446 / 3.289 / 11.697;
   load_go_reachability 0.017 / 0.256 / 0.886;
   load_authorized_targets 0.023 / 0.211 / 0.476;
   load_os_package_advisory 0.011 / 0.177 / 5.201;
   load_python_reachability 0.001 / 0.023 / 89.452;
   load_jvm_reachability 0.002 / 0.016 / 0.027;
   load_scanner_analysis_scope and load_resolved_digest_evidence 0.000
   throughout.
   Shares of handler: holder 0.65 / 0.99 (p50/p95 per-row share),
   write 0.16 / 0.59.
   Fan-out sub-signals (post-write holder reopen volume per pass):
   fanout_holders p50 0 / p95 474 / max 889;
   fanout_holder_statements p50 0 / p95 512 / max 1632;
   written_rows p50 403 / p95 3844 / max 6933.
3. The one committed resweep (2026-11-25T17:28:07Z): path=keyed,
   identifiers 454, keys 232, inserted 0, updated 200, deleted 32,
   duration 25.875s, previous_watermark_age 89.196s, bounded true,
   remaining 54558. Advisory failures in the same window: 158/158
   `advisory_group_winners_watermark_stale`.
4. QA replica census (`/tmp/qa_sci_q.sql` via `/tmp/qa-read.sh`):
   SCI universe 12425 (pending 8355, retrying 530, succeeded 1603,
   superseded 1897, dead_letter 29, claimed 11);
   drain backlog (pending+retrying) 8885;
   never-attempted (last_attempt_at NULL) 1568 at avg age 104.1h;
   pending rows all attempt_count 0 at avg age 219.0h;
   `supply_chain_impact_canonical_winners` 84910 rows.

## Not covered

- No per-statement Postgres breakdown inside `load_active_evidence`
  (branch stats exist as `sub_duration_active_evidence_branch_*` and were not
  cut here); the p95-vs-max gap (77s vs 277s) and the manifest/Python
  outliers (365s / 89s max) are NOT_CHECKED for cause.
- No `write_findings` split (canonical commit vs suppression diff vs
  fan-out stamp I/O); its p95 tail (130.5s) exceeding holder p95 is measured
  but unexplained.
- No gate-wait measurement beyond the 4 observed defers (supports and
  attachment gates had zero in-window samples; absence is not exoneration).
- Winners tick period distribution: only one commit observed, so no p50/p95
  for the tick itself; the 53 skips + 54558 remainder + 158 stale-watermark
  failures are the measured signal.
- Per-claim queue_wait vs attempt_count join (claim-then-defer/reopen churn
  shape) is NOT_CHECKED by this lane; numbers above are the coordinator's
  input.
- No theory shim, no implementation, no design doc per the STOP rule.
