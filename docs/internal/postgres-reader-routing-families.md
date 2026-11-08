# Postgres query families, with reader verdicts (#10416)

Reader-safe means every row the family reads may come from the replica
under a 30-second staleness budget. Writer-only means it stays on the
writer. Split means part moves and part stays, as noted. `W*` means
writer or checkpoint-bounded fenced reader only — never a bare replica.
Family numbers match the arbiter ruling on
[eshu-hq/eshu#10416](https://github.com/eshu-hq/eshu/issues/10416). All
paths below are under `go/`; line numbers are entry points, not the
whole body. The superseded census draft is
[10416-reader-family-registry-draft](10416-reader-family-registry-draft.md).

## Safe to move (R)

| # | Family | Key files |
|---|--------|-----------|
| 2 | Facts filtered paging (query handlers only) | `internal/storage/postgres/facts_filtered.go:164,202,256,305` |
| 24 | Content reads (files, entities) | `internal/query/content_reader.go:33,39`, `internal/query/content_reader_entity.go:32` |
| 27 | Status snapshots and ledger reads, behind a writer checkpoint | `internal/storage/postgres/status.go:67`, `internal/runtime/postgres/status_reader.go:40` |
| 43 | IaC inventory read port | `internal/query/iac/inventory_postgres.go:22` |

## Split (S) — only the named half moves

| # | Family | Moves | Stays |
|---|--------|-------|-------|
| 1 | Facts core | None (dropped permanently) | All loads — handler-side and in-cycle — feed just-written generation projection where read-your-writes is structural (`internal/storage/postgres/facts.go:246,258`) |
| 16 | Readiness and observer gauges | Queue-depth and fence-count gauges (`internal/storage/postgres/queue_observer.go:237`, `internal/storage/postgres/corpus_fence_observer.go:101`) | Reads embedded in reducer claim SQL, and any gauge that ever gates scheduling |
| 28 | Changed-since | Query reads | Journal writes inside ack transactions |
| 32 | Search-vector index build | Pending-scope discovery (`internal/storage/postgres/eshu_search_vector_pending.go:114`) | Fenced batch commits (`internal/storage/postgres/eshu_search_vector_fenced_batch.go:81`), revalidated on the writer |
| 33 | Search documents | Query reads | Projection writes and scope-fence decisions |
| 36 | Alerts | Reads and unread cursors | Retirement garbage collection |
| 42 | Governance audit | `List` / `Summary` reads | `Append` writes (`internal/storage/postgres/governance/audit/store.go:184`) |

## Writer-only (W) — never move

Claims, leases, fences, and pins:
`internal/storage/postgres/projector_queue_claim_sql.go:14-52`,
`internal/storage/postgres/projector_queue_claim_pool_sql.go:19,171,203`,
`internal/storage/postgres/reducer_queue_claim_query.go:85,103`,
`internal/storage/postgres/workflow_control_sql.go:89,125`,
`internal/storage/postgres/workflow_claim_pacing.go:74`,
`internal/storage/postgres/control_scanner_claim_guard_sql.go:10`,
`internal/storage/postgres/control_scanner_heartbeat.go:25`,
`internal/storage/postgres/ingestion_claim_fence.go:53,79,105`, and
`internal/storage/postgres/coordination/coordination.go:18`. These run
inside writer transactions or row locks; a lagging copy could approve a
stale epoch.

Ack, heartbeat, fail, and retry: projector ack with its delta-baseline
fence read (`internal/storage/postgres/projector_queue_ack.go:29,92`,
`internal/storage/postgres/projector_queue_delta_baseline.go:302`),
reducer ack and replay
(`internal/storage/postgres/reducer_queue_ack.go:22,311`), batch paths
(`internal/storage/postgres/reducer_queue_batch.go:17,75,357`), redrive
and drift hooks
(`internal/storage/postgres/projector_queue_ack.go:345`,
`internal/storage/postgres/projector_queue_config_state_drift_trigger_hook.go:128`),
dead-letter and replay handling
(`internal/query/admin/store/replay.go:26`,
`internal/recovery/replay.go:323,325,370`), and phase-repair queueing
(`internal/storage/postgres/graph_projection_phase_repair_queue.go:132,151,190`).

Commits and projections: ingestion commits
(`internal/storage/postgres/ingestion.go:86,119,132`), fact upserts
(`internal/storage/postgres/facts.go:239`), content upsert, delete, and
reap (`internal/storage/postgres/content_writer.go:44`,
`internal/storage/postgres/content_writer_delete_batch.go:48,91,134,165`,
`internal/storage/postgres/content_writer_reap.go:196,244`,
`internal/storage/postgres/content_writer_sql.go:126,233`), retention
prunes
(`internal/storage/postgres/generation_retention_sql.go:163,269,414`),
retirement purges
(`internal/storage/postgres/repository_retirements_purge_sql.go:49`),
winners rebuilds
(`internal/storage/postgres/supply_chain_impact_winners_keyed_sql.go:40,214`),
image
identity, consumption keys, shared intents, schema and bootstrap locks,
and the supply-chain impact transaction
(`internal/reducer/supplychain/core/writer_retract.go:39`).

Reads that decide *what* happens: delta-guard counts feeding refusal
(`internal/storage/postgres/facts.go:202,223`), ingestion baselines and
unchanged-skip, the uncovered-writers probe feeding full-versus-delta
choice
(`internal/storage/postgres/generation_projected_commit.go:468`),
scanner discovery and liveness reads feeding advance and admission
(`internal/storage/postgres/control_scanner_discovery.go:221`,
`internal/storage/postgres/cloud_resource_liveness.go:33,47`), and
content reads inside writer transactions.

Auth (W*, checkpoint-bounded only):
`internal/scopedtoken/identity.go:98`, identity subjects and tokens,
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
