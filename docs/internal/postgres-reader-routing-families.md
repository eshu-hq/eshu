# Postgres query families, with reader verdicts (#10416)

Reader-safe means every row the family reads may come from the replica
under a 30-second staleness budget. Writer-only means it stays on the
writer. Split means part moves and part stays, as noted. `W*` means
writer or checkpoint-bounded fenced reader only — never a bare replica.
Family numbers match the arbiter ruling on
[eshu-hq/eshu#10416](https://github.com/eshu-hq/eshu/issues/10416). All
paths below are under `go/`; anchors name the file and symbol, never a
line number, so they survive edits. The superseded census draft is
[10416-reader-family-registry-draft](10416-reader-family-registry-draft.md).

## Safe to move (R)

| # | Family | Key files |
|---|--------|-----------|
| 2 | Facts filtered paging (query handlers only) | `go/internal/storage/postgres/facts_filtered.go` (`ListFactsByKind` variants) |
| 24 | Content reads (files, entities) | `go/internal/query/content_reader.go` (`NewContentReader`), `go/internal/query/content_reader_entity.go` (`GetEntityContent`) |
| 27 | Status snapshots and ledger reads, behind a writer checkpoint | `go/internal/storage/postgres/status.go` (`NewStatusStore`), `go/internal/runtime/postgres/status_reader.go` (`NewSnapshotStatusReader`) |
| 43 | IaC inventory read port | `go/internal/query/iac/inventory_postgres.go` (`PostgresIaCInventoryStore`) |

## Split (S) — only the named half moves

| # | Family | Moves | Stays |
|---|--------|-------|-------|
| 1 | Facts core | None (dropped permanently) | All loads — handler-side and in-cycle — feed just-written generation projection where read-your-writes is structural (`go/internal/storage/postgres/facts.go`: `LoadFacts`, `LoadProjectionFacts`) |
| 16 | Readiness and observer gauges | Queue-depth and fence-count gauges (`go/internal/storage/postgres/queue_observer.go`: `QueueDepths`, `go/internal/storage/postgres/corpus_fence_observer.go`: `CorpusFenceCounts`) | Reads embedded in reducer claim SQL, and any gauge that ever gates scheduling |
| 28 | Changed-since | Query reads | Journal writes inside ack transactions |
| 32 | Search-vector index build | Pending-scope discovery (`go/internal/storage/postgres/eshu_search_vector_pending.go`: `ListPendingSearchVectorScopes`) | Fenced batch commits (`go/internal/storage/postgres/eshu_search_vector_fenced_batch.go`: `upsertEshuSearchVectorMetadataBatchFenced`), revalidated on the writer |
| 33 | Search documents | Query reads | Projection writes and scope-fence decisions |
| 36 | Alerts | Reads and unread cursors | Retirement garbage collection |
| 42 | Governance audit | `List` / `Summary` reads | `Append` writes (`go/internal/storage/postgres/governance/audit/store.go`: `Append`) |

## Writer-only (W) — never move

Claims, leases, fences, and pins:
`go/internal/storage/postgres/projector_queue_claim_sql.go` (scope-claim fence and `claimProjectorWorkQuery`),
`go/internal/storage/postgres/projector_queue_claim_pool_sql.go` (`claimProjectorWorkPoolSQL`),
`go/internal/storage/postgres/reducer_queue_claim_query.go` (`claimReducerWorkQuery`),
`go/internal/storage/postgres/workflow_control_sql.go` (`heartbeatWorkflowClaimQuery`),
`go/internal/storage/postgres/workflow_claim_pacing.go` (`CheckClaimPacing`),
`go/internal/storage/postgres/control_scanner_claim_guard_sql.go` (`scannerDerivedClaimAuthority`),
`go/internal/storage/postgres/control_scanner_heartbeat.go` (`heartbeatDerivedScannerClaim`),
`go/internal/storage/postgres/ingestion_claim_fence.go` (`ClaimPlaneFence`), and
`go/internal/storage/postgres/coordination/coordination.go` (`Locker`). These run
inside writer transactions or row locks; a lagging copy could approve a
stale epoch.

Ack, heartbeat, fail, and retry: projector ack with its delta-baseline
fence read (`go/internal/storage/postgres/projector_queue_ack.go`: `Ack`,
`go/internal/storage/postgres/projector_queue_delta_baseline.go`: `checkAckDeltaBaseline`),
reducer ack and replay
(`go/internal/storage/postgres/reducer_queue_ack.go`: `ackReducerWorkQuery`), batch paths
(`go/internal/storage/postgres/reducer_queue_batch.go`: `ClaimBatch`), redrive
and drift hooks
(`go/internal/storage/postgres/projector_queue_ack.go`: `runCrossplaneRedriveHook`,
`go/internal/storage/postgres/projector_queue_config_state_drift_trigger_hook.go`: `runConfigStateDriftTriggerHook`),
dead-letter and replay handling
(`go/internal/query/admin/store/replay.go`: `ReplayFailedWorkItems`,
`go/internal/recovery/replay.go`: `ReplayStore`, `ReplayFailed`), and phase-repair queueing
(`go/internal/storage/postgres/graph_projection_phase_repair_queue.go`: `Enqueue`, `ListDue`, `Delete`).

Commits and projections: ingestion commits
(`go/internal/storage/postgres/ingestion.go`: `NewIngestionStore`, `CommitScopeGeneration`, `CommitClaimedScopeGeneration`), fact upserts
(`go/internal/storage/postgres/facts.go`: `UpsertFacts`), content upsert, delete, and
reap (`go/internal/storage/postgres/content_writer.go`: `NewContentWriter`,
`go/internal/storage/postgres/content_writer_delete_batch.go` (entity/file/reference chunk deleters),
`go/internal/storage/postgres/content_writer_reap.go`: `reapStaleContentEntities`,
`go/internal/storage/postgres/content_writer_sql.go`: `deleteContentFileQuery`, fingerprint-band consts), retention
prunes
(`go/internal/storage/postgres/generation_retention_sql.go`: `generationRetentionCandidateQuery`, shared-intent and scope-generation delete consts),
retirement purges
(`go/internal/storage/postgres/repository_retirements_purge_sql.go`: purge const block),
winners rebuilds
(`go/internal/storage/postgres/supply_chain_impact_winners_keyed_sql.go`: `consumeWinnersDirtyLogSQL`),
image
identity, consumption keys, shared intents, schema and bootstrap locks,
and the supply-chain impact transaction
(`go/internal/reducer/supplychain/core/writer_retract.go`: `runSupplyChainImpactTx`).

Reads that decide *what* happens: delta-guard counts feeding refusal
(`go/internal/storage/postgres/facts.go`: `countGuardFacts`, `countGuardFactsGeneration`), ingestion baselines and
unchanged-skip, the uncovered-writers probe feeding full-versus-delta
choice
(`go/internal/storage/postgres/generation_projected_commit.go`: `UncoveredProjectionWriters`),
scanner discovery and liveness reads feeding advance and admission
(`go/internal/storage/postgres/control_scanner_discovery.go`: `ListAuthorizedRunningImageObservationsPage`,
`go/internal/storage/postgres/cloud_resource_liveness.go`: `undrainedCloudAdmissionWhere`), and
content reads inside writer transactions.

Auth (W*, checkpoint-bounded only):
`go/internal/scopedtoken/identity.go` (`ResolveScopedToken`), identity subjects and tokens,
browser sessions, and OAuth flows. A stale replica changes allow into
deny or revives a revoked token.

Findings and grouped inventory reads stay on the fenced reader: a
synchronous handler cannot defer, and a stale page could serve
suppressed findings as active. They already run on the standby through
the fenced reader, so moving them would save near zero.

Not production routing candidates: benchmark harnesses, latency-gate
seeds, embedded test databases, local-supervisor CLIs, AST guards, and
bench schedulers (family 44).

## Why the obvious candidates stayed

Three families looked movable and failed the rule on inspection: the
readiness scan whose output anchors the fence timer, the scan-then-list
repair read that is not replay-safe across workers from a lagging
copy, and the winners-audit token reader whose verdict consumes
per-worker state. Details and file lines are in the ruling on
[eshu-hq/eshu#10416](https://github.com/eshu-hq/eshu/issues/10416).
