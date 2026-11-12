# Reducer batch-claim funnel telemetry (#10565 box 6)

This companion note keeps the package README focused while preserving the
funnel design, evidence, and operator contract for the batch-claim admission
telemetry future claim changes must respect.

The service layer (`runBatchConcurrent` in `go/internal/reducer/service_batch.go`)
records the admitted batch size (`eshu_dp_reducer_batch_claim_size`) once per
`ClaimBatch` with `queue="reducer"`, including the early-return conflict
path. The storage claim records no size histogram: production wires the same
`*Instruments` into both layers (`go/cmd/reducer/main_helpers.go` and
`go/cmd/reducer/main.go`), so a second recording in the storage claim would
double-count every production claim — `TestClaimBatchDoesNotRecordBatchClaimSize`
pins the storage silence and `TestServiceBatchClaimRecordsSingleBatchClaimSize`
pins the single service emission. The issue requires the full admission
funnel — candidates in, representatives after the fence, and drops by reason —
as the operator signal for the claim-admission ceiling work. With
`ESHU_REDUCER_BATCH_CLAIM_FUNNEL_ENABLED=true` (default false per arbiter
ruling ACCEPT-(b), #10565), a claim runs one extra read-only counts query
(`reducerClaimBatchFunnelQuery`,
`reducer_queue_batch_claim_funnel.go`) and records
`eshu_dp_reducer_batch_claim_candidates` and
`eshu_dp_reducer_batch_claim_representatives` (per-claim histograms,
`batch-claim-funnel-count` buckets) plus
`eshu_dp_reducer_batch_claim_drops_total` (counter, closed `reason`:
`not_ready`, `not_representative`, `projector_drain`, `semantic_cap`,
`live_lease_fence`, `over_limit`, `lock_lost`). The funnel reconciles per
claim: admitted + drops == candidates (exact uncontended; approximate under a
claimer storm, where a concurrent claim can land between the two statements).

The counts derive from the same Go sources as the shipped claim: the funnel
composes `reducerClaimBatchCandidateChainCTEs()` (everything from
`reducer_source_inflight` through `candidate`) verbatim, so a future gate edit
lands in both or the substring guards fail. The sweep exclusion derives from
the same shared sweep sources (locked columns, carrier arm, carrier CTE, row
filters) rendered as SELECT-only stand-ins — `TestClaimBatchFunnelQueryIsReadOnly`
pins both the sharing and the no-mutation contract. The live-lease fence derives as
pre-fence minus representatives (the candidate CTE's only additional filter);
the limit trim and lock-time loss derive in Go (`over_limit` past the batch
limit, `lock_lost` as the within-limit shortfall against the admitted batch).
Funnel failure is best-effort: it records nothing and never fails the claim,
and queues without instruments skip the second query entirely. The shared
chain keeps the claim's `$1..$7` numbering, so the funnel's final SELECT
carries a tautological `$3`/`$4` WHERE clause: Postgres rejects parameters
whose type it cannot infer (42P18) and an unreferenced placeholder has no
type.

No-Regression Evidence: the shipped claim text is byte-identical across the
split commit against its parent — the chain moved into
`reducerClaimBatchUpstreamCTEs()` / `reducerClaimBatchRepresentativeCTEs()`
(split only for the repository funlen cap; the wrapper rejoins them) with no
other change, verified by diffing the moved region against HEAD plus the
`TestClaimBatchFunnelQueryReusesShippedChain` verbatim guards, and by the
live C-1 candidate differential and C-3 storm staying green. The added cost is
one read-only counts SELECT per claim (no payload fetch, no locking, no row
update — `TestClaimBatchFunnelQueryIsReadOnly` pins the no-mutation
contract). Funnel-vs-claim cost on the stale-heavy fixture (1,200 stale rows, disposable PostgreSQL 18.6,
`EXPLAIN (ANALYZE, BUFFERS)`, rollback tx): funnel 94.4 ms / ~12.9 k buffer
hits with zero `Update`/`LockRows` nodes vs full claim 113.9 ms (see the
`funnel-vs-claim` log line in `TestClaimBatchGatePrecomputeStaleHeavyExplain`).
Neither
that fixture nor the gate-precompute fixture is production scale, so every
performance claim in the PR body stays net-of-funnel `NOT_CHECKED` until the
QA-lane sustained window (issue boxes 7-8) measures it. Since F1 the funnel
replays the candidate chain with a read-only sweep derivation instead of the
claim's mutating sweep, so the shipped per-claim total is claim-chain plus
funnel-chain replay — when the funnel is enabled, the service-side
`QueueClaimDuration` includes the funnel statement, which boxes 7-8 must
know. With the default-off gate the funnel never runs and the duration is
unchanged.

Observability Evidence: `eshu_dp_reducer_batch_claim_candidates`,
`eshu_dp_reducer_batch_claim_representatives`, and
`eshu_dp_reducer_batch_claim_drops_total{reason}` are registered in
`go/internal/telemetry/instruments.go`, emitted from
`ReducerQueue.captureClaimBatchFunnel`/`emitClaimBatchFunnel`, and covered by the
`telemetry-coverage-reducer-core.md` batch-claim row and the
`batch-claim-funnel-count` bucket row. `TestClaimBatchRecordsFunnelTelemetry`
pins the seven closed reasons and funnel reconciliation hermetically;
`TestClaimBatchFunnelReconcilesLive` proves reconciliation on the seeded
fixture with exact per-reason values: limited (candidates=22, admitted=2,
representatives=13, not_ready=1, not_representative=1, projector_drain=2,
semantic_cap=5, over_limit=11) and unlimited (admitted=13, the full
representative set including the holder of the shadowed holder-key).
