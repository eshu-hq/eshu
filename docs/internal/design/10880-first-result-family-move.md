# Moving the first reducer-result family out of the fact table (#10880)

Status: **Design ruling** (arbiter `arbiter-eshu`, 2026-11-20); slices
0–2 executed 2026-11-20/21, slices 3–4 dispositioned (see "Execution
record" at the end). This record decides which family moves first, the
table pattern it follows, where the answering-store declaration lives,
and the slice order. It approves no schema change by itself; each
slice still needs its own proof (below).

Refs #10880. Part of #10860 (phase 3, step 1). Governing rule: decision D5
as rewritten in
[the #10861 ruling](../arbiter/10861-reducer-execution-model-ruling-2026-11-18.md)
— a result goes into the graph only when a declared read traverses
relationships with depth or fan-out not fixed at write time; fixed-depth
joins, lists, counts, and filters go into a table made for their reads;
every one of the 162 capabilities declares its answering store or stores,
and a check fails when a read uses a store it did not declare.

RULING: supply-chain impact findings move first, by completing the
half-finished winners-table move — payload joins the winners table, readers
stop joining `fact_records`, the reducer stops writing finding facts, and
the ten fact-table indexes drop last, after latency proof.

## Acceptance sweep against `origin/main` (2026-11-20)

| #10880 criterion | State today | Evidence |
| --- | --- | --- |
| Answering store for all 162 capabilities + failing check | **Not met** | No `answering_store` anywhere outside the ruling text; generated entries carry only `capability, display_name, maturity, derived_maturity, surfaces, profiles, proof_signals, console, authorization` |
| One family moved to its own table | **Half met for impact findings** | `supply_chain_impact_canonical_winners` exists, but the list read still joins `fact_records` for the payload |
| Golden-corpus API/MCP equality | Harness exists, proof not run for a move | `scripts/verify-golden-corpus-gate.sh` (#3800) |
| Read-latency proof before index removal | Harness exists | `scripts/verify-read-api-latency-gate.sh` (#6797) + `testdata/benchmarks/read-api-route-budgets.txt` |
| Before/after fact updates, index count, save time | Not recorded | Ten impact indexes on `fact_records` counted below |
| Migration order for remaining families | Recorded by this document | "Remaining families" section |

## Decision 1: supply-chain impact findings move first

The recommendation in #10880 is confirmed on inspected evidence:

- **Cost.** Supply-chain impact is 51% of reducer handler time
  ([10861 design](10861-reducer-execution-model.md), decision 3 table).
  It owns ten dedicated partial expression indexes on `fact_records` —
  the largest special-purpose set found:
  `fact_records_supply_chain_impact_active_scan_idx`,
  `_advisory_lookup_idx`, `_finding_id_idx`, `_group_members_idx`,
  `_lookup_idx`, `_package_lookup_idx`, `_priority_lookup_idx`,
  `_repository_lookup_idx`, `_status_lookup_idx`, `_winners_keyed_idx`
  (DDL in `migrations/003_fact_records.sql`, `084_…`, `288_…`, `289_…`,
  `291_…`, and `schema_fact_records_supply_chain_impact_indexes.go`).
  Every collector save pays to maintain all ten.
- **Write path (verified).** `PostgresSupplyChainImpactWriter.
  WriteSupplyChainImpactFindings`
  (`WriteSupplyChainImpactFindings` in `go/internal/reducer/supplychain/core/writer.go`) encodes each
  finding as `reducerderivedv1.SupplyChainImpactFinding`
  (`sdk/go/factschema/decode_reducerderived.go:13,40`) and batch-upserts
  versioned rows into `fact_records` via `go/internal/reducer/factwrite`
  (`batch_insert.go`, `ON CONFLICT (fact_id)` with a fencing-token guard).
  Upsert and retraction commit together per (scope, generation). The
  kind is governed in `ReducerSupplyChainImpactFindingFactKind` in `go/internal/facts/reducer_derived.go` and the
  `reducer_derived` family declaration (same file, line 96).
- **Read path (verified).** `go/internal/query/supply/chain/impact/`
  serves list, count/inventory aggregates, and explain. The list read
  `ListFindingsFromWinnersQuery` (`findings_queries.go:127`) filters on
  the winners table but still `JOIN fact_records AS refetch ON
  refetch.fact_id = page.winner_fact_id` (line 277) for the payload —
  so the fact rows cannot be removed and the indexes cannot drop.
  `go/internal/query/read-models.md` documents the same split (line 293:
  aggregates "re-use the existing partial indexes on `fact_records`").
  Security-alert reconciliations are a **separate** family
  (`reducer_security_alert_reconciliation`,
  `factKind` in `go/internal/query/supply/chain/alerts/store.go`) and are not part
  of this move.
- **Graph absent (verified).** No projector Cypher writes impact
  findings; `go/internal/projector/runtime/` only queues
  `DomainSupplyChainImpact` reducer intents. Every declared read is a
  fixed-depth list, count, grouped count, or single-finding explain with
  evidence hydration by fact id. Under D5 this is a table family, full
  stop. Capabilities served: `supply_chain.impact_findings.list`,
  `supply_chain.impact_findings.aggregate`,
  `supply_chain.impact_explanation.read` (plus advisory-evidence seeding
  from impact anchors, which stays a collector-fact read).

Rejected alternative: a brand-new findings table beside winners. Winners
already carries the canonical dedup key, every filter column, its own
seven indexes, a dirty-log keyed resweep, and an audit. A second table
would duplicate that machinery and add a second source of truth. The
move completes winners instead.

## The three existing precedents, and the pattern to follow

1. **Winners read model (derived table rebuilt from facts).** DDL home:
   numbered migration `migrations/033_supply_chain_impact_canonical_winners.sql`
   (typed columns, `canonical_key` primary key, seven read indexes).
   Maintenance: `SupplyChainImpactWinnersStore.ResweepWinners`
   (`supply_chain_impact_canonical_winners_rebuild.go:203`) reconciles
   the whole table to the active fact set in **one atomic statement**,
   writes only changed rows (#9774), deletes non-winners, and always
   stamps the `supply_chain_impact_winners_materialization` singleton
   watermark, single-flighted by `pg_try_advisory_xact_lock`. A dirty
   log (migration 272 onward) feeds keyed incremental resweeps. Lesson:
   rebuild-from-facts + watermark is the backfill and the backstop; no
   dual-write is needed while facts remain the input.
2. **Container image identity support store (typed table becomes the
   source of truth).** DDL home: `migrations/092_container_image_identity_support_store.sql`
   (`container_image_identity_supports`, `_support_sets`,
   `_scope_state`) plus `092b_…` views/functions
   (`container_image_identity_current_supports`,
   `container_image_identity_current_support_facts_for(…)`). Cutover is
   per scope: `scope_state.active_set_id` flips read authority, and the
   trigger `reject_container_image_identity_fact_record_write` then
   **refuses** legacy `fact_records` writes for that scope (092, line
   408). Lesson: when the fact write stops, stop it in the database,
   not by convention; readers switch through one named view/function.
3. **Search index and content store (purpose-built tables with their
   own indexes).** `migrations/003b_eshu_search_index.sql`
   (`eshu_search_index_documents/terms/stats`; terms
   `PARTITION BY HASH (scope_id)` MODULUS 64 — the D6 precedent) and
   `schema_content_store.go` (`content_files`, `content_entities`,
   `content_file_references`, `repository_refs`, DDL as a Go constant
   assembled in `schema.go`). Lesson: a read family owns its indexes;
   the fact table keeps none of them.

**Pattern for this move (P1 rebuild + P2 cutover):**

- DDL home: a **new numbered migration** (landed as 331/332; main
  owned 325-330 by merge time. Never edit a shipped `.sql` —
  migrations README invariant), plus the
  `schema_order_test.go` / schema lockstep entries every migration
  carries. No new table: the migration adds `payload JSONB` (and the
  winner's `source_confidence`) to
  `supply_chain_impact_canonical_winners`.
- Backfill/rebuild: extend `supplyChainImpactWinnerSelectSQL` so the
  existing atomic resweep carries the winner payload; first resweep
  after deploy is the backfill. Watermark semantics unchanged.
- Reader switch: list, aggregates, and explain read winners only; the
  `fact_records` payload join is deleted. Explain keeps hydrating
  **collector** evidence facts by id from `fact_records` — that read
  is declared as `fact_records_collector` and does not keep any impact
  index alive. Suppression overlay logic in `findings_queries.go`
  moves onto the stored payload unchanged.
- Write stop (separate, later slice): the reducer keeps writing
  finding facts until readers are proven equal; then
  `WriteSupplyChainImpactFindings` writes the winners/source rows
  directly, a P2-style reject trigger bars new
  `reducer_supply_chain_impact_finding` fact writes, and old finding
  rows are retired by the normal generation-retention path (they are
  already excluded from collector counts by
  `linksfreshnessstore.ExcludeReducerDerivedKinds`).
- Index drop is **last** and conditional (see slice 4). While finding
  facts are still written, `fact_records` keeps the ten indexes; the
  drop migration lists exactly the ten names above and no others.

One semantic risk is named, not waved away: winners holds one row per
canonical (deduped) finding, while `fact_records` holds one row per
(scope, generation) support. Readers already collapse to canonical
(`read-models.md` line 404) and grant-filter on
`winner_scope_id`/`repository_id`, so behavior should be identical —
but slice 2 must **prove** it on scoped-token and multi-support cases,
not assume it. If proof shows a grant-visible support being hidden by
the canonical pick, the fallback is a supports table in the P2 shape,
and that finding comes back to the arbiter before slice 3 starts.

## Decision 2: graph-absent families — the result table is the truth

Per D5, the graph gains a relationship **only** when a declared read
traverses with depth or fan-out not fixed at write time. "The reducer
result is the only answer today" is not by itself a reason to add graph
edges; for fixed-depth reads the result table **is** the source of
truth, rebuildable from collector facts. Two guardrails from the
ruling stand: both stores rebuild from facts, and no family becomes
graph-only until #10883 has timed a full graph rebuild at 9,000+
scopes. The `exact` truth label is unchanged by this decision — it
already means "authoritative graph truth **or durable semantic
truth**", and a declared result table is the durable semantic truth
for its family.

Applied to the families the graph does not hold today (from
`go/internal/facts/reducer_derived.go` and `read-models.md`): impact
findings, security-alert reconciliations, the four drift findings
(AWS, multi-cloud, Terraform config-state, code-drifted), the three
package correlations, CI/CD run correlations, Kubernetes correlations,
service-catalog correlations, observability-coverage correlations,
and workload identity are all list/count/filter families → result
tables. Any future read that needs unbounded traversal over one of
them (for example transitive blast radius through impact findings)
must declare `graph` in the catalog first; that declaration, not this
document, is what would justify projection.

## Decision 3: the answering store lives in the capability matrix

Where the 162 capabilities are declared today (verified): the machine
source of truth is `specs/capability-matrix.v1.yaml` plus the
`specs/capability-matrix/*.yaml` fragments (parsed by
`go/internal/capabilitycatalog/matrix.go`), overlaid with editorial
metadata from `specs/capability-catalog.v1.yaml`, and reconciled by
`go/cmd/capability-inventory` (`-mode generate|verify`) into
`go/internal/capabilitycatalog/data/catalog.generated.json` — 162
entries, confirmed by parsing the artifact. Because the API and MCP
are moving to their own repositories, that generated JSON is the
cross-repository contract; the declaration must land there, not only
in Go code.

Where the field belongs: the **matrix**, not the overlay. The overlay
is for metadata that cannot be derived (display names, maturity
overrides); the answering store is a runtime fact every capability
must state, like its profiles. Concretely:

- Add `answering_stores: [...]` to `matrixFileCapability`
  (`matrix.go:64`) and `MatrixCapability` (`matrix.go:24`), as a
  **closed enum** validated at load (the `allowedVerificationKinds`
  pattern, `matrix.go:79`): `graph`, `fact_records_reducer`,
  `fact_records_collector`, `read_table:<table>`, `content_search`,
  `status_fixed`. Mixed declarations are lists (13 capabilities need
  two stores today); the 6 unsorted capabilities are classified
  first, each from its handler code, in the same slice.
- Carry it through `Entry` (`catalog.go:38`) as
  `answering_stores` in the generated JSON; regenerate with
  `cd go && go run ./cmd/capability-inventory -mode generate`.

How the check fails a read that uses an undeclared store — two layers,
both extending existing machinery:

1. **Declaration completeness (static).** `Build`/`Reconcile`
   (`catalog.go:112`, `reconcile.go:50` `Finding`) emits a finding for
   any capability with no `answering_stores`, which makes
   `-mode verify` fail — the same stale-artifact failure the
   generator already returns. This proves every capability declares,
   not that declarations are honest.
2. **Honesty (code audit).** A lockstep test in the
   `capability_lockstep_*_test.go` family (`go/internal/query/`)
   maps each capability's handler to the stores its SQL/Cypher
   actually names (table names in query constants, graph-reader
   calls) and fails when the used set is not a subset of the declared
   set. Per the repository gate rule it ships with a seeded-violation
   RED/GREEN pair: a fixture handler reading `fact_records` under a
   `graph`-only declaration must fail the test, and the clean tree
   must pass. Runtime enforcement (rejecting a query mid-flight) is
   explicitly **not** built: the store is chosen at compile time in
   handler SQL, so a static gate catches every real case without
   adding a hot-path check.
   Status: layer 2 is **not yet landed**. What slice 0 ships is
   declaration-completeness (layer 1) plus sampled lockstep probes, not
   the full handler-to-store audit; the audit is remaining work below,
   and no claim in this record says otherwise.

## Decision 4: slice plan (each slice independently committable)

| Slice | Change | Files (primary) | Required proof |
| --- | --- | --- | --- |
| 0. Declare stores | `answering_stores` on all 162 matrix rows (6 unsorted classified from handlers); enum + `Entry` field; regenerate catalog; verify-mode finding; lockstep honesty test with seeded RED/GREEN | `specs/capability-matrix*.yaml`, `specs/capability-matrix/*.yaml`, `go/internal/capabilitycatalog/{matrix,catalog,reconcile}.go`, `go/cmd/capability-inventory/`, `go/internal/capabilitycatalog/data/catalog.generated.json`, `go/internal/query/capability_lockstep_answering_store_test.go` (new), `docs/public/reference/capability-catalog.md` | `go run ./cmd/capability-inventory -mode verify` exit 0; seeded violation fails, clean tree passes; no behavior change (catalog JSON diff is additive only) |
| 1. Payload onto winners | New migration (landed 331/332) adds `payload JSONB` + winner `source_confidence`; resweep select carries payload; write-on-change comparison includes payload | `go/internal/storage/postgres/migrations/NNN_supply_chain_impact_winners_payload.sql` (new), `supply_chain_impact_canonical_winners_rebuild.go`, winner-select SQL home, `schema_order_test.go`, `supply_chain_impact_canonical_winners_schema_test.go` | Live rebuild test: stored payload byte-equal to winner fact payload across the filter matrix; resweep row counts (inserted/updated/unchanged) unchanged for a no-change second resweep; measure winners table + WAL growth from the JSONB column on the retained golden database and record it (prove-the-theory shim before slice 2) |
| 2. Reader switch | List/explain/aggregates read winners only; delete the `fact_records` payload join; catalog: impact capabilities change `fact_records_reducer` → `read_table:supply_chain_impact_canonical_winners` (explain keeps `fact_records_collector` for evidence hydration) | `go/internal/query/supply/chain/impact/findings_queries.go` (+ siblings), `go/internal/query/read-models.md`, matrix rows for the three impact capabilities + regenerated catalog | **Golden-corpus equality**: `scripts/verify-golden-corpus-gate.sh` green with API **and** MCP impact answers byte-identical before/after; focused `go test ./internal/query/... -run SupplyChainImpact` incl. scoped-token multi-support cases (the named risk above) |
| 3. Latency proof + write stop | Run the read-latency gate on the switched readers; then reducer stops writing finding facts (direct winners maintenance input), P2-style reject trigger migration; record before/after fact-table updates, index count, save time for the family | `scripts/verify-read-api-latency-gate.sh` output recorded in PR; `go/internal/reducer/supplychain/core/writer.go`, `writer_retract.go`, `go/internal/reducer/factwrite/` call sites, new reject-trigger migration, `go/internal/facts/reducer_derived.go` (kind disposition note) | `scripts/verify-read-api-latency-gate.sh`: impact routes within `testdata/benchmarks/read-api-route-budgets.txt` (count route budget 2000 ms today) with no regression vs the pre-switch run on the same seeded scale; concurrency: competing-writer live test on the new write path (fencing/retraction parity), per the queue-proof rule — row equality alone is not enough |
| 4. Drop the ten indexes | Migration drops exactly the ten named `fact_records_supply_chain_impact_*` indexes; fix the stale "63 indexes" count in `docs/public/reference/postgres-tuning.md` from the same census | `migrations/NNN_drop_fact_records_supply_chain_impact_indexes.sql` (new), `schema_fact_records_supply_chain_impact_indexes.go`, `schema_order*_test.go`, `docs/public/reference/postgres-tuning.md` | Slice-3 latency report already recorded (gate: no slice 4 without it); live test proves plans for surviving readers name winners indexes; before/after table: fact updates/day for the kind = 0, index count 152→142 (live count re-read on the **read replica** at execution time), save-time delta measured on the golden database |

Slice 0 is independent of slices 1–4 and lands first: it is small,
behavior-free, and every later family move reuses it. Slices 1–4 are
ordered and must not be reordered — in particular no index drops
before the slice-3 latency report exists, and no write stop before
slice-2 equality is green. Telemetry: reuse existing instruments
(`eshu_dp_postgres_query_duration_seconds`, winners resweep counts,
`query.supply_chain_impact_*` spans); slice 3 adds one counter for
rejected legacy finding-fact writes so an operator can see a stale
binary still trying the old path.

## Remaining families, in migration order

1. **Security-alert reconciliations** (`reducer_security_alert_reconciliation`)
   — next: same list/aggregate shape, own partial indexes
   (repository/package/status, CVE/GHSA GIN), readers in
   `supply/chain/alerts/` already isolated in one store type.
2. **Drift findings as one group** (AWS, multi-cloud, Terraform
   config-state, code-drifted) — 22% of handler time (cloud drift),
   read-only dispositions already declared in
   `reducer_derived.go`; one shared findings-table shape, four kinds.
3. **Package correlations** (ownership, consumption, publication) —
   bounded correlation reads (`GET /api/v0/package-registry/correlations`);
   check first whether any declared read needs graph traversal, since
   package identity itself is graph-resident (mixed declaration is
   allowed and likely).
4. **Correlation families** (CI/CD run, Kubernetes, service-catalog,
   observability-coverage, workload identity) — same six-outcome
   contract family in `read-models.md`; move together once one proves
   the shape. `reducer_cloud_asset_resolution` is reducer-internal
   with no read surface and does not move.

## What this ruling will not do, and why

- **No graph projection for impact findings.** No declared read needs
  unbounded traversal; adding edges would create a second truth to
  keep rebuildable for zero read benefit (D5).
- **No new findings table beside winners** (see Decision 1).
- **No runtime store police.** The honesty check is static; a
  per-query runtime check would tax the hot path to catch what a
  compile-time audit already catches.
- **No serialization.** Nothing here reduces workers or single-threads
  the resweep; the existing single-flight lock is a correctness fence
  with a measured cadence, kept as is.
- **No index drop on faith.** Index use is read on the read replica
  (39 fact indexes scan only there); slice 4 requires the replica
  census at execution time, not the 2026-11-17 numbers.

## What stays unverified (NOT_CHECKED)

- Live QA figures from #10880 (152 indexes, 121 GB, 88M rewrites) are
  taken from the issue's 2026-11-17 read-only measurements; this
  ruling verified code and DDL, not live QA. Slice 4 re-reads them.
- Winners payload growth (table size, WAL, resweep time with JSONB in
  the change comparison) is unmeasured until the slice-1 shim runs on
  representative data. If it regresses the resweep materially, slice 1
  stops and returns here.
- Scoped-token equivalence of the canonical pick on multi-support
  findings is a slice-2 proof obligation, not an assumption (see the
  named risk). The P2 supports-table fallback is pre-approved shape,
  not pre-approved scope.
- The 6 unsorted capabilities are not named in committed artifacts;
  slice 0 identifies them from handler code and records the census it
  used, so the classification is auditable.
- #10883 (graph rebuild at 9,000+ scopes) remains open; nothing in
  this plan makes any family graph-only, so it does not block slices
  0–4.

## Execution record (2026-11-20/21, branch `drive/10880-move-reducer-results`)

- **Slice 0 landed** (`c9909ef9f5`): `answering_stores` on all 162
  matrix rows, closed-enum load validation, `missing_answering_store`
  reconcile finding, regenerated catalog, lockstep sampled-probe test
  (declaration-completeness plus sampled probes; the full
  handler-to-store audit of Decision 3 layer 2 remains open).
- **Slice 1 landed** (`c900d01a88`): migration
  `331_supply_chain_impact_winners_payload.sql` adds `payload JSONB`
  and `source_confidence TEXT` to the winners table. The full and
  keyed resweeps and the writer-backed audit carry both columns via
  `supplyChainImpactWinnerDataColumns`, so a payload-only change
  rewrites the winner row (proof:
  `TestSupplyChainImpactWinnersResweepPayloadCarriedLive`). The staged
  audit cannot see an evidence-only rewrite under one fact id; only
  the writer capture guards it ([limits](10880-pr-a-review-limits.md)).
  The stored payload bakes in the operator suppression object; readers
  rewrite only the effective state. Telemetry: no new instrument (resweep
  counts now include payload rewrites;
  `eshu_dp_postgres_query_duration_seconds` covers the wider rows).
  Review P1-1 fix: 331 alone left pre-existing rows at the column
  defaults until the first post-deploy resweep, so migration
  `332_supply_chain_impact_winners_payload_backfill.sql` backfills
  both columns by `winner_fact_id` join, replicating the winner
  select's operator bake, gated on `payload = '{}'` for replay
  idempotency. A separate file (the 126/127 split, #7961) keeps
  331's ACCESS EXCLUSIVE ALTER off the backfill scan; the UPDATE
  takes ROW EXCLUSIVE plus one row lock and one `fact_records`
  primary-key probe per winner, the shape of one routine resweep.
  A reader-side fallback join was rejected: it would re-add the
  fact-table payload read slice 2 removed, to cover a window the
  backfill closes at deploy. Proof:
  `TestFindingsWinnersPayloadBackfillServesWithoutResweepLive`
  resets winners to the 331 defaults, applies only 332, and reads
  legacy-equal pages with no resweep in between.
- **Slice 2 landed for the list read** (`808865baee`):
  `ListFindingsFromWinnersQuery` no longer joins `fact_records` or
  rescans operator candidates; page payloads come from the winners
  row via a canonical-key join. Live parity against the legacy fact
  read passes on ten filter cases including scoped-token repository
  and scope grants and live/expired suppressions
  (`TestFindingsWinnersPayloadParityLive`) — the named multi-support
  risk did not materialize on this corpus. Aggregates, grouped
  inventory, and explain still read `fact_records`, and the runtime
  workload filter reads `reducer_workload_identity` facts in both
  paths, so `supply_chain.impact_findings.list` is declared mixed:
  `read_table:supply_chain_impact_canonical_winners` +
  `fact_records_reducer`. Golden-corpus gate: attempted in this lane;
  fails first-drain on branch and bare base with identical
  advisory-projection dead letters, tracked as #11161 (store-level
  parity is the local proof).
  Review P2 (suppression freshness) verdict: covered, no fix needed.
  Operator writes reach the baked payload through two dirty paths:
  the migration-310 activation trigger stages `scope` rows
  (source `operator`) for both generations when the operator
  scope's `active_generation_id` flips, and the reducer writer
  stages `fact_upsert` identifiers for every changed finding,
  operator-scope re-derivations included; the keyed tick expands
  the scope rows, always re-adds every active operator fact to its
  active set, and the payload sits in the write-on-change columns,
  so a suppression-object-only change rewrites the winner. Proof:
  `TestSupplyChainImpactWinnersResweepKeyedRefreshesBakedSuppressionLive`
  (dirty backlog non-empty after the flip, baked reason refreshed
  and cleared on the keyed path) plus the pre-existing operator
  override/removal case in the keyed change-class parity proof.
- **Slice 3 (write stop) NOT landed.** Inspecting the write path
  showed blockers the ruling's slice sketch did not enumerate: the
  protected publication authorize/publish pair runs inside the
  finding-fact write transaction
  (`supply_chain_impact_canonical_winners_store.go`
  `supplyChainImpactTx`), and aggregates
  (`aggregates_queries.go`), explain (`explain_postgres.go`), the
  gate-off legacy list (`findings_split_queries.go`), and readiness
  all still read finding facts. Stopping the fact write before those
  readers move would break them; the write stop therefore follows
  the aggregate/explain reader moves, not this lane. The rejected-
  write counter lands with that slice. Read-latency gate
  (`scripts/verify-read-api-latency-gate.sh`): **NOT_CHECKED** here.
  Local shim instead (corrected at review: the first record compared
  against the legacy fact-dedup read, but slice 2 replaced the
  winners + `fact_records` refetch join, so that is the baseline
  re-measured here). Disposable Postgres 18, 2,000 synthetic
  winners, page 50, priority-desc, 20 iterations per round, two
  rounds in alternating order, pages byte-equal across paths:
  replaced path (winners + fact-refetch + operator overlay)
  12.11 ms avg / 11.71 ms median, then 9.61 / 9.60 ms; new winners-
  payload path 9.57 / 9.61 ms, then 7.56 / 7.33 ms — the new path
  is faster in both orders. Legacy fact-dedup on the same seed, for
  context only: 22.48 ms avg. Full resweep 92 ms. These are small-N
  local numbers, not seeded-scale or production-scale proof.
- **Slice 4 (drop ten indexes) NOT landed**, by the ruling's own
  gate: no read-replica census was available in this lane and slice
  3 has not landed, so the indexes still serve live readers.
  Before/after for this family after slices 1–2: fact-table updates
  unchanged (the reducer still writes finding facts — slice 3
  pending); fact-table index count unchanged (10 impact indexes
  retained); collector save time unchanged. Winners growth at the
  corrected synthetic seed: 2,000 rows, 4,636,672 bytes total
  relation size, 1,136 bytes average stored payload — the payload
  column roughly doubles the winners row, with no material resweep
  regression at this scale (92 ms full resweep). The stale "63
  indexes" figure in `postgres-tuning.md` stays for the slice-4
  census to correct.
- **Slice 2b(i) aggregates landed, gated default-off** (branch
  `drive/10880-move-reducer-results`, unpushed): `PostgresAggregateStore`
  carries `ReadFromWinners` with `NewPostgresAggregateStoreWithReadModel`
  and a gated `NewPostgresAggregateStoreWithReadStore`, threaded from the
  existing `readImpactFromWinners` gate through `serve_adapters_data.go`,
  `d6_read_admission_wire.go`, and the service-intelligence mount
  (`main_wiring.go`/`main.go`); scoped-token calls stay on the legacy CTE
  by construction, mirroring the list read. Winners count, priority and
  severity buckets, and all five inventory dimensions read
  `supply_chain_impact_canonical_winners` with the same 20-argument shape
  (caller-auth args referenced, never consumed — the winners table never
  holds the derived protected branch). Live proof:
  `TestAggregatesWinnersParityLive` (byte-identical envelopes across 7
  filters x count + 5 dimensions, incl. live/expired suppressions and the
  scoped fallback) plus `TestAggregatesWinnersReadGate` (routing pins for
  the combined count statement and inventory). Capability
  `supply_chain.impact_findings.aggregate` is now declared mixed and the
  catalog regenerated + verified. Telemetry: No-Observability-Change — no
  new instrument; the gate reuses the existing winners resweep counts.
- **Slice 2b combined count: shape measured on real Postgres.** The winners
  twin of #10970's single statement was proved before it stayed. Disposable
  `postgres:18`, 300,000 winners rows over 2,000 repositories,
  `EXPLAIN (ANALYZE, BUFFERS)`, median of 7 warm runs, host load ~60 (read
  ratios). A: first draft, `aggregate_winners` NOT MATERIALIZED, eight
  references (93,065 buffer hits). B: A with the CTE MATERIALIZED. C: the
  three old winners statements summed. D (shipped): one scan grouped to
  (status, priority bucket, severity bucket) in a MATERIALIZED
  `winner_groups` CTE, every output derived from it (11,524 hits).

  | Filter (rows matched) | A | B | C | D |
  | --- | --- | --- | --- | --- |
  | empty (300k) | 636 ms | 462 ms | 260 ms | 92 ms |
  | `status=affected_exact` (112.5k) | 431 ms | 184 ms | 162 ms | 54 ms |
  | `detection_profile=precise` (162.5k) | 555 ms | 199 ms | 192 ms | 72 ms |
  | `repository_id` (150) | 1.3 ms | 0.6 ms | 1.3 ms | 0.5 ms |

  The draft was slower than the three statements it replaced (eight scans
  against three), so its "scanned once per column" comment was wrong.
  MATERIALIZED alone (B) still lost to C on the empty filter. D wins every
  row, about 2.8x over C on the empty filter. Totals and both bucket maps
  matched across A, B, and D for all four filters. `winner_groups` is a
  closed-vocabulary cross product, a few hundred rows at most. The four
  live parity tests (`TestAggregatesWinnersParityLive`, both
  `TestFindingsWinnersPayload*`, `TestGroupedInventoryStoreWinnersLive`)
  pass on D. Method: throwaway `generate_series` seed, not committed.
- **Newly listed 2b blocker (found during slice 2b(i)):** the advisory
  evidence reader (`go/internal/query/supply/chain/advisory/evidence_sql.go`
  `repo_probe` + `impact_candidates`) reads
  `reducer_supply_chain_impact_finding` facts directly. Decision 1's
  "advisory-evidence seeding" line did not name this reader; the write
  stop (slice 3) must move or retire it too. Unscoped aggregates are the
  only newly gated path; explain, the scoped aggregate path, the gate-off
  legacy list, readiness target resolution, the protected publication
  pair, and this advisory reader remain on finding facts.
- Remaining order is unchanged: explain/advisory/readiness reader moves
  and the gate-off, then slice 3 (write stop + reject trigger + latency
  gate), then slice 4 (replica census + index drop), then the families
  listed under "Remaining families" above.

No-Regression Evidence: slices 1–2 above — live proofs
`TestSupplyChainImpactWinnersResweepPayloadCarriedLive` (payload
equality, payload-only rewrite, write-on-change counts),
`TestFindingsWinnersPayloadParityLive` (byte-identical pages,
scoped-token grants), and
`TestFindingsWinnersPayloadBackfillServesWithoutResweepLive`
(332-backfilled rows serve legacy-equal pages, no resweep) on
disposable Postgres 18, plus the corrected 2,000-row shim against
the path slice 2 replaced (resweep 92 ms; new path 9.57 then 7.56 ms
avg vs replaced 12.11 then 9.61 ms avg; 1,136-byte average payload).
Small-N local numbers; seeded-scale gate deferred to slice 3.

No-Observability-Change: slices 1–2 add no metric, span, log key,
route, worker, or knob; existing resweep counts and
`eshu_dp_postgres_query_duration_seconds` cover the wider rows.
The rejected-legacy-write counter lands with slice 3.
- Lane D pointer: [10880-ac5-ac6-closeout.md](../evidence/10880-ac5-ac6-closeout.md) reports AC5/AC6 from Lane B outputs (save time NOT_CHECKED); it closes nothing.
