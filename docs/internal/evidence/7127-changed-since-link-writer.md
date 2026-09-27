# 7127 PR-3a: the dark changed-since link writer

PR-3a of #7127 adds the changed-since link ledger and its writer. Migration 134
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
| G4 plan, deltas and state | PASS | `TestLinkDeltaChainReadUsesPrimaryKey`: 2,000,000 delta rows over 500 scopes and 5,000 links; the probed scope holds 0.2% of the rows over 4 links. The access is the primary key with `scope_id`, `generation_id` and `prior_generation_id` in the index condition, at most 10 buffers per row plus 64. It holds in `force_custom_plan` and `force_generic_plan`, with statistics from 13k rows (autovacuum off) and after ANALYZE, and it reads 1,600 rows. RED: with the key dropped, the assertions fail. `TestLinkStatementPlanShape`: 3,844,080 state rows over 800 scopes; the state side reads `changed_since_key_state_pkey` by `scope_id` with no sequential scan for the 600-key and 45,000-key scopes; the 600-key link took 5.3 ms against the 100 ms gate. See deviation 4. |
| G5 temp files | PASS | Scale run: `pg_stat_database.temp_files` and `temp_bytes` deltas were 0 across the root and the incremental 1.0x link at `work_mem` 256MB, and 0 again with 2 and 4 concurrent links. |
| G6 RssAnon | PASS | Scale run: the peak single-backend `RssAnon` during a 1.0x incremental link was 464,480 kB (453.6 MiB), under the 524,288 kB (512 MiB) gate with an 11% margin. |
| G7 timing ratio | PASS | Unattended run (`7127-link-writer-scale.py --g7-only --valid-rounds 10`, raw: `7127-link-writer-g7-results.json`), which waits for the host's 1-minute load to fall below 18 before each round: 10 valid rounds (load at start 9.9-17.8), bare_b, L1b and root interleaved with the first mover rotated, all rolled back. L1b/bare_b paired median **1.11** (range 0.35-1.34; 8 of 10 at or under 1.3), gate 1.3x. Root/bare_b paired median **1.67** (range 0.47-3.36; 9 of 10 at or under 3.0), gate 3x: PASS on the median, one round (round 3, 3.356) above 3x; per-round table under "G7 rounds". The spread is wide: two rounds had bare_b outliers (22.3 s and 13.5 s against 4.8-6.9 s elsewhere) that put L1b/bare_b below 0.4, and one root sample (21.1 s against 8.8-12.6 s) is the only root ratio above 3x. The first scale run's 6 rounds were all invalid (load 36.7-70.9) and are not pooled. |
| G8 cap proof | PARTIAL | Scale run, 1, 2 and 4 concurrent 1.0x incremental links on different scopes (4 slots). **Memory:** summed peak `RssAnon` was 453.6 MiB, 908.5 MiB and 1,381.8 MiB; the n=4 sampled peak falls below 4 x 453.6 MiB because the four backends did not peak in the same sweep. **Read probe** (a 2,000-row page read from `fact_records` every 200 ms): p50 1.4 / 2.6 / 1.4 ms and p95 2.6 / 34.2 / 17.3 ms. **Walls:** 6.3 s; 29.9 and 29.1 s; 30.5-32.3 s. The host load rose from 19.1 to 81.7 across the run, so the wall times are not comparable between n values and do not prove or disprove the 2-slot default. The memory figures support the ruling's bound of two links near 1 GiB. |
| G9 fence | PASS | Store: `TestLinkFenceOneWinnerPerActivation` (24 rounds of 4 concurrent writers: one link and one cursor advance per round, losers `cursor_locked` in under 1 s) and `TestCursorHeldReturnsRetryWithoutWaiting`. Runner, two OS processes of the compiled test binary (`TestTwoProcessRunnersLinkEachActivationOnce`, the production `Runner` in each): 40 scopes, 121 activations; 131 `cursor_locked` races, none taking 1 s; `attempt_count` 0 on every cursor; every cursor at its last activation. The reducer binary itself was not run: it needs a graph backend, which this proof does not. |
| G10 kill and rerun | PASS | `TestLinkKilledMidStatementRerunsToIdenticalRows`: the incremental statement is held on a row lock and its backend terminated. No partial rows, no cursor move, one counted `connection_lost` (attempt 1, backoff 30 s); a retry inside the backoff is deferred; after it, the rerun links and every ledger row equals an uninterrupted reference. |
| G11 lock outcomes | PASS | `TestGenerationLockedIsRetryable`: generation held `FOR UPDATE` gives `generation_locked` in under 1 s, `attempt_count` 0, cursor unmoved, link after release. `TestDeltaWithoutRootAndPrunedBeforeLink`: an absent generation is a `pruned_before_link` break with the state kept. |
| G12 slot outcomes | PASS | Store `TestSlotBusyBlocksFullLinksOnly`; runner `TestRunnerMovesOnPastABusySlot`: with every slot held the full link is a non-counting `slot_busy` (`attempt_count` 0) and the runner moves on; a delta activation of another scope completes in the same cycle. |
| G13 schema | PASS | `TestLedgerSchemaHasNoForeignKeys`: no foreign key on or referencing the six tables, and `fact_category` and `stable_fact_key` are NOT NULL. RED: a planted FK and a dropped NOT NULL inside a rolled-back transaction are reported. |
| G14 switch off | PASS | `TestChangedSinceLinkRunnerIsOffByDefault` (`cmd/reducer`): with no environment and with `false`, `changedSinceLinkRunnerFor` returns nil (the runner is not constructed) and a database double that fails on any query, exec or begin sees none. |
| G15 break keeps state | PASS | `TestChainBreakKeepsStateThenIncremental`: root F0, then two delta breaks (`overlay_unproven`, `prior_mismatch`) keep the state rows and generation. The next full generation links `incremental` F0 -> F3, the state equals the aggregate of F3, and only the 8 changed keys are written. |
| G16a poison bound | PASS | Runner `TestRunnerPoisonsAFailingLinkAfterMaxAttempts`: a link made to fail every time (a planted trigger) is tried exactly 5 times, then one `link_poisoned` break; the cursor passes the activation once; state rows and `state_generation_id` unchanged; a healthy scope links both its activations in the first cycle. Store `TestFailingLinkIsPoisonedAfterMaxAttempts`: `next_attempt_at` strictly increasing (30 s, 60 s, 120 s, 240 s), deferred inside the backoff. RED: the same check on a runner with no effective limit reports violations. |
| G16b non-counting | PASS | `TestNonCountingOutcomesNeverPoison`: slots held, cursor held and generation held, each for MaxAttempts + 2 cycles: `attempt_count` 0 and no poison marker. RED: a planted classifier that counts `slot_busy` counts and poisons. `TestRecordFailureSkipsAHeldCursor`: a count is never written without the cursor lock. |
| G16c recovery | PASS | `TestFailingLinkIsPoisonedAfterMaxAttempts`: after the poisoning the next full generation links `incremental` from the kept state, the state equals its aggregate, and the marker is cleared. |
| G16d one outcome per activation | PASS | `TestTwoProcessRunnersLinkEachActivationOnce`: 81 links + 40 breaks = 121 activations across the two processes; link rows equal the reported links; no failure, no poisoning. |
| G16e repo rows | PASS | Every new live test is classified in `specs/live-tests.v1.yaml` (`verify-live-tests-ledger.sh`: 492 rows, all classified); the seven variables are in `go/internal/envregistry` and the generated reference; the telemetry-coverage row lists every new signal. |
| G16f Ifá | N/A | No `fact_work_items` row is created and no graph edge is written, so no Ifá family row and no dead-letter row applies (ruling 8.10). `ifa-determinism` and `ifa-fault-injection` still run in CI because migrations change, and must stay green with the switch off. |

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

What this run does not measure:
- the timing gates under a valid load;
- cold cache;
- the read-API latency of a real deployment with the domain on and off. That
  belongs to the dark deployment (ruling 8.7).


### G7 rounds (unattended run)

Raw data: `7127-link-writer-g7-results.json`. Every round was valid by the load rule. The root basis is not stated in ruling 8.6, so both the median and the maximum are reported: the median passes, and one round is above 3x.

| Round | Load at start | First mover | bare_b (s) | L1b (s) | root (s) | L1b/bare_b | root/bare_b |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 0 | 15.82 | bare_b | 4.90 | 5.29 | 9.15 | 1.079 | 1.868 |
| 1 | 15.83 | l1b | 22.30 | 7.73 | 10.45 | 0.347 | 0.468 |
| 2 | 12.90 | root | 6.83 | 8.91 | 11.32 | 1.305 | 1.658 |
| 3 | 12.02 | bare_b | 6.28 | 8.38 | 21.07 | 1.336 | 3.356 (max, above 3x) |
| 4 | 10.92 | l1b | 13.45 | 5.08 | 12.61 | 0.377 | 0.938 |
| 5 | 17.83 | root | 6.93 | 6.03 | 11.84 | 0.870 | 1.708 |
| 6 | 14.96 | bare_b | 4.82 | 5.13 | 10.24 | 1.065 | 2.123 |
| 7 | 14.54 | l1b | 6.25 | 7.14 | 10.16 | 1.141 | 1.624 |
| 8 | 12.05 | root | 5.61 | 6.79 | 8.80 | 1.211 | 1.568 |
| 9 | 9.92 | bare_b | 5.49 | 6.49 | 9.21 | 1.180 | 1.676 |

Paired medians: L1b/bare_b 1.110 (gate 1.3x, PASS); root/bare_b 1.667 (gate 3x, PASS on the median; maximum 3.356 in round 3).

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
| `backlogScopesQuery` (the runner's candidate read) | journal and cursor only | Plan-checked in `TestLinkDeltaChainReadUsesPrimaryKey` at the 8.4 fixture size (5,000 journal rows, 500 cursors, a fifth backing off): it reads only those two tables, 1.62 ms execution |
| `backlogStatsQuery`, `orphanScopesQuery` | full scan of the journal and the cursor | The journal holds one row per activation, bounded by generation retention once PR-3d lands (T0: about 5,161 activations per 7 days on ops-qa); the cursor has one row per scope |
| `recordAttemptQuery`, `poisonActivationQuery`, `createCursorsQuery` | cursor PK equality; the journal's unique `(scope_id, generation_id)` probe | One row per scope |
| `backfillChainsQuery`, `journalActiveGenerationsQuery` | `ingestion_scopes` scan, journal probes by the unique `(scope_id, generation_id)` | One row per scope |
| `deleteOrphanScopeStatements` | each table's `scope_id` key prefix | Deleted scopes only |
| `ledgerSizeQuery` | catalog only | Reads no table rows |

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
- `bash scripts/verify-live-tests-ledger.sh` (repo root): ok, 492 rows
- `ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash scripts/verify-telemetry-coverage.sh` (repo root): rc 0 (after the coverage row was widened to `go/internal/reducer/freshness/links/*.go`; with `observe.go` alone it flagged `runner.go` as an uncovered stage)
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
