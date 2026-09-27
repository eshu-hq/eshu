# 7127 PR-3a: the dark changed-since link writer

PR-3a of #7127 adds the changed-since link ledger and its writer. Migration 133
creates six tables. The store `go/internal/storage/postgres/freshness/links`
links one activation per transaction. The reducer domain
`go/internal/reducer/freshness/links` journals activations and drives the
store. The domain stays behind `ESHU_CHANGED_SINCE_LINK_ENABLED`, which is off by
default. Nothing reads the ledger yet: the read path is PR-3c. Retention cleanup
of the ledger is PR-3d, and it must land before the switch is turned on
anywhere.

Source binding: branch `perf/7127-changed-since-link-writer` on `origin/main`
`049be71612`. The PostgreSQL used throughout is `postgres:18-alpine` 18.6
(aarch64) in a disposable container.

## Design as built

The design follows the #7127 arbiter ruling's contract (sections 2.3, 2.5
amended by 8.5, 2.8, 7 and 8.6):

- **Tables.** The six tables of ruling 2.3 are created with no foreign keys
  (ruling 7.3) and no indexes beyond those in 2.3: the primary keys,
  `changed_since_activations_scope_seq_idx`, and
  `changed_since_key_state_owner_idx`.
- **Link transaction.** The transaction takes three locks in this order, none
  of which waits:
  1. the cursor row, `FOR UPDATE SKIP LOCKED`;
  2. a plain existence read of the generation, then the generation `FOR KEY
     SHARE SKIP LOCKED`;
  3. for a full link, one of N `pg_try_advisory_xact_lock(7127, slot)` slots.

  It then sets `SET LOCAL work_mem = '256MB'`, `plan_cache_mode =
  force_custom_plan` and `statement_timeout`, and runs one statement. That
  statement is `root` in the shim's `root_b` shape or `incremental` in the
  `l1b_256.sql` shape. The digest input is the shipped
  `changedSincePayloadDigestInput`. Its bytes moved to
  `linksfreshnessstore.PayloadDigestInput` and the root constant is an alias,
  so the read statement, the golden-corpus fragments file and the writer hash
  the same bytes (`TestChangedSinceOracleFragmentsMatchGoConstants` green).
- **Outcomes.** A miss on any lock, or a statement timeout (SQLSTATE 57014),
  rolls back and returns `*RetryError` with `cursor_locked`,
  `generation_locked`, `slot_busy` or `statement_timeout`. It satisfies
  `reducercontract.RetryableError`, and the cursor does not move.
- **Chain break (ruling 8.5).** A chain break advances
  `state_activation_seq`, keeps the state, and records its reason:
  - `pruned_before_link`: the generation row is absent;
  - `delta_without_root`: a delta activation with no state;
  - `prior_mismatch`: a delta activation whose prior is unknown or does not
    match the state;
  - `overlay_unproven`: a delta activation whose prior matches the state
    (see G2).
- **Runner.** The runner is a side runner beside generation retention. Each
  cycle does four things:
  - it journals a `sweeper` row with a NULL prior for every active generation
    that has none, plus a backfill of retained chains bounded per cycle
    (ruling 2.8), under a try-only advisory lock (slot 0 of class 7127);
  - it deletes the ledger rows of deleted scopes;
  - it links the backlog, with at most `ESHU_CHANGED_SINCE_LINK_WORKERS`
    scopes at once;
  - it samples the gauges.

### Deviations from the ruling's text, with the observation behind each

1. **Directory names.** The ruling names `go/internal/storage/postgres/changed_since_link*.go`
   and `go/internal/reducer/changedsince`.
   - The dirgate ledger pins `internal/storage/postgres` at exactly 321
     non-test files, and growth fails whatever the digest
     (`scripts/lib/dirgate-grandfather.tsv`).
   - The pre-commit naming-glue gate rejected `changedsince` as a glued
     compound (naming.md rule 3).
   - The store is therefore `storage/postgres/freshness/links`, package
     `linksfreshnessstore`, next to `freshness/aws` and `freshness/gcp`. The
     domain is `reducer/freshness/links`.
   - `cmd/reducer` sits at the 40-file cap, so the wiring lives in
     `generation_retention_wiring.go`.
2. **Runner, not a queue domain.** The journal and the per-scope cursor are
   the durable queue, so there are no `fact_work_items` rows. A retryable
   outcome leaves the cursor, and the next cycle retries the same activation.
   The question went to the coordinator, and this note records the answer the
   PR ships with. G16's dead-letter and fault-injection rows follow from it.
3. **G3 index name.** Ruling 8.6 names `fact_records_scope_generation_idx`,
   but the shim fixture had only migration 003's indexes. On the full
   bootstrap the planner also picks `fact_records_scope_generation_keyset_idx`
   (`scope_id, generation_id, observed_at, fact_id`, migration 099), which
   reads the same single (scope, generation) range. The test accepts either
   index and fails on any other access shape.
4. **G4 chain read shape.** The plain join form of `ev` in the shim's
   `chain_read_v3.sql` fails at 2M rows with stale statistics. Its custom plan
   is a Bitmap Heap Scan whose index condition is `scope_id = 's0007'` only, so
   it reads every retained delta of the scope. The `links CROSS JOIN LATERAL
   (... OFFSET 0)` form that ruling 8.4 allows passes in both plan-cache modes
   with stale and with fresh statistics. PR-3c's read statement must use it,
   and needs its own equality run against v1.

## Gates G1-G16

| Gate | Result | Evidence |
| --- | --- | --- |
| G1 accuracy | PASS | `TestChangedSinceLinkAccuracyAgainstClassificationOracle` (root package). The fixture has three full generations covering update, `indexed_at`-only change, changed non-minimum duplicate, multiplicity change, tombstone, tombstone with an active row, drop, add, kind change, scalar content-entity payload and a `reducer_` kind. After each link the state equals the independent aggregate, and the link deltas equal a diff built from the shipped `changedSinceClassificationCTEs` (SHA-256 over ordered rows, unchanged counts included). The RED writer without the tombstone predicate gives 6 oracle mismatches; the RED writer without `indexed_at` normalization gives 7. |
| G2 delta oracle | FAIL (not built) | No overlay link and no real-git-collector delta oracle in this PR. Scan 0b has no artifact. Per ruling 8.6, a delta activation is a chain break (`overlay_unproven` when its prior matches the state), and the overlay follows in its own PR with the collector proof. |
| G3 plan, L1b | PASS | `TestLinkStatementPlanShape`: exactly one `fact_records` scan, an Index Scan whose condition names `scope_id` and the activating generation (`fact_records_scope_generation_keyset_idx` without ANALYZE, `fact_records_scope_generation_idx` for the 45k-key scope after ANALYZE), and `Triggers` empty, for a 600-key and a 45,000-key scope with and without `ANALYZE`. See deviation 3. |
| G4 plan, deltas and state | PASS | `TestLinkDeltaChainReadUsesPrimaryKey`: 2,000,000 delta rows over 500 scopes and 5,000 links; the probed scope holds 0.2% of the rows over 4 links. The access is the primary key with `scope_id`, `generation_id` and `prior_generation_id` in the index condition, at most 10 buffers per row plus 64. It holds in `force_custom_plan` and `force_generic_plan`, with statistics from 13k rows (autovacuum off) and after ANALYZE, and it reads 1,600 rows. RED: with the key dropped, the assertions fail. `TestLinkStatementPlanShape`: 3,844,080 state rows over 800 scopes; the state side reads `changed_since_key_state_pkey` by `scope_id` with no sequential scan for the 600-key and 45,000-key scopes; the 600-key link took 5.3 ms against the 100 ms gate. See deviation 4. |
| G5 temp files | PASS | Scale run: `pg_stat_database.temp_files` and `temp_bytes` deltas were 0 across the root and the incremental 1.0x link at `work_mem` 256MB, and 0 again with 2 and 4 concurrent links. |
| G6 RssAnon | PASS | Scale run: the peak single-backend `RssAnon` during a 1.0x incremental link was 464,480 kB (453.6 MiB), under the 524,288 kB (512 MiB) gate with an 11% margin. |
| G7 timing ratio | NOT_CHECKED | Scale run: 6 interleaved rounds were recorded with the first mover alternating, and **0 were valid**. The host's 1-minute load was 36.7-70.9 on 18 CPUs at every round start, against a validity bound of load below 18. The invalid rounds give L1b/bare_b paired ratios of 1.05-1.25 (median 1.14); they are not pooled and are not a pass. Root at 8.0 s against same-run bare_b samples of 6.0-7.7 s is also invalid by the same rule. Ruling 8.2's fallback is to rerun on the remote test machine from the reviewed branch. |
| G8 cap proof | PARTIAL | Scale run, 1, 2 and 4 concurrent 1.0x incremental links on different scopes (4 slots). **Memory:** summed peak `RssAnon` was 453.6 MiB, 908.5 MiB and 1,381.8 MiB; the n=4 sampled peak falls below 4 x 453.6 MiB because the four backends did not peak in the same sweep. **Read probe** (a 2,000-row page read from `fact_records` every 200 ms): p50 1.4 / 2.6 / 1.4 ms and p95 2.6 / 34.2 / 17.3 ms. **Walls:** 6.3 s; 29.9 and 29.1 s; 30.5-32.3 s. The host load rose from 19.1 to 81.7 across the run, so the wall times are not comparable between n values and do not prove or disprove the 2-slot default. The memory figures support the ruling's bound of two links near 1 GiB. |
| G9 fence | PASS | `TestLinkFenceOneWinnerPerActivation`: 24 rounds of 4 concurrent `LinkWriter`s released together on one scope with a 3,000-row generation. Every round gives exactly one root link, one link row and one cursor advance, and every loser returns `cursor_locked` in under 1 s (or idle after the winner committed). `TestCursorHeldReturnsRetryWithoutWaiting` pins the non-blocking half deterministically: with the cursor held by another session, the result is `cursor_locked` in under 1 s. Both ran in the compiled test binary against PostgreSQL 18, not in the reducer binary. |
| G10 kill and rerun | PASS | `TestLinkKilledMidStatementRerunsToIdenticalRows`: 150k-row generations. The incremental statement is held mid-statement by a row lock on a state row it must update, then terminated with `pg_terminate_backend`. The ledger rows and the cursor are unchanged afterwards. The rerun links, and all ledger rows equal an uninterrupted reference scope built from the same fixture. |
| G11 lock outcomes | PASS | `TestGenerationLockedIsRetryable`: with the generation held `FOR UPDATE`, the result is `generation_locked` in under 1 s, the cursor does not move, and the link succeeds after release. `TestDeltaWithoutRootAndPrunedBeforeLink`: an absent generation gives `pruned_before_link`. |
| G12 slot outcomes | PASS | `TestSlotBusyBlocksFullLinksOnly`: with both slots held, a full link returns `slot_busy` while a delta activation (a break) still advances; the full link succeeds after release. |
| G13 schema | PASS | `TestLedgerSchemaHasNoForeignKeys`: no foreign key on or referencing the six tables, and `fact_category` and `stable_fact_key` are NOT NULL. RED: a planted FK and a dropped NOT NULL inside a rolled-back transaction are reported. |
| G14 switch off | PASS | `TestChangedSinceLinkRunnerIsOffByDefault` (`cmd/reducer`): with no environment and with `false`, no runner is built, and a database double that fails the test on any query, exec or begin sees none. A nil `Service.ChangedSinceLinkRunner` is never started. |
| G15 break keeps state | PASS | `TestChainBreakKeepsStateThenIncremental`: root F0, then two delta breaks (`overlay_unproven`, `prior_mismatch`) keep the state rows and generation. The next full generation links `incremental` F0 -> F3, the state equals the aggregate of F3, and only the 8 changed keys are written. |
| G16 repo gates | PARTIAL | Every new live test is classified in `specs/live-tests.v1.yaml` (`verify-live-tests-ledger.sh`: 489 rows, all classified). All six variables are in `go/internal/envregistry` and the generated reference. Ifa dead-letter and fault-injection rows: the domain has no work-item queue (deviation 2); the other side runners (generation retention, infra reconcile) have no Ifa rows either. |

## Scale run

Generator: `7127-link-writer-scale.py` (driver) and
`7127-link-writer-fixture.sql` (rows). Raw output:
`7127-link-writer-scale-results.json`. It ran against container
`postgres:18-alpine` 18.6 with `shared_buffers` 1GB, default `work_mem` 64MB
(each link sets 256MB locally), `random_page_cost` 1.1, `synchronous_commit`
off, a 12 GiB container memory limit, and 18 host CPUs.

The fixture has four 1.0x scopes. Each has full generations F0 and F1 of the
shim's generator: 771,201 effective keys, 770,881 after F1, and 4,442
delta rows per F0 -> F1 link. There are also 30 noise scopes of 25,000 rows
per generation. The load took 473.8 s and the two index builds 65.8 s.
Only the two indexes the link statement can use were built; G3 covers the
full bootstrap.

| n | root walls (s) | incremental walls (s) | temp files | peak backend RssAnon (kB) | summed RssAnon (kB) | probe p50 / p95 (ms) | host load at window start |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 8.0 | 6.3 | 0 | 464,480 | 464,480 | 1.4 / 2.6 | 19.1 |
| 2 | 10.1, 8.8 | 29.9, 29.1 | 0 | 465,528 | 930,260 | 2.6 / 34.2 | 18.7 |
| 4 | 50.6, 63.7, 29.7, 28.0 | 32.3, 32.2, 32.2, 30.5 | 0 | 423,740 | 1,414,988 | 1.4 / 17.3 | 81.7 |

G7 rounds, all invalid (load at start: 70.9, 69.1, 62.5, 51.7, 43.2, 36.7):
L1b/bare_b ratios were 1.11, 1.05, 1.19, 1.15, 1.25 and 1.13.

What this run does not measure:
- the timing gates under a valid load;
- cold cache;
- the read-API latency of a real deployment with the domain on and off. That
  belongs to the dark deployment (ruling 8.7).

## Mutation checks

Each check mutated one production line, ran the gate's test, and then
restored the file with `git checkout`. Every test failed as required:

| Mutation | Test | Result |
| --- | --- | --- |
| cursor lock without `SKIP LOCKED` | `TestCursorHeldReturnsRetryWithoutWaiting` | FAIL (blocked 3 s, no `cursor_locked`) |
| generation lock `NOWAIT` instead of `SKIP LOCKED` | `TestGenerationLockedIsRetryable` | FAIL |
| a chain break clears the state | `TestChainBreakKeepsStateThenIncremental` | FAIL |
| slot always granted | `TestSlotBusyBlocksFullLinksOnly` | FAIL |
| switch defaults on | `TestChangedSinceLinkRunnerIsOffByDefault` | FAIL |

`TestCursorHeldReturnsRetryWithoutWaiting` exists because the race test in G9
also accepts an idle loser, so it alone would not notice a blocking cursor
lock.

## Statements that read a ledger table by scope (ruling 8.4, "sweep the class")

| Statement | Access | Assertion or reason |
| --- | --- | --- |
| `IncrementalLinkSQL` state side | PK prefix `scope_id` | G4 state test |
| `IncrementalLinkSQL` delete and upsert | PK equality | G4 state test (no seq scan of the state table) |
| `clearStateQuery` (root after a digest change) | PK prefix `scope_id` | Deletes nothing on a first root; same prefix as the G4 access |
| `lockCursorQuery`, `advanceCursorQuery`, `ensureCursorQuery` | cursor PK equality | One row per scope |
| `nextActivationQuery` | `changed_since_activations_scope_seq_idx` | `(scope_id, activation_seq)` with `LIMIT 1` |
| `backlogScopesQuery`, `backlogStatsQuery`, `orphanScopesQuery` | full scan of the journal and the cursor | The journal holds one row per activation, bounded by generation retention once PR-3d lands (T0: about 5,161 activations per 7 days on ops-qa); the cursor has one row per scope |
| `backfillChainsQuery`, `journalActiveGenerationsQuery` | `ingestion_scopes` scan, journal probes by the unique `(scope_id, generation_id)` | One row per scope |
| `deleteOrphanScopeStatements` | each table's `scope_id` key prefix | Deleted scopes only |
| `ledgerSizeQuery` | catalog only | Reads no table rows |

## Commands

Every command ran from `go/` unless noted, with `ESHU_POSTGRES_TEST_DSN` set
to the disposable container.

- `go test ./internal/storage/postgres/freshness/links -count=1` (live): ok, 158 s
- `go test ./internal/storage/postgres/freshness/links -run CursorHeld -count=1 -v` (live): ok
- `go test ./internal/storage/postgres -run TestChangedSinceLinkAccuracy -count=1 -v` (live): ok
- `go test ./internal/storage/postgres/... ./internal/reducer/... ./cmd/reducer ./internal/telemetry/... ./internal/envregistry -count=1` (no DSN): ok, 107 packages
- `go test -race ./internal/reducer/freshness/links -count=1`: ok
- `docs/internal/evidence/7127-link-writer-scale.py --container eshu-7127-3a-pg --port 25471 --rounds 6` (repo root): rc 0
- `bash scripts/verify-live-tests-ledger.sh` (repo root): ok, 489 rows
- `ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash scripts/verify-telemetry-coverage.sh` (repo root): ok
- `mkdocs build --strict --clean --config-file docs/mkdocs.yml` (repo root, via `uv run`): rc 0
- `git diff --cached --check`: rc 0

The fixture SQL is deterministic. The results JSON is a measurement, not a
regenerable artifact: a rerun produces new timings, and the RSS figures follow
the host.

Performance Evidence: G3/G4 plan shapes above; G5-G8 in "Scale run", measured
with `7127-link-writer-scale.py` on `7127-link-writer-fixture.sql`.
No-Regression Evidence: the domain is off by default and issues no SQL (G14),
so reducer, projector and read-API behaviour is unchanged until it is enabled.
Observability Evidence: `eshu_dp_changed_since_links_total{link_kind,outcome}`,
`eshu_dp_changed_since_link_retries_total{reason}`,
`eshu_dp_changed_since_chain_breaks_total{reason}`, the link duration,
delta-row and key histograms, the backlog, lag and state-size gauges, the span
`reducer.changed_since_link`, and one `changed-since link` log line per link.
