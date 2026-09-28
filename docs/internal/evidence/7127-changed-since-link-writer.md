# 7127 PR-3a: the dark changed-since link writer

PR-3a of #7127 adds the changed-since link ledger and its writer. Migration 136
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
- **Outcomes (ruling 8.10).** A lock miss rolls back and returns a
  non-counting `*RetryError` (`cursor_locked`, `generation_locked`,
  `slot_busy`), writes nothing, and the runner moves on. A failure once the
  link statement ran is a counting `*FailureError` (`statement_timeout`,
  `connection_lost`, `sql_error`, `internal`). `RecordFailure` counts it in a
  second short transaction under the cursor lock, backs off
  min(30 min, 30 s × 2^(n-1)), and at `ESHU_CHANGED_SINCE_LINK_MAX_ATTEMPTS`
  (default 5) records a `link_poisoned` chain break: the cursor advances, the
  state stays, and the poison marker is set until the next full link clears
  it. The transaction has a context deadline of the statement timeout plus
  30 s; the candidate list is a hint and the head is re-read under the lock.
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
2. **Runner, not a queue domain (ruling 8.10, Q1).** The journal and the
   per-scope cursor are the durable queue; no `fact_work_items` row is
   created and no graph edge is written. The ruling's queue-domain wording in
   2.5, 8.6 and G16 is withdrawn; the runner carries its own poison bound and
   G16 is replaced by G16a-G16f.
3. **G3 index name.** Ruling 8.6 names `fact_records_scope_generation_idx`,
   but the shim fixture had only migration 003's indexes. On the full
   bootstrap the planner also picks `fact_records_scope_generation_keyset_idx`
   (`scope_id, generation_id, observed_at, fact_id`, migration 099), which
   reads the same single (scope, generation) range. The test accepts either
   index and fails on any other access shape.
4. **G4 chain read shape (PR-3c must use the LATERAL form).** The plain join form of `ev` in the shim's
   `chain_read_v3.sql` fails at 2M rows with stale statistics. Its custom plan
   is a Bitmap Heap Scan whose index condition is `scope_id = 's0007'` only, so
   it reads every retained delta of the scope. The `links CROSS JOIN LATERAL
   (... OFFSET 0)` form that ruling 8.4 allows passes in both plan-cache modes
   with stale and with fresh statistics. PR-3c's read statement must use it,
   and needs its own equality run against v1. The test keeps the plain join
   as a guard: it must still fail with stale statistics.

## Gates G1-G16

| Gate | Result | Evidence |
| --- | --- | --- |
| G1 accuracy | PASS | `TestChangedSinceLinkAccuracyAgainstClassificationOracle` (root package). The fixture has three full generations covering update, `indexed_at`-only change, changed non-minimum duplicate, multiplicity change, tombstone, tombstone with an active row, drop, add, kind change, scalar content-entity payload and a `reducer_` kind. After each link the state equals the independent aggregate, and the link deltas equal a diff built from the shipped `changedSinceClassificationCTEs` (SHA-256 over ordered rows, unchanged counts included). The RED writer without the tombstone predicate gives 6 oracle mismatches; the RED writer without `indexed_at` normalization gives 7. |
| G2 delta oracle | FAIL (not built) | No overlay link and no real-git-collector delta oracle in this PR; scan 0b has no artifact. As ruling 8.6 allows, a delta activation is a chain break (`overlay_unproven` when its prior matches the state), and the overlay follows in its own PR with the collector proof. |
| G3 plan, L1b | PASS | `TestLinkStatementPlanShape`: exactly one `fact_records` scan, an Index Scan whose condition names `scope_id` and the activating generation (`fact_records_scope_generation_keyset_idx` without ANALYZE, `fact_records_scope_generation_idx` for the 45k-key scope after ANALYZE), and `Triggers` empty, for a 600-key and a 45,000-key scope with and without `ANALYZE`. See deviation 3. |
| G4 plan, deltas and state | PASS | `TestLinkDeltaChainReadUsesPrimaryKey`: 2,000,000 delta rows over 500 scopes and 5,000 links; the probed scope holds 0.2% of the rows over 4 links. The access is the primary key with `scope_id`, `generation_id` and `prior_generation_id` in the index condition, at most 10 buffers per row plus 64. It holds in `force_custom_plan` and `force_generic_plan`, with statistics from 13k rows (autovacuum off) and after ANALYZE, and it reads 1,600 rows. RED: with the key dropped, the assertions fail. `TestLinkStatementPlanShape`: 3,844,080 state rows over 800 scopes; the state side reads `changed_since_key_state_pkey` by `scope_id` with no sequential scan for the 600-key and 45,000-key scopes; the 600-key link took 5.3 ms against the 100 ms gate. See deviation 4. After the ruling arb-7127-g8 fix the state half also asserts that the delete reads the state table by Tid Scan, and P1 asserts the plan class under four planted statistics states. |
| G5 temp files | PASS | 0 temp files for 1.0x links at 256MB in every run: the first scale run, the G8 cap proof on the fixed statement (30 windows, n = 1 / 2 / 4) and P8(c) (120 links). |
| G6 RssAnon | PASS | Fixed statement, G8 cap proof: the peak single-backend RssAnon of a 1.0x link never exceeded 494,280 kB (482.7 MiB) in 30 windows, against 524,288 kB (512 MiB); 5.7% margin. The first run (pre-fix statement) measured 464,480 kB. |
| G7 timing ratio | PASS | Rerun on the fixed statement (P7): 10 valid rounds (load at start 4.95-13.94 on 18 CPUs), L1b/bare_b paired median **1.086** (range 0.895-1.301; gate 1.3). Root/bare_b median 1.841 against 3x; one round (round 4) reached 4.425, a 27.05 s root sample against 8.3-15.3 s elsewhere. Per-round table in the G8 companion note. |
| G8 cap proof | PASS | Fixed statement, fresh container, load-gated (P8 a): 10 valid windows for each of n = 1 / 2 / 4; median slowest link wall 6.85 / 7.18 / 7.90 s; median summed RssAnon 468 / 733 / 1,375 MiB (max 507 / 848 / 1,763 MiB); read-probe p95 median 1.5 / 2.2 / 1.7 ms; 0 temp files; no timeout. The earlier stall (round 26) was the statistics-sensitive delete plan, fixed under ruling arb-7127-g8 and proven by P1-P8; see the companion note. |
| G9 fence | PASS | Store: `TestLinkFenceOneWinnerPerActivation` (24 rounds of 4 concurrent writers: one link and one cursor advance per round, losers `cursor_locked` in under 1 s) and `TestCursorHeldReturnsRetryWithoutWaiting`. Runner, two OS processes of the compiled test binary (`TestTwoProcessRunnersLinkEachActivationOnce`, the production `Runner` in each): 40 scopes, 121 activations; 131 `cursor_locked` races, none taking 1 s; `attempt_count` 0 on every cursor; every cursor at its last activation. Built binary: two `cmd/reducer` processes, see "G9 on the built binary". |
| G10 kill and rerun | PASS | `TestLinkKilledMidStatementRerunsToIdenticalRows`: the incremental statement is held on a row lock and its backend terminated. No partial rows, no cursor move, one counted `connection_lost` (attempt 1, backoff 30 s); a retry inside the backoff is deferred; after it, the rerun links and every ledger row equals an uninterrupted reference. |
| G11 lock outcomes | PASS | `TestGenerationLockedIsRetryable`: generation held `FOR UPDATE` gives `generation_locked` in under 1 s, `attempt_count` 0, cursor unmoved, link after release. `TestDeltaWithoutRootAndPrunedBeforeLink`: an absent generation is a `pruned_before_link` break with the state kept. |
| G12 slot outcomes | PASS | Store `TestSlotBusyBlocksFullLinksOnly`; runner `TestRunnerMovesOnPastABusySlot`: with every slot held the full link is a non-counting `slot_busy` (`attempt_count` 0) and the runner moves on; a delta activation of another scope completes in the same cycle. |
| G13 schema | PASS | `TestLedgerSchemaHasNoForeignKeys`: no foreign key on or referencing the six tables, and `fact_category` and `stable_fact_key` are NOT NULL. RED: a planted FK and a dropped NOT NULL inside a rolled-back transaction are reported. Also asserted: `changed_since_key_state` is an ordinary table (`relkind = 'r'`), because the delete identifies rows by `ctid`. |
| G14 switch off (the link domain issues no SQL; generation retention still prunes the ledger tables, #7127 PR-3d) | PASS | `TestChangedSinceLinkRunnerIsOffByDefault` (`cmd/reducer`): with no environment and with `false`, `changedSinceLinkRunnerFor` returns nil (the runner is not constructed) and a database double that fails on any query, exec or begin sees none. |
| G15 break keeps state | PASS | `TestChainBreakKeepsStateThenIncremental`: root F0, then two delta breaks (`overlay_unproven`, `prior_mismatch`) keep the state rows and generation. The next full generation links `incremental` F0 -> F3, the state equals the aggregate of F3, and only the 8 changed keys are written. |
| G16a poison bound | PASS | Runner `TestRunnerPoisonsAFailingLinkAfterMaxAttempts`: a link made to fail every time (a planted trigger) is tried exactly 5 times, then one `link_poisoned` break; the cursor passes the activation once; state rows and `state_generation_id` unchanged; a healthy scope links both its activations in the first cycle. Store `TestFailingLinkIsPoisonedAfterMaxAttempts`: `next_attempt_at` strictly increasing (30 s, 60 s, 120 s, 240 s), deferred inside the backoff. RED: the same check on a runner with no effective limit reports violations. |
| G16b non-counting | PASS | `TestNonCountingOutcomesNeverPoison`: slots held, cursor held and generation held, each for MaxAttempts + 2 cycles: `attempt_count` 0 and no poison marker. RED: a planted classifier that counts `slot_busy` counts and poisons. `TestRecordFailureSkipsAHeldCursor`: a count is never written without the cursor lock. |
| G16c recovery | PASS | `TestFailingLinkIsPoisonedAfterMaxAttempts`: after the poisoning the next full generation links `incremental` from the kept state, the state equals its aggregate, and the marker is cleared. |
| G16d one outcome per activation | PASS | `TestTwoProcessRunnersLinkEachActivationOnce`: 81 links + 40 breaks = 121 activations across the two processes; link rows equal the reported links; no failure, no poisoning. Like G9, this ran on two OS processes of the compiled test binary with the production `Runner`; the same count held on two built `cmd/reducer` processes (see "G9 on the built binary"). |
| G16e repo rows | PASS | Every new live test is classified in `specs/live-tests.v1.yaml` (`verify-live-tests-ledger.sh`: 502 rows on `origin/main` `944c526081`, all classified); the seven variables are in `go/internal/envregistry` and the generated reference; the telemetry-coverage row lists every new signal. |
| G16f Ifá | N/A | No `fact_work_items` row is created and no graph edge is written, so no Ifá family row and no dead-letter row applies (ruling 8.10). `ifa-determinism` and `ifa-fault-injection` still run in CI because migrations change, and must stay green with the switch off. |

## PR-3e: the prior fence

PR-3e changes the link transaction's lock set (cursor, activating
generation, prior, slot), adds the rebase outcome (`root` link with the
`prior_pruned` break) and fences the backfill insert. G3, G11 and G15 are
extended to the rebase statement and the prior, and the P3 bound of ruling
arb-7127-3d no longer holds: no link names a pruned prior. Proof, races,
mutations and the one known lock wait are in
[7127-changed-since-prior-fence.md](7127-changed-since-prior-fence.md).

## G9 on the built binary

Ruling 8.10 asks for G9 on two reducer processes from the built binary. This
run used two `cmd/reducer` processes built from this branch (Go code
identical to `4b2eacc29b`, go1.27.1 darwin/arm64, `eshu-reducer` sha256
`df2efbe99d95b341…6012e856`), one Postgres (`postgres:18-alpine` 18.6,
`sha256:77f585114c32…1a1873`) and one Neo4j (`neo4j:2026-community`, Neo4j
2026.09.0, `sha256:91fb0bf237c4…fdf4e`; not the compose pin, which this host
did not have). Schema came from the built `eshu-bootstrap-data-plane` (rc 0).

Each process ran with `ESHU_GRAPH_BACKEND=neo4j`,
`ESHU_CHANGED_SINCE_LINK_ENABLED=true`,
`ESHU_CHANGED_SINCE_LINK_POLL_INTERVAL=200ms`, the other link knobs at their
defaults (2 slots, 4 workers, 120 s, 5 attempts), and
`ESHU_GENERATION_RETENTION_ENABLED=false` with
`ESHU_QUERY_PROFILE=local_full_stack`, so retention could not prune the
seeded generations. Both processes were healthy (`/healthz` 200) before the
seed. The seed is the fixture of `TestTwoProcessRunnersLinkEachActivationOnce`
as one SQL transaction: 40 scopes, generations g0 and g1 (full, 4,000 facts
each) and g2 (delta, active), three journal rows per scope. The bootstrap's
`eshu:global` scope adds one sweeper activation. The backlog drained within
5 s of the commit. The reference is one process of the same binary on a
second database, same bootstrap and seed.

| Check | Two processes | Reference (one process) |
| --- | --- | --- |
| Outcomes, from each process's `eshu_dp_changed_since_links_total` | A: 21 root, 21 incremental, 21 breaks; B: 20 root, 19 incremental, 19 breaks | 41 root, 40 incremental, 40 breaks |
| Activations, and outcomes from the `changed-since link` log lines | 121, and 121 lines (A 63, B 58); no (scope, activation_seq) twice; 3 scopes had activations linked by both processes, in order | 121 |
| `cursor_locked` (non-counting) | A 8, B 7 | 0 |
| `slot_busy` (non-counting) | A 205, B 222 | 360 |
| Failures, poisonings | no `link_failures_total` series, `link_poisoned_scopes` 0, no `changed-since link failed` or `poisoned` log line | same |
| Cursor state | `attempt_count` 0 and no poison marker on every cursor; every cursor at its last activation | same |
| Ledger equality | md5 of each table (activations, key state 160,000 rows, link deltas 5,160, bucket counts 40, links 80, cursors 40; `race-*` scopes, timestamps excluded) equal to the reference; `eshu:global` rows equal in count | reference |

The only ERROR lines in either log were `claim partition lease: context
canceled` from the shared-projection runner at SIGTERM, after the drain.
Commands and raw output (launcher, seed SQL, digest SQL, logs, metrics) stayed
in the operator scratchpad; both containers were removed afterwards.

## Scale run

The scale run (G5, G6, G8) measured the link statements before the ruling
8.10 changes; those changes add cursor columns, the failure accounting and a
transaction deadline, and leave `RootLinkSQL` and `IncrementalLinkSQL`
unchanged.

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

What this first scale run does not measure (G7 and G8 were rerun under the
load rule; see their rows):
- the timing gates under a valid load;
- cold cache;
- the read-API latency of a real deployment with the domain on and off. That
  belongs to the dark deployment (ruling 8.7).


The per-round G7 and G8 tables, the G8 stall diagnosis and the fix with
its proofs are in
[7127-changed-since-link-writer-g8.md](7127-changed-since-link-writer-g8.md).

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
| `IncrementalLinkSQL` delete (`del`) | Tid Scan by the `ctid` array from `diff`; no indexable predicate on the target | P1 (`TestIncrementalLinkPlanClassUnderPlantedStatistics`) and G4 |
| `IncrementalLinkSQL` upsert (`ups`) | arbiter-index probe per row, no join | G4 state test (no seq scan of the state table) |
| `clearStateQuery` (root after a digest change) | PK prefix `scope_id` | Deletes nothing on a first root; same prefix as the G4 access |
| `lockCursorQuery`, `advanceCursorQuery`, `ensureCursorQuery` | cursor PK equality | One row per scope |
| `nextActivationQuery` | `changed_since_activations_scope_seq_idx` | `(scope_id, activation_seq)` with `LIMIT 1` |
| `backlogScopesQuery` (the runner's candidate read) | journal and cursor only | Plan-checked in `TestLinkDeltaChainReadUsesPrimaryKey` at the 8.4 fixture size (5,000 journal rows, 500 cursors, a fifth backing off): it reads only those two tables, 1.62 ms execution |
| `backlogStatsQuery`, `orphanScopesQuery` | full scan of the journal and the cursor | The journal holds one row per activation, bounded by generation retention once PR-3d lands (T0: about 5,161 activations per 7 days on ops-qa); the cursor has one row per scope |
| `recordAttemptQuery`, `poisonActivationQuery`, `createCursorsQuery` | cursor PK equality; the journal's unique `(scope_id, generation_id)` probe | One row per scope |
| `backfillChainsQuery`, `journalActiveGenerationsQuery` | `ingestion_scopes` scan, journal probes by the unique `(scope_id, generation_id)` | One row per scope |
| `deleteOrphanScopeStatements` | each table's `scope_id` key prefix | Deleted scopes only |
| `ledgerSizeQuery` | catalog only | Reads no table rows |

Join audit of the ledger statements (arbiter ruling arb-7127-g8, section 4):

| Statement or CTE | Join | Can it go quadratic on a wrong estimate |
| --- | --- | --- |
| `cur` | none | No: one index or bitmap scan, a sort, an aggregate |
| `diff` | FULL JOIN on two equality columns | No: a full join is a hash or merge join, never a nested loop; an over-estimate may choose a sequential scan of the state table, which is O(fleet) and bounded |
| `ins`, `ups`, `bk`, `lnk` | none | No: `ups` probes the arbiter index once per row |
| `del` | none after the fix (Tid Scan) | Was the defect: the key join could plan a nested loop that rescans `diff` per state row |
| `RootLinkSQL`, `clearStateQuery` | none | No |
| `backlogScopesQuery`, `backlogStatsQuery`, `orphanScopesQuery`, the journal statements | joins over the journal and the cursor | Bounded by table size: thousands of journal rows, one cursor row per scope |

## Commands

Every command ran from `go/` unless noted, with `ESHU_POSTGRES_TEST_DSN` set
to the disposable container.

- `go test ./internal/storage/postgres/freshness/links ./internal/reducer/freshness/links -count=1 -timeout 40m` (live): ok, 143.6 s and 72.9 s
- `go test ./internal/storage/postgres -run ChangedSince -count=1` (live, includes G1 and the existing changed-since statement tests): ok
- `go test ./internal/storage/postgres -run TestChangedSinceOracleFragmentsMatchGoConstants -v`: PASS, `scripts/lib/golden-corpus-changed-since-sql-fragments.sh` unchanged
- `go test ./internal/storage/postgres/... ./internal/reducer/... ./cmd/reducer ./internal/telemetry/... ./internal/envregistry -count=1` (no DSN): ok, 107 packages
- `go test -race ./internal/reducer/freshness/links -count=1`: ok
- `docs/internal/evidence/7127-link-writer-scale.py --container eshu-7127-3a-pg --port 25471 --rounds 6` (repo root): rc 0
- `docs/internal/evidence/7127-link-writer-scale.py --container eshu-7127-3a-g7 --port 25472 --start-container --g7-only --valid-rounds 10 --deadline 100m` (repo root): rc 0, 10 valid rounds
- `bash scripts/verify-live-tests-ledger.sh` (repo root): ok, 492 rows before the rebase onto `810f40225e`; see the post-rebase commands below
- `ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash scripts/verify-telemetry-coverage.sh` (repo root): rc 0 (after the coverage row was widened to `go/internal/reducer/freshness/links/*.go`; with `observe.go` alone it flagged `runner.go` as an uncovered stage)
- `mkdocs build --strict --clean --config-file docs/mkdocs.yml` (repo root, via `uv run`): rc 0
- `git diff --cached --check`: rc 0

The fixture SQL is deterministic. The results JSON is a measurement, not a
regenerable artifact: a rerun produces new timings, and the RSS figures follow
the host.

### After the rebase onto `810f40225e` and the review fixes

The ledger migration was then 134 (`#7291` landed 133 on main); it is now 136
after #7305 landed 134 and 135 (see the next section's first bullet).

| Command | rc |
| --- | --- |
| `go build ./...` | 0 |
| `go test ./internal/storage/postgres/freshness/links ./internal/reducer/freshness/links -count=1` (live) | 0 (66.3 s, 13.3 s) |
| `go test -race` on the same two packages (live) | 0 (60.7 s, 18.8 s) |
| `go test ./internal/storage/postgres -run ChangedSince -count=1` (live, G1 and the existing changed-since tests) | 0 |
| `go test ./internal/storage/postgres/... ./internal/reducer/... ./cmd/reducer ./internal/telemetry/... ./internal/envregistry -count=1` | 0 (107 packages) |
| `verify-live-tests-ledger.sh` | 0 (496 rows) |
| `verify-env-registry-doc.sh`, `verify-package-docs.sh`, `verify-dirgate.sh --all`, `verify-markdown-line-cap.sh --all` | 0 each |
| `verify-telemetry-coverage.sh`, `verify-performance-evidence.sh` (base `origin/main`) | 0, 0 |
| `mkdocs build --strict --clean` | 0 |
| `git diff --check origin/main...HEAD` | 0 |

## Notes for the PR body

- Migration number: this PR ships `136_changed_since_link_ledger.sql`. It
  was 133 until #7291 landed 133 on main, then 134 until #7305 landed 134
  and 135. Open PR #7301 adds 133-138 (136 included) and open PR #7206 adds
  134; whoever lands after this renumbers and re-pins the golden digest,
  count and checksum.
- Naming: the store is `storage/postgres/freshness/links` and the domain
  `reducer/freshness/links`, because the dirgate ledger pins
  `storage/postgres` at 321 files and the naming-glue gate rejects
  `changedsince`.
- G2 is FAIL (not built): delta activations are `overlay_unproven` breaks.
- G16f: no Ifá rows, because no `fact_work_items` row is created and no graph
  edge is written; `ifa-determinism` and `ifa-fault-injection` still run on
  the migration change.
- G9 and G16d ran on two processes of the compiled test binary and on two
  built `cmd/reducer` processes against Postgres and Neo4j; the ledger of the
  two-process run equals a one-process reference ("G9 on the built binary").
- PR-3c must use the LATERAL form of the chain read (G4).
- Known gap (ruling 8.10 item 10): a poisoned link is not listed by
  `list_dead_letter_work_items` or the status surface; the cursor row is the
  durable record.
- All new live tests are `class: scheduled`; they are local and scheduled
  proof, not the blocking CI lane.
- G8 found a product defect: the incremental link's delete joined the state
  table to the diff CTE, and when a scope was absent from the statistics the
  planner made it quadratic (three of four n=4 links cancelled at 120 s). The
  arbiter ruled the fix (arb-7127-g8): delete by `ctid` with no indexable
  predicate on the target, plus a Go row-count invariant. P1-P8 pass on the
  fixed statement, and G8 now passes with 10 valid windows per n. Small
  fleets (about 100 scopes or fewer) or a `digest_version` change could hit
  the defect; a fleet of ops-qa's size probably not in steady state.
- G7 root/bare_b: median 1.841 passes 3x; one round reached 4.425.
- Review P3(f) is fixed: the advisory-class collision test derives the
  two-integer lock classes from the code instead of a hand-kept list (six
  sites today, including migration 062's class 5318), fails on any site it
  cannot resolve, and has a seeded RED (a planted SQL and Go site).
- Record-only review items (P3): (b) `LedgerStats.DeltaBytes`/`DeltaRows` are
  read each cycle but not exported as gauges; (c) the state half of G4 has no
  seeded RED of its own; (d) one fixture row with a tombstone kind sorting
  below the active kind would pin the `kind` FILTER in G1.

Performance Evidence: G3/G4 plan shapes above; G5-G8 in "Scale run", measured
with `7127-link-writer-scale.py` on `7127-link-writer-fixture.sql`.
No-Regression Evidence: the domain is off by default and issues no SQL (G14),
so reducer, projector and read-API behaviour is unchanged until it is enabled.
Observability Evidence: `eshu_dp_changed_since_links_total{link_kind,outcome}`,
`eshu_dp_changed_since_link_retries_total{reason}`,
`eshu_dp_changed_since_chain_breaks_total{reason}`, the link duration,
delta-row and key histograms, the backlog, lag and state-size gauges, the span
`reducer.changed_since_link`, and one `changed-since link` log line per link.
