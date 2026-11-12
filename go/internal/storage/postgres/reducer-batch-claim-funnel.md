# Reducer batch-claim funnel telemetry (#10565 box 6)

This companion note keeps the package README focused while preserving the
funnel design, evidence, and operator contract for the batch-claim admission
telemetry future claim changes must respect.

`ClaimBatch` (`reducer_queue_batch.go`) recorded only the admitted batch size
(`eshu_dp_reducer_batch_claim_size`). The issue requires the full admission
funnel — candidates in, representatives after the fence, and drops by reason —
as the operator signal for the claim-admission ceiling work. Every claim now
runs one extra lock-free counts query (`reducerClaimBatchFunnelQuery`,
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
lands in both or the substring guards fail. The live-lease fence derives as
pre-fence minus representatives (the candidate CTE's only additional filter);
the limit trim and lock-time loss derive in Go (`over_limit` past the batch
limit, `lock_lost` as the within-limit shortfall against the admitted batch).
Funnel failure is best-effort: it records nothing and never fails the claim,
and queues without instruments skip the second query entirely. The shared
chain keeps the claim's `$1..$7` numbering, so the funnel's final SELECT
carries a tautological `$3`/`$4` WHERE clause: Postgres rejects parameters
whose type it cannot infer (42P18) and an unreferenced placeholder has no
type.

No-Regression Evidence: the shipped claim text is byte-identical — the chain
moved into `reducerClaimBatchUpstreamCTEs()` /
`reducerClaimBatchRepresentativeCTEs()` (split only for the repository
funlen cap; the wrapper rejoins them) with no other change, verified by
diffing the moved region against HEAD plus the
`TestClaimBatchFunnelQueryReusesShippedChain` verbatim guards, and by the
live C-1 candidate differential and C-3 storm staying green. The added cost is
one counts-only SELECT per claim (no payload fetch, no locking, no row
update), measured at ~0.85ms execution / ~3.0ms planning against ~1.5ms /
~2.6ms for the claim itself on the 21-row gate-precompute fixture
(disposable PostgreSQL 18.6 via `EXPLAIN (ANALYZE, BUFFERS)`): about half a
claim on a tiny backlog where fixed planning dominates. Both scale with the
same base scan and the funnel never takes locks. Production-scale
before/after belongs to the QA-lane sustained window (issue boxes 7-8).

Observability Evidence: `eshu_dp_reducer_batch_claim_candidates`,
`eshu_dp_reducer_batch_claim_representatives`, and
`eshu_dp_reducer_batch_claim_drops_total{reason}` are registered in
`go/internal/telemetry/instruments.go`, emitted from
`ReducerQueue.recordClaimBatchFunnel`, and covered by the
`telemetry-coverage-reducer-core.md` batch-claim row and the
`batch-claim-funnel-count` bucket row. `TestClaimBatchRecordsFunnelTelemetry`
pins the seven closed reasons and funnel reconciliation hermetically;
`TestClaimBatchFunnelReconcilesLive` proves reconciliation on the seeded
fixture (candidates=14, admitted=2, representatives=6, drops summing to 12).
