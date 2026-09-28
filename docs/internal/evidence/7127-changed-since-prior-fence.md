# 7127 PR-3e: fence the prior generation in the changed-since link writer

PR-3e of #7127 implements C1-C5 of arbiter ruling arb-7127-3d. The link
writer now locks the generation it links from (the prior, X) as well as the
generation it links to (G). When X is already gone, the writer rebases instead
of writing a link that names a pruned generation. The backfill inserts each
activation through its locked generation row. The orphan probe is exported as
a gauge.

Source binding: branch `perf/7127-pr3e-prior-fence` on `origin/main`
`c8b5145212`. PostgreSQL `postgres:18-alpine` 18.6 (aarch64), disposable
container `7127e-pg`, removed afterwards. No NornicDB and no Neo4j.

## Root cause (arbiter ruling arb-7127-3d, section 5)

Retention's ledger delete is complete only if every ledger writer holds `FOR
KEY SHARE` on every generation it names until it commits. The writer held G
but not X, and the backfill held nothing. A link `(X -> G)` could therefore
commit after X was pruned: an orphan that no retention rule finds again.
`TestRetentionRacesLinkWriter`, with staggered starts, reproduces it on the
old writer: `race 12: probe = 1/0/0` and `race 8: probe = 1/0/0`, two runs out
of two.

## Design as built

| Id | Change | Where |
| --- | --- | --- |
| C1 | After G's lock and before the slot, when the state generation X is set and differs from G: the plain existence read of X (`generationExistsQuery`), then `FOR KEY SHARE SKIP LOCKED` (`lockGenerationQuery`). No row back means `generation_locked`, non-counting. X absent means a rebase | `prior.go` `fencePrior`, `link.go` `linkInTx` |
| C2 | `RebaseLinkSQL` shares the incremental statement's `diff`, `del` and `ups` CTEs byte for byte (`stateDiffCTE`, `stateMoveCTEs`), so only changed keys are written. It records a `root` link with an empty prior and writes no delta or bucket rows. The result has `Kind` root, `Break` `prior_pruned` and `RebasedFrom` X; the cursor advances as for a link and the poison marker clears | `link_sql.go`, `prior.go` `rebase` |
| C3 | The backfill insert is `INSERT ... SELECT ... FROM scope_generations AS generation WHERE ... FOR KEY SHARE OF generation SKIP LOCKED ON CONFLICT DO NOTHING`. A row not inserted ends that scope's chain for the pass | `journal_sql.go` `insertActivationQuery`, `journal.go` `backfill` |
| C4 | The rule for any later writer, `pairwise` first, is in the store's `AGENTS.md` and `README.md` ("The writer rule") | docs |
| C5 | `JournalStore.Orphans` runs the ruling's probe. The runner records `eshu_dp_changed_since_ledger_orphans{kind}` at most once a minute | `journal.go`, `reducer/freshness/links/observe.go` |

The lock order is cursor, G, X, slot. Every lock is non-blocking.
`IncrementalLinkSQL` is byte-identical after the refactor (sha256
`76ae2046…d8d6a231` before and after); `RootLinkSQL` is unchanged.

Runner accounting: a rebase is `links_total{link_kind="root",
outcome="linked"}`, counted as `Linked` so the scope keeps draining, plus one
`chain_breaks_total{reason="prior_pruned"}`. Its log line and span carry
`rebased_from_generation_id`.

### Why the probe is throttled

The probe scans the link, activation and bucket-count tables. It was
measured on a fixture of 25,000 generations, 25,000 links, 192,000 bucket
rows and 25,000 activations (1,000 scopes × 25, about ops-qa's retained
scale). Five runs took 93-105 ms with JIT on; three took 54-65 ms with JIT
off. The plan is three hash anti joins. Busy runner cycles follow each other
at once, so the probe runs at most once per `orphanProbeInterval` (one
minute), and the gauge keeps the last value. This is a constant, not a knob.

## Proof

Every run used `ESHU_POSTGRES_TEST_DSN` against the disposable container.
The host load average was 20-58 during the runs.

| Proof (ruling, "FOR THE COORDINATOR TO FILE" item 1) | Test | Result |
| --- | --- | --- |
| G1 and G15 on the rebase branch | `TestRebaseOnPrunedPriorWritesOnlyChangedKeys` | PASS. The state equals x1's independent aggregate. The rows rewritten (by `xmin`) and deleted equal the keys an incremental link of the same facts writes (5 written and 2 deleted; 9 state rows before, 8 after). The link row is `rb1||root|0`. No delta or bucket row. The probe is 0/0/0. The next link from the rebased state is an incremental whose state equals rb2's aggregate |
| G3 plan shape of the rebase statement | `TestRebaseLinkPlanShape` | PASS for a 600-key and a 45,000-key scope among 20 filler scopes × 20,000 state rows, under absent, fresh, never and over statistics: one `fact_records` index scan whose condition names the scope and g1, a Tid Scan delete, no rescanning nested loop, the state side by `changed_since_key_state_pkey` on `scope_id`. The analyzed run fires no trigger. REDs: a scope predicate on the delete target fails (a); with both `fact_records` (scope, generation) indexes dropped, G3 fails |
| G11 extended to X | `TestPriorHeldByRetentionIsGenerationLocked` | PASS. X held `FOR UPDATE` gives `generation_locked` in under 1 s; the cursor is unmoved, `attempt_count` is 0 and no link is written. After the release the link is an incremental from X |
| G11 with a committed updater (the #7115 shape) | `TestPriorLockWithACommittedUpdaterDoesNotWait` | PASS, three cases, each under 1 s. A running `KEY SHARE` member, a committed non-key update and an open no-key update give a link. The same with retention's `FOR UPDATE SKIP LOCKED`: retention gets no row (the member's lock carries to the new version) and the link proceeds. A committed update with retention holding the new version gives `generation_locked` |
| At least 20 races of link against prune on X | `TestRetentionRacesLinkWriter` (in-process, each side on its own pooled session, 24 races, staggered 0-14 ms) | PASS. Endings: generation_locked 13 (each retry rebased), link_then_prune 6, skip 4, rebase 1. The probe is zero after every race and after the final prunes; no headless delta |
| Same, on OS processes | `TestLinkAndRetentionProcessesRace` (one process runs the production `Runner`, two run retention; compiled test binary, not the built `cmd/reducer`) | PASS 3/3. Runs had 60, 58 and 56 links, of which 4, 5 and 3 were rebases, and 25, 40 and 37 generation_locked retries. The orphan probe was `{0 0 0}` every time. The longest retention batch took 894, 514 and 417 ms (gate 1 s) |
| P3 of the ruling, inverted | `TestRetentionBoundP3`, `TestLinkWriterDoesNotWaitOnRetentionInFlight` | PASS. After X is pruned the writer rebases and the probe stays 0/0/0; `get_changed_since` from X answers `retention_expired`. With retention in flight on X, the writer gets `generation_locked` without waiting, then rebases after the commit |
| C3 | `TestBackfillEndsTheChainAtAHeldGeneration`, `TestBackfillInsertFencesItsGeneration` | PASS. With b1 held, the pass journals b0 (backfill) and b3 (sweeper) in under 1 s. A pruned generation inserts 0 rows. An inserted row keeps retention off its generation until the pass commits |
| C5 | `TestOrphansCountsPlantedRows`, `TestRecordGaugesReportsLedgerOrphans` | PASS. Planted rows read 2/1/1. The gauge carries kinds `link`, `activation` and `bucket_group`, and the probe ran once in two back-to-back cycles |
| Runner accounting | `TestRebaseIsALinkAndAPriorPrunedBreak` | PASS. `linked` 1, `prior_pruned` 1, log field `rebased_from_generation_id` |
| G14 (switch off, zero SQL) | `TestChangedSinceLinkRunnerIsOffByDefault` | PASS, unchanged |

Analyzed rebase plans at fresh statistics (from the test log):

| Scope | `fact_records` | state side (diff) | state delete |
| --- | --- | --- | --- |
| 600 keys | Index Scan `fact_records_scope_generation_idx`, cond scope and g1, 600 rows, 36 buffers | Index Scan `changed_since_key_state_pkey` on `scope_id`, 600 rows, 269 buffers | Tid Scan, 60 rows |
| 45,000 keys | Index Scan `fact_records_scope_generation_idx`, cond scope and g1, 45,000 rows, 2,539 buffers | Bitmap Heap Scan on `changed_since_key_state_pkey` by `scope_id`, 45,000 rows, 2,931 buffers | Tid Scan, 4,500 rows |

RED before the fix (same tests, old writer): the rebase, G11-on-X, P3,
in-flight, C3 and C5 tests failed as intended. `TestRetentionRacesLinkWriter`
found an orphan (`probe = 1/0/0`) in two runs out of two.

Mutation checks (one production site mutated, test run, file restored from
a copy):

| Mutation | Test | Result |
| --- | --- | --- |
| no lock on X in `fencePrior` | `TestPriorHeldByRetentionIsGenerationLocked` | FAIL (`LinkNext = <nil>, want generation_locked`) |
| same | `TestRetentionRacesLinkWriter` | FAIL (`race 12: probe = 1/0/0`) |
| rebase through `RootLinkSQL` | `TestRebaseOnPrunedPriorWritesOnlyChangedKeys` | FAIL (every key rewritten) |
| backfill keeps going after a row not inserted | `TestBackfillEndsTheChainAtAHeldGeneration` | FAIL (b2 journaled with prior b1) |

## The #7115 shape on the lock statement, and the one wait

A shim on 18.6 held the lock statement's snapshot open, using a `pg_sleep`
InitPlan in its qual, while the other sessions acted. D is the link's lock.

| Case | D |
| --- | --- |
| running `KEY SHARE` member, committed non-key update after D's snapshot, open no-key update | no wait (2.006 s = the sleep) |
| same, retention `FOR UPDATE SKIP LOCKED` on the new version | retention gets no row; D no wait (2.009 s) |
| no member, committed non-key update after D's snapshot, retention `FOR UPDATE` then `DELETE` held 6 s | **waits** for retention's transaction (6.84 s), then gets no row: `generation_locked` |

- The last case follows the update chain (`heap_lock_updated_tuple`), which
  ignores `SKIP LOCKED`. It applies to the activating generation's lock that
  PR-3a shipped, and now to the prior's.
- It needs a non-key update of the generation to commit between the lock
  statement's snapshot and its row lock, while retention holds the new
  version. The only updater of superseded generation rows found is the
  projector Heartbeat supersede statement (`projector_queue_sql.go`), for a
  generation old enough to prune.
- It is not a deadlock: retention never waits on anything a link holds. The
  wait is bounded by retention's transaction and by the link's context
  deadline. The lock runs before `SET LOCAL statement_timeout`, so that
  timeout does not bound it.
- The fresh-snapshot cases are pinned by
  `TestPriorLockWithACommittedUpdaterDoesNotWait`. The old-snapshot case is
  pinned by `TestGenerationLockOldSnapshotWaitsOnRetention`. It derives a
  paused variant from the shipped lock statement (a `pg_sleep` InitPlan in
  its qual) and holds retention's `FOR UPDATE` plus `DELETE` for 3 s. Three
  runs out of three: the lock waits until retention commits (3.003 s) and
  returns no row. The control, without the committed update, skips at once
  (1.002-1.005 s, the pause only). Whether to bound the wait (for example with
  `lock_timeout` around the two generation locks) is with the arbiter
  (arb-7127-3e-wait).

## Commands

| Command (from `go/` unless noted) | rc |
| --- | --- |
| `go test ./internal/storage/postgres/freshness/links -count=1 -timeout 40m` (live) | 0 (521.9 s) |
| `go test ./internal/reducer/freshness/links -count=1` (live) | 0 (35.4 s) |
| `go test -race ./internal/reducer/freshness/links -count=1` (live) | 0 (32.9 s) |
| `go test ./cmd/reducer -run TestChangedSinceLinkRunnerIsOffByDefault -count=1 -v` | 0 |
| `go test ./internal/reducer/freshness/links -run TestLinkAndRetentionProcessesRace -count=1 -v`, three times (live) | 0, 0, 0 |
| `bash scripts/verify-live-tests-ledger.sh` (repo root) | 0 (553 rows) |

No-Regression Evidence: a full link now runs two primary-key statements on
`scope_generations` for its prior: the existence read and the `FOR KEY SHARE
SKIP LOCKED` lock. With pgbench in the container (`-M prepared -c 1 -T 5`,
25,000 generations, three interleaved rounds) the two added 0.044-0.053 ms
per transaction over an empty-transaction control (0.054-0.064 ms against
0.010-0.011 ms). The smallest link measured here takes 7.5 ms, and ops-qa's
largest takes about 13 s. The lock dirties the prior's row as the activating
generation's lock already does. A rebase writes the same state rows as an
incremental link of the pair and no delta or bucket rows, so it writes less
than the link it replaces. With the switch off nothing runs (G14). The
backfill's insert through `scope_generations` is one primary-key probe per
row, at most 64 rows per scope.

Observability Evidence: `eshu_dp_changed_since_ledger_orphans{kind}` (new,
the orphan probe, sampled at most once a minute),
`eshu_dp_changed_since_chain_breaks_total{reason="prior_pruned"}`,
`eshu_dp_changed_since_links_total{link_kind="root",outcome="linked"}` for a
rebase, `eshu_dp_changed_since_link_retries_total{reason="generation_locked"}`
for a held prior, the span attribute `changed_since.rebased_from_generation_id`,
the log field `rebased_from_generation_id`, and the ERROR log `changed-since
ledger orphan probe failed`.
