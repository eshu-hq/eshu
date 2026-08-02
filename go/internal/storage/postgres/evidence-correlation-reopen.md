# Evidence: correlation reducer reopen (rc-1 deployable-unit)

Scope: `ReopenSucceededReducerWorkItems` (`ingestion_reopen_correlation.go`) and
its bootstrap-index `correlation_reopen` phase. It replays succeeded additive
correlation reducer work items (initially `deployable_unit_correlation`) during
deferred maintenance so they re-run once the resolved `DEPLOYS_FROM`
relationships they consume exist. This generalizes the proven
`ReopenDeploymentMappingWorkItems` / `ReopenCodeImportRepoEdgeWorkItems` reopens.

Superseded by the cross-scope correlation reopen section in `README.md` (#5846).
The bootstrap-index phase described here was, until that change, the ONLY caller:
the ingester's `RunDeferredRelationshipMaintenance` replayed just the two
relationship domains, so under normal ingestion these correlation domains were
never replayed at all. The remaining blanket-replay domain list now lives in
`CrossScopeCorrelationReopenDomains` and both runtimes consume it. The identity
-> CI/CD -> supply-chain chain moved to the durable producer-completion queue in
#5740, because unordered maintenance could leave a quiet corpus one link short.
Read the README section for the current split, per-drain bound, and
completion-event lease/fanout contract; the numbers below are the original
single-domain bootstrap-only baseline.

## Performance Evidence

- No-Regression Evidence: this adds no hot-path query. The reopen reuses the
  existing `ReducerQueue.ReopenSucceeded` guarded UPDATE
  (`reopenSucceededReducerWorkQuery`, `WHERE status = 'succeeded'`), one
  statement per succeeded work item, and the list query is the
  domain-parameterized form of the proven
  `listSucceededCodeImportRepoEdgeWorkItemsQuery`
  (`stage='reducer' AND domain=$1 AND status='succeeded'`).
- Baseline: the B-7 golden-corpus gate before this change (single
  deferred-maintenance + drain pass) — `rc-1 CORRELATES_DEPLOYABLE_UNIT = 0`.
- After: the gate with the correlation reopen + two maintenance/drain cycles —
  `rc-1 CORRELATES_DEPLOYABLE_UNIT = 1` (`deployable-config -> deployable-source`),
  `pipeline_wall_time` PASS at elapsed 34s against the 1800s ceiling (2.0x of the
  15m baseline), `fact_work_items_residual = 0` (no dead-letters) on every drain.
- Backend/version: NornicDB (compose default), Postgres 16 (compose), 10-repo
  synthetic corpus. Input shape: bounded — one reopen per repo that produced a
  succeeded `deployable_unit_correlation` work item.
- Why safe: reopen only transitions rows still in `succeeded`, so it is
  idempotent across the two maintenance passes; the bounded work-item count and
  the unchanged drain wall-time (34s, far under budget) show the added second
  cycle does not regress the repo-scale contract.

## Observability

- Observability Evidence: new counter `eshu_dp_correlation_reopened_total` (Int64, `domain` attribute),
  emitted from `ReopenSucceededReducerWorkItems`, registered in
  `go/internal/telemetry/instruments.go`, documented in
  `docs/public/observability/telemetry-coverage.md` (phase publish stage). It
  lets an operator graph how many correlation work items were replayed per domain
  per maintenance pass, mirroring the deployment_mapping and code_import reopen
  counters.
