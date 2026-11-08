# Reader Family Registry — DRAFT (eshu-hq/eshu#10416)

Draft census only. No production code changed. Each family records its store
port, call sites, reader-safe / writer-required verdict against the four
clauses, a staleness budget with rationale, and the heaviest query observed
in code or comments (no load was run against production).

Clause shorthand: (i) SELECT-only through `db.Queryer`; (ii) result never
feeds a fence/claim/lease/pin/CAS/generation-commit or any write decision in
the same cycle; (iii) seconds-stale input changes only WHEN the next cycle
acts, never WHAT is committed; (iv) idempotent under stale-snapshot replay.

## 1. Connection census — one pool per component today

| Component | Open site | Route |
|---|---|---|
| reducer | `go/cmd/reducer/run.go:78` via `runtimecfg.OpenPostgresForWorkload(..., PostgresWorkloadRuntime)` | single writer pool |
| reducer secret-lines lock pool | `go/cmd/reducer/run.go:343` (max 1 conn, repair only) | writer pool |
| projector | `go/cmd/projector/main.go:90`, same opener, runtime workload | single writer pool |
| ingester | `go/cmd/ingester/main.go:85`, same opener | single writer pool |
| workflow-coordinator | `go/cmd/workflow-coordinator/main.go:99`, coordinator workload → control route | single pool |
| collectors (git, cicd-run, sbom, security-alerts, oci, package-registry, document/media-import, grafana, mimir, pagerduty, component-extension) | same shared writer opener, runtime workload | single writer pool |
| admin-status | same shared writer opener | single writer pool |
| API / MCP | `go/internal/runtime/postgres/access.go` — `Open` builds split pools (`Writer()` at :237, `Reader()` at :242, reader forced `default_transaction_read_only=on` at :174, freshness fence on borrow) | writer + reader already split |
| advisory producer | `PostgresRouteProducer`, separate `ESHU_POSTGRES_PRODUCER_DSN` (`go/internal/runtime/role_open.go`) | already split |

So the movement is: reducer, projector, ingester, coordinator, collectors —
everything hanging off the one shared writer pool — have no reader handle yet.
The API/MCP `Access` type and the `WithReadStore` constructors below are the
pattern to copy.

## 2. Reader-safe families (all four clauses hold)

| # | Family | Store port | Call sites | Staleness budget + rationale | Heaviest query |
|---|---|---|---|---|---|
| F1 | Winners serving reads (findings list/explain, grouped inventory, maintainer watermark) | `FindingQueryer` / `db.Queryer` (`go/internal/query/supply/chain/impact/findings.go`: `NewPostgresFindingStoreWithReadStore`; `grouped_inventory_postgres.go`: `NewPostgresGroupedInventoryStoreWithReadStore`; watermark `SelectWinnersWatermarkQuery` in `findings.go`) | API query handlers; reducer maintainer populates, never reads-for-write here | 300 s — keyed resweep + audit backstop bound drift; watermark-gated; winners vs legacy output byte-identical | legacy `ListFindingsQuery` ROW_NUMBER dedup sort over the filtered set (being replaced by the O(page) `ListFindingsFromWinnersQuery` in `findings_queries.go`) |
| F2 | Queue-depth gauges | `QueueObserverStore`, `db.Queryer` (`go/internal/storage/postgres/queue_observer.go`: `NewQueueObserverStore`) | `go/cmd/reducer/postgres_gauge_wiring.go` (gauges only) | 60 s — sampled gauges; move only WHEN a dashboard/autoscaler reacts, never WHAT is committed | `SourceQueueDepths` multi-group aggregate in `queue_observer.go` |
| F3 | Status snapshot reads | `db.ReadStore` (`go/internal/runtime/postgres/status_reader.go`: `NewSnapshotStatusReader`) | API/MCP status surface | 60 s — point-in-time status; snapshot already repeatable-read | full status snapshot (`ReadStatusSnapshot` in `status_reader.go`) |
| F4 | Decision/evidence admin reads | `DecisionReader`, `db.Queryer` (`go/internal/storage/postgres/decisions/reader.go`: `NewDecisionReader`) | `go/internal/query/admin/store/` | 300 s — human/audit surface, no write decision consumes it | `listDecisionsSQL` filtered admin scan in `decisions/reader.go` |
| F5 | Repository retirement reads | `RetirementStore`, `db.Queryer` (`go/internal/storage/postgres/repository_retirements.go`: `NewRetirementStore`) | `go/cmd/api/wiring_router.go` | 300 s — admin read surface | retirement listing scan |
| F6 | Repository page-selection reads | `PageSelectionStore`, `db.Queryer` (`go/internal/storage/postgres/repository_selection_batch.go`: `NewPageSelectionStore`) | `go/cmd/api/wiring_router.go` | 300 s — pagination aid for callers, never a commit input | bounded page query |

## 3. Writer-required families (failing clause named)

| # | Family | Store port | Call sites | Failing clause |
|---|---|---|---|---|
| W1 | Readiness producer scans (`RuntimeProducerStates`, runs-on intents) | `db.Queryer` (`readiness/producer/store.go:121-122`) — passes (i) | reducer readiness; `ImpactProducerState` (`impact_store.go:67`) via `go/cmd/reducer/wiring_handlers.go` | (ii) — `newest_in_flight_workable_at` is the fence's elapsed-bound anchor (comment on `RuntimeProducerStatesQuery`); a stale read moves the fence |
| W2 | Relationship-generation authority lookups (active / complete / incomplete scopes) | checker adapters (`accepted_generation.go:43-57`) | `go/cmd/reducer/main.go:115-122` repo-dependency lane | (ii) — gate graph-projection authority per row |
| W3 | Resolution producer lookup | `db.Queryer` (`accepted_generation.go:162`) — passes (i) | `go/cmd/reducer/main.go:124`, `crossrepo.EvaluateOwnGate` | (ii) — own-gate verdict defers or admits the cycle |
| W4 | Accepted-generation lookup + prefetch | `db.ExecQueryer` (`accepted_generation.go:194,220`) | `go/cmd/reducer/main.go:129,462,512` | (i) + (ii) — acceptance fence input on the hot path |
| W5 | Generation freshness / active / prior checks | `db.ExecQueryer` (`generation_freshness.go:169,236,267,299`) | `go/cmd/reducer/main.go:192,329-332,382`, `wiring_handlers.go:181` | (i) + (ii) — materializer writer gates |
| W6 | Generation liveness recovery | `db.ExecQueryer` (`generation_liveness.go:90`) | `generation_liveness_wiring.go:84,131` (`RecoverWedgedGenerations`, :101) | (i) + (ii) — recovery writes |
| W7 | Fencing token issue + protected-scan authorize/publish + policy pins | tx (`supply_chain_impact_canonical_winners_store.go:210-353,431`) | supply-chain publish path | (i) + (ii) — mints fencing tokens, pins, publishes |
| W8 | Collector evidence summary maintainer | `db.ExecQueryer` (`collector_evidence_summary.go:138`) | `go/cmd/reducer/main.go:501-505` (Rebuilder + Freshness + LeaseOwner) | (i) + (ii) — lease claim in the cycle |
| W9 | Winners audit + repair queueing | `db.Queryer` port but `AuditWinners` demands `db.Beginner` (`supply_chain_impact_winners_audit.go:105-123`; temp table + `queueWinnersAuditRepairsSQL`) | `reducer/supplychain/core/winners_keyed_maintainer.go` | (i) + (ii) — audit queues repairs under tx; the candidate-scan SELECT alone is the splittable half |
| W10 | Winners resweep claim + rebuild | claim SQL + rebuild writes (`supply_chain_impact_canonical_winners_rebuild.go:182-249`) | winners keyed maintainer | (i) + (ii) — claim fence + table writes |
| W11 | Retention work-item sweeper | `Beginner + Queryer` (`retention/work/items/sweeper.go:21-24,44,53`) | retention runner | (i) + (ii) — SELECT feeds deletes in the same tx |
| W12 | Generation retention prune | `db.ExecQueryer` (`generation_retention.go:59,75`) | `go/cmd/reducer/generation_retention_wiring.go:127` | (i) + (ii) — candidate SELECT feeds prune deletes |
| W13 | Search-index orphan / superseded sweeps | `Beginner + Queryer` (`search/index/retention/orphan.go:25-28`) | `generation_retention_wiring.go:134-136` | (i) + (ii) — census SELECT feeds deletes |
| W14 | Graph orphan sweep | cypher `Executor + Reader` (`storage/cypher/sweep/orphan/store.go:134`) | `go/cmd/reducer/graph_orphan_sweep_wiring.go:46` | (i) + (ii) — graph deletes; also not the PG writer |
| W15 | Search-document sweeper | pending lister on `ExecQueryer` store (`eshu_search_document_pending.go:61`) + intent writes (`projector/eshu_search_document_sweeper.go:47`) | `go/cmd/reducer/search_document_sweeper.go:24` | (i) + (ii) — list feeds enqueue |
| W16 | Config-drift catchup sweeper | lister + `ReducerIntentWriter` (`projector/config_state_drift_catchup_sweeper.go:100`) | `go/cmd/reducer/config_state_drift_catchup_sweeper.go:36` | (ii) — catchup writes intents in the same cycle |
| W17 | Crossplane redrive sweeper | state store writes (`cmd/projector/crossplane_redrive_catchup.go:77`) | `go/cmd/projector/runtime_wiring.go:101` | (i) + (ii) — `SweepBatch` mutates redrive state |
| W18 | Freshness links runner | journal writes, failure records, orphan deletes (`reducer/freshness/links/runner.go:173,278`) | reducer links loop | (ii) — observations commit retries/deletes this cycle |
| W19 | Vector build-ready / pending scans | `db.Executor` / `db.ExecQueryer` (`eshu_search_vector_build_ready.go:56`, `eshu_search_vector_pending.go:108`) | `go/cmd/reducer/search_vector_build_wiring.go` | (i) — write-capable ports; selection feeds the build |
| W20 | ECS declaration census + container-image scope state | `db.Queryer` (`ecs_binding_declaration_census.go:74`; `container_image_identity_scope_state.go:35`) — pass (i) | `go/cmd/reducer/main.go:349` ("census ... feed the [materialization]"); `service_materialization.go:64` | (ii) — census feeds service materialization writes |

## 4. Top-3 biggest wins (movable, ranked)

1. **Winners serving reads (F1).** The `WithReadStore` constructors already
   exist and the read is O(page) on the maintained table; switching the API
   findings path to the replica is close to a config flip once the maintainer
   watermark is replica-visible. Kills the legacy ROW_NUMBER dedup sort on the
   writer.
2. **Winners audit candidate scan (W9 SELECT half).** `AuditWinners`
   recomputes the whole table (`createWinnersAuditCandidatesSQL` — full
   recompute + full-join diff, the 60 s+ scan class from the issue) only to
   diff and queue repairs. Splitting the lock-free candidate scan onto the
   reader and verifying under the writer lock (the shape the code already
   uses for "changed between read and lock") moves the heavy half off the
   writer without touching fencing.
3. **Queue-depth / status / admin reads (F2–F6).** Individually cheap but
   constant (gauges tick, status polls, admin scans); all already behind
   `db.Queryer`/`db.ReadStore` ports, so they can move as one batch with no
   logic change.

## 5. NOT_CHECKED (explicitly unverified — do not route on this draft alone)

- Changed-since serving path (`query/freshness/`) backing store + feed: not traced.
- `supply_chain_impact_authorized_targets.go` (`db.Queryer` helpers) and
  `control_scanner_targets_prove.go` consumers: not traced; authorization
  adjacency means default writer-required until proven.
- Projector/reducer queue claim reads (claim path): not audited here; assumed
  writer-required, needs its own pass.
- Which API handlers already use `Access.Reader()` vs `Writer()`: not audited.
- Winners keyed resweep tick interval and audit cadence: budgets above assume
  minutes; confirm before wiring.
- Replica lag under load and writer-CPU before/after: per-issue follow-up
  items, not measured here (no load run per task constraints).

## 6. Commands run (read-only)

- `git branch --show-current`, `git rev-parse HEAD`, `git status --short`
- `rg -n "sql.Open"`, `rg -n "stdlib.OpenDB"`, `rg -n "OpenPostgresForWorkload"`
  across `go/cmd go/internal` (non-test)
- `rg -ln "db.Queryer"` + constructor-signature pass over
  `go/internal/storage/postgres/*.go`, `readiness/producer/`,
  `retention/work/items/`, `decisions/`, `queue_observer.go`,
  `runtime/postgres/`
- `rg -ln -i "winner|materialized|sweeper"` + reads of `findings.go`,
  `findings_queries.go`, `grouped_inventory_postgres.go`,
  `supply_chain_impact_canonical_winners_{rebuild,store,audit}.go`,
  `readiness/producer/store.go`
- Caller tracing via `rg -ln "New<S…>Store|AuditWinners|ResweepWinners|QueueDepths("`
  in `go/cmd/reducer`, `go/cmd/projector`, `go/cmd/api`
