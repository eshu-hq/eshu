# maintenance

## Purpose

Runs the reducer's periodic side-runners: generation liveness recovery,
poison dead-letter liveness recovery (#4740), generation retention pruning,
the graph orphan sweep, the repo-dependency activation-gate decorators, and
the collector-readiness evidence summary resweep (#3466). This package moved
out of the flat `internal/reducer` root under issue #6061.

## Ownership boundary

This package owns the six side-runners/decorators listed above: their
policies, results, recoverer/pruner/sweeper contracts, run loops, and
telemetry recording. It does **not** own intent claiming, graph projection,
or the `Service.Run` side-runner startup loop itself (`Service.startSideRunners`
stays in the reducer root, since it wires every family's side-runner, not just
this one). It does not own `AcceptedGenerationLookup`, `AcceptedGenerationPrefetch`,
`SharedProjectionAcceptanceKey`/`SharedProjectionIntentRow`, or
`PartitionLeaseManager` as root logic -- those are root-owned contracts this
package mirrors locally (see Dependencies below) so it never imports
`internal/reducer`.

## Exported surface

| symbol | consumer |
|---|---|
| `GenerationLivenessRunner`, `GenerationLivenessPolicy`, `GenerationLivenessResult`, `GenerationLivenessRecovery`, `GenerationLivenessRecoverer`, `GenerationLivenessRunnerConfig` | `cmd/reducer` (`generation_liveness_wiring.go`, `config.go`); `Service.GenerationLivenessRunner` field |
| `PoisonLivenessRunner`, `PoisonLivenessPolicy`, `PoisonLivenessResult`, `PoisonLivenessRecoverer`, `PoisonLivenessRunnerConfig` | `cmd/reducer` (`poison_liveness_wiring.go`, `config.go`); `Service.PoisonLivenessRunner` field |
| `GenerationRetentionRunner`, `GenerationRetentionPolicy`, `GenerationRetentionResult`, `GenerationRetentionPruner`, `GenerationRetentionRunnerConfig` | `cmd/reducer` (`generation_retention_wiring.go`, `config.go`, `default_wiring_test.go`); `Service.GenerationRetentionRunner` field |
| `GraphOrphanSweepRunner`, `GraphOrphanSweepPolicy`, `GraphOrphanSweepResult`, `GraphOrphanSweeper`, `GraphOrphanSweepRunnerConfig`, `ErrGraphOrphanSweeperRequired` | `cmd/reducer` (`graph_orphan_sweep_wiring.go`, `config.go`); `Service.GraphOrphanSweepRunner` field |
| `RelationshipGenerationActiveLookup` | `internal/storage/postgres` (`NewRelationshipGenerationActiveLookup`); `cmd/reducer` (`newRepoDependencyProjectionRunner`) |
| `GateAcceptedGenerationOnActive`, `GateAcceptedGenerationPrefetchOnActive` | `cmd/reducer` (`newRepoDependencyProjectionRunner`) -- the reducer root's `RepoDependencyProjectionRunner.AcceptedGen`/`AcceptedGenPrefetch` fields stay root-typed, so `main_helpers.go` adapts across the two packages' `AcceptedGenerationPrefetch` spellings with two thin closures (see that file's comment) |
| `AcceptedGenerationLookup`, `AcceptedGenerationPrefetch` | no cross-package caller; local mirrors of `reducer.AcceptedGenerationLookup`/`AcceptedGenerationPrefetch` so this package's gate functions never import `internal/reducer` |
| `PartitionLeaseManager` | no cross-package caller; local mirror of `reducer.PartitionLeaseManager` so `GraphOrphanSweepRunner.LeaseManager` never imports `internal/reducer` -- satisfied structurally by whatever concrete lease store the root wires in |
| `InfraInventoryReconcileRunner`, `InfraInventoryReconcileRunnerConfig`, `InfraInventoryReconciler`, `InfraInventoryReconcileRequest`, `InfraInventoryReconcileBatch`, `InfraInventoryReconcileRepo` | `cmd/reducer` (`infra_inventory_reconcile_wiring.go` adapts `storage/postgres/infra/inventory.ReconcileCycle`); `Service.InfraInventoryReconcileRunner` field |
| `ActivationObligationRunner`, `ActivationObligationRunnerConfig`, `ActivationObligationStore`, `ActivationMaintainer`, `ActivationObligation`, `ActivationFinalizeResult`, `ActivationCatchUpPage`, `ActivationStats`, `ErrActivationLeaseLost`, `ErrActivationInapplicable`, `ErrActivationCatalogChanged`, `ActivationHoldError`, `HoldActivation`, `ActivationHoldReasons`, `ActivationHold*`, `ActivationOutcome*` | `internal/storage/postgres/activation` (`RunnerStore` implements the store port); `Service.ActivationObligationRunner` field; `postgres.ActivationMaintainer` implements `ActivationMaintainer`; `cmd/reducer` (`generation_liveness_wiring.go`) builds the runner when `ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED=true` (#7584) |
| `CollectorEvidenceSummaryMaintainer`, `CollectorEvidenceSummaryRebuilder`, `CollectorEvidenceSummaryLeaseManager`, `CollectorEvidenceFreshnessLookup`, `CollectorEvidenceSummaryDomain` | `cmd/reducer` (`main.go` wires `Service.CollectorEvidenceSummaryMaintainer` directly with this type) |

See `doc.go` for the godoc-rendered contract.

## Dependencies

`reducer/sharedintent`, `internal/telemetry`, `pkg/log`. Never `internal/reducer`.

`AcceptedGenerationLookup` and `AcceptedGenerationPrefetch` are aliases to the
unnamed function signatures that `reducer.AcceptedGenerationLookup` and
`reducer.AcceptedGenerationPrefetch` (`shared_projection_worker.go`) define
as named types. Because the local names are aliases rather than defined
types, a root-typed `AcceptedGenerationLookup` value is directly assignable
to this package's functions with no conversion. `PartitionLeaseManager` is a
plain interface mirroring `reducer.PartitionLeaseManager`
(`shared_projection_worker.go`)
method-for-method -- Go interfaces are satisfied structurally, so this needs
no conversion either. `AcceptedGenerationPrefetch`'s nested return type
(`AcceptedGenerationLookup`) means the two packages' `AcceptedGenerationPrefetch`
spellings are not directly interchangeable the way the lookup alone is; the one
call site that crosses the boundary (`cmd/reducer/main_helpers.go`) adapts with
two thin wrapper closures instead.

## Telemetry

| signal | dimensions |
|---|---|
| `eshu_dp_generation_liveness_recovered_total`, `eshu_dp_generation_liveness_superseded_total`, `eshu_dp_generation_liveness_failures_total` | -- |
| `eshu_dp_poison_liveness_recovered_total`, `eshu_dp_poison_liveness_failures_total` | -- |
| `eshu_dp_generation_retention_generations_pruned_total`, `eshu_dp_generation_retention_rows_pruned_total`, `eshu_dp_generation_retention_failures_total`, `eshu_dp_generation_retention_skipped_total`, `eshu_dp_generation_retention_duration_seconds`, `eshu_dp_generation_retention_batch_size`, `eshu_dp_generation_retention_oldest_eligible_age_seconds`, `eshu_dp_generation_retention_phase_duration_seconds`, `eshu_dp_generation_retention_scope_lock_hold_seconds`, `eshu_dp_generation_retention_over_limit_batches_total` | `eshu_dp_generation_retention_rows_pruned_total` by `table`, including the changed-since ledger tables `changed_since_links`, `changed_since_link_deltas`, `changed_since_link_bucket_counts` and `changed_since_activations` (#7127); `eshu_dp_generation_retention_skipped_total` by `reason` (`row_limit`, `row_limit_ledger` = deferred to a batch of its own); `eshu_dp_generation_retention_failures_total` by `reason` (`store_error`, or `key_index_unavailable` for an `ErrGenerationRetentionKeyIndexUnavailable` refusal); `eshu_dp_generation_retention_phase_duration_seconds` by `phase`; `eshu_dp_generation_retention_over_limit_batches_total` counts batches of one admitted over the limit by their ledger rows. The cycle log carries `changed_since_ledger_rows_pruned`, `rows_over_batch_row_limit` and `locked_scope_rows` (the scope rows held by the pruned batch, not the selection's full lock set; 1 for a batch of one narrowed to its own scope row); a WARN names it beside `changed_since_ledger_rows_counted` when the ledger delete differs from its pre-count |
| `eshu_dp_infra_inventory_reconcile_total`, `eshu_dp_infra_inventory_reconcile_duration_seconds`; span `reducer.infra_inventory_reconcile` | `eshu_dp_infra_inventory_reconcile_total` by `outcome` (`match`/`suspect`/`repaired`/`error`) |
| `eshu_dp_repo_dependency_gate_decisions_total` | `decision` (`bypassed`/`deferred_error`/`deferred_inactive`/`active`) |

`GraphOrphanSweepRunner` and `CollectorEvidenceSummaryMaintainer` register no
metrics of their own -- both record structured completion/failure logs
(`phase=reduction`, `failure_class=graph_orphan_sweep_error` on failure for
the sweep), and the sweep runner additionally emits a lease-release WARN
when its deferred bounded release fails. Both sweep logs also carry
`lease_ttl_seconds`
(`telemetry.LogKeyLeaseTTLSeconds`, registered in `telemetry.LogKeys()`):
the configured lease TTL on completion, and the TTL that would have guarded
the cycle on failure (pair with the error — the claim itself may have
errored before any lease was held); the release-failure WARN carries the
configured TTL after which the unreleased lease expires server-side.
The reducer registers
`eshu_dp_graph_orphan_nodes` and `eshu_dp_active_generations` as observable
gauges independently of these runners (see
`docs/public/observability/telemetry-coverage.md`), so neither row moved
here.

No-Regression Evidence: #6061 relocates this family's production logic
without changing it. Every hunk in the moved production files is one of
three kinds: a package clause; (in `accepted_generation_active_gate.go` only)
an identifier-requalification from the root's `SharedProjectionAcceptanceKey`/
`SharedProjectionIntentRow` aliases to `sharedintent.AcceptanceKey`/
`sharedintent.Row` directly; or one of the three locally-mirrored root
declarations this move adds (see Dependencies above) -- `AcceptedGenerationLookup`
and `AcceptedGenerationPrefetch`, both new in `accepted_generation_active_gate.go`,
and `PartitionLeaseManager`, new in `graph_orphan_sweep_runner.go`. Measured on
this branch: `go build ./...` exits 0, `go vet ./...` exits 0, `go test
./internal/reducer/... ./cmd/reducer ./internal/storage/postgres -count=1`
passes.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph, or
storage contract. The signals above are the same before and after the move.

## Gotchas / invariants

- `AcceptedGenerationPrefetch`'s nested `AcceptedGenerationLookup` return type
  breaks the otherwise-free alias interop between this package and the
  reducer root (see Dependencies) -- do not assume every alias here is a
  drop-in replacement for its root counterpart; the boundary adapter in
  `cmd/reducer/main_helpers.go` is load-bearing, not incidental.
- `PoisonLivenessRunner` only re-drives dead-letter rows when
  `PoisonLivenessRunnerConfig.AutoRetryEnabled` is true (the default posture is
  surface-only); the stuck-gauge that reports the poison class size is wired
  independently in `cmd/reducer` and stays active regardless.
- The repo-dependency activation gate (`GateAcceptedGenerationOnActive`)
  applies ONLY to source runs whose generation IDs are relationship
  generation IDs (`repo_dependency`/`repo_dependency:<scope>`); code-import and
  package-consumption source runs carry scope generation IDs that never
  appear in `relationship_generations`, so the gate must bypass them (B-13).
- `GraphOrphanSweepRunner` uses a single-owner Postgres partition lease
  (`graph_orphan_sweep` domain, partition 0/1) so concurrent reducer replicas
  never contend on the same static-label Cypher writes.
- `CollectorEvidenceSummaryMaintainer` reconciles via a full idempotent
  resweep (`RebuildAllCollectorEvidence`), not incremental per-scope dirty
  tracking, so it cannot miss a change class; the durable freshness guard caps
  cluster-wide resweeps at ~one per cadence regardless of replica count.
- `GenerationRetentionRunner` retries a pass that pruned nothing but reported
  skipped candidates soon (1m doubling per consecutive skipped-only pass,
  capped at the poll interval) instead of sleeping the full interval (#7398).
  Lock-held candidates stay invisible by design (the candidate SELECT uses
  SKIP LOCKED), so a lock-starved pass keeps the full sleep.

  No-Regression Evidence (#7398): baseline, a pass that found candidates but
  pruned none slept the full poll interval (default 1h), so a skipped-only
  streak ran 2 passes in its first 63 minutes (t=0, t=60m). After, the waits are
  1, 2, 4, 8, 16, 32 minutes, then the poll interval, so the same streak runs 7
  passes (t=0, 1, 3, 7, 15, 31, 63m) and then returns to one pass per poll
  interval. The extra cost is bounded at 6 passes per streak. Each pass is the
  existing candidate selection (SKIP LOCKED) and prune path: no new query, no
  Cypher, no new concurrency, still one goroutine, context cancellation
  unchanged. Measured with the unit harness (fake pruner, Go 1.27.1,
  darwin/arm64): `TestGenerationRetentionRunnerSkippedRetryBacksOffToPollInterval`
  asserts the 8-wait sequence above with a 1h poll interval, and
  `TestGenerationRetentionRunnerSkippedBackoffResetsAfterPrune` asserts the
  streak resets after a pruning pass. Not measured against live Postgres: the
  per-pass query cost is unchanged, only how often a blocked backlog is
  retried.

  Observability Evidence (#7398): no new signal is needed. Every pass already
  emits `eshu_dp_generation_retention_skipped_total{reason}` and the
  `generation retention cycle completed` log line with `skipped_total`,
  `skipped_by_reason` and `rows_over_batch_row_limit`. An operator sees the new
  cadence as that log line's interval shrinking from the poll interval to 1, 2,
  4 … minutes while skips persist, and returning to the poll interval once the
  streak is capped or a pass prunes.
- Test helpers (`acceptedGenerationFixed`, `reducerCounterValue`, `hasAttrs`)
  are duplicated from the reducer root by design; do not export a root test
  helper to reach it. The `Service.startSideRunners` wiring proof for each
  runner (`TestServiceStartsGenerationRetentionRunner`,
  `TestServiceStartsGraphOrphanSweepRunner`) stays in the reducer root for the
  same reason `TestServiceStartsSearchVectorBuildRunner` does -- `Service` and
  its unexported `startSideRunners` method are root-owned.

## Related docs

- `go/internal/reducer/README.md`
- `go/internal/reducer/recovery-runners.md`
- `go/internal/reducer/sharedintent/README.md`
- `docs/public/observability/telemetry-coverage.md`
