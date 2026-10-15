# 7127 PR-3b: the projector Ack journals its activation (dark)

PR-3b of #7127 makes the projector Ack write the `changed_since_activations`
row of the generation it activates, in its own transaction, so the link writer
does not wait for the reducer's sweeper. It is dark:

- `ESHU_CHANGED_SINCE_LINK_ENABLED` stays off by default, and no environment
  changes in this work. With it off, Ack sends exactly the statements it sent
  before and no ledger SQL.
- Nothing reads the ledger. The read path is unchanged: main still serves the
  full two-generation diff (`changed_since_sql.go`). The last measurement on
  the 12k-file scope was 11.3-18.2 s on the API and 11.0-13.7 s on MCP
  (11-18 s), and PR-3b does not change it. PR-3c owns the ledger read.
- Before any environment turns the flag on for the ingester, including the QA
  environment in dark mode, a built-binary run with the ingester and the
  reducer, the flag on, and the Ack journal, the journal pass, the link writer
  and retention all running must pass in BIN1 (#7401). This note records that
  condition; #7401 is not edited here.

Source binding: branch `perf/7127-pr3b-ack-journal` on `origin/main`
`eef372440`. PostgreSQL 18.6 (`postgres:18`, aarch64) in two disposable
containers: one with `pg_stat_statements` preloaded (`track = all`), one
without it, as the CI readiness service runs. The host had 16 CPUs and load1
between 13 and 70 during the runs, so no wall time below is a result.

## Design as built

The arbiter rulings of 2026-10-15 on #7127 (mode 3) set the shape. With
`ProjectorQueue.ChangedSinceJournalEnabled` on (the ingester and
bootstrap-index set it from `linksfreshnessstore.Enabled`, the parse the
reducer uses), two Ack statements change. Both are derived from the shipped
constants, and `TestAckJournalStatementsDeriveFromShippedQueries` pins the
prefixes:

1. `supersedeProjectorActiveGenerationReturningQuery`: the active supersede
   plus `RETURNING generation_id`. Exactly one row is the prior; none (a first
   activation) or several give a NULL prior, as the sweeper writes.
2. `activateAndJournalProjectorGenerationQuery`: one statement with three
   CTEs. `activated` is the shipped activate `UPDATE` with `RETURNING`. `gate
   AS MATERIALIZED` evaluates `linksfreshnessstore.AckJournalGateSQL`,
   `pg_try_advisory_xact_lock_shared(7127, 0)`, once for the activated row.
   `journaled` inserts `source = 'ack'` with the generation's `activated_at`,
   `ON CONFLICT (scope_id, generation_id) DO NOTHING`, only when the gate was
   acquired. The statement returns `activated`, `journaled` and `gated`.

The journal pass takes the same key exclusively with `pg_try_advisory_xact_lock`
(`trySlotQuery`, slot 0 of `SlotLockClass`). Neither side waits:

| Ack gate | Pass | Result |
| --- | --- | --- |
| acquired | starts later | the pass's try-lock misses; it skips the cycle |
| missed (a pass holds the key) | running | the Ack writes no row and commits; the next sweeper journals the generation with a NULL prior |
| acquired by two Acks | none | both hold the shared key; the scope row orders Acks of one scope |

A missed gate is accuracy-neutral: a full link is computed from the cursor
state, and the prior only feeds the delta break reasons.

### Conflict domain and lock order

- **Scope row** (`ingestion_scopes`, `FOR UPDATE` by the Ack's first
  statement, held to commit). It orders Acks of one scope, so their
  `activation_seq` values follow their commit order. Retention takes scope
  rows `FOR UPDATE SKIP LOCKED` and skips the scope while an Ack holds it.
- **Journal key** (advisory, `(7127, 0)`): shared for Acks, exclusive for the
  pass, try-only on both sides. It is the only lock the change adds, and it
  is taken last, after every row lock the Ack already held on main.
- **Activation unique key** (`(scope_id, generation_id)`): the Ack's insert
  never waits on it. The pass cannot have an uncommitted insert while the Ack
  holds the gate, and another Ack of the scope waits on the scope row first.
- **Writer rule** (arbiter ruling arb-7127-3d, C4): the Ack's own `UPDATE`s
  hold both generations its row names, so retention cannot prune either until
  the Ack commits.

### Why the gate (the inversion)

Without the gate, a pass can run between the Ack's insert and its commit. If
the scope's active generation has no journal row (it was activated with the
flag off, or before the flag was turned on, which is every scope at first
enablement), the backfill or the sweeper inserts that older generation with a
higher `activation_seq` and commits first. The cursor then ends on the
superseded generation while the newer one is active. Both shapes in the
original plan (a plain INSERT after the activate statement, and an ungated
CTE) fail this way. `TestProjectorAckJournalInversion` reproduces it
deterministically and shows the gate prevents it.

## Results

All tests are tracked under `go/internal/storage/postgres` (hermetic tests
plus `projector_queue_ack_journal_*_live_test.go`, enrolled in the
live-postgres-readiness runner) and `go/internal/storage/postgres/freshness/links`.

| Proof | Test | Result |
| --- | --- | --- |
| Flag off, statements | `TestProjectorAckWithJournalOffSendsTheShippedStatements` (hermetic), `TestProjectorAckJournalOffWritesNothing` (live) | 9 statements, each the shipped constant with the same arguments; none names the ledger, an advisory lock or `RETURNING generation_id`; 0 ledger rows |
| Flag on, statements | `TestProjectorAckWithJournalOnReplacesOnlyTwoStatements` | 9 statements; only the active supersede and the activate statement differ |
| Prefix guards, gate key | `TestAckJournalStatementsDeriveFromShippedQueries`, `TestAckJournalGateUsesTheJournalKey` | both statements start with the shipped ones; the gate is the links constant, derived from `SlotLockClass` and `journalLockSlot` (seeded RED: slot 1 fails); no projector queue source carries an advisory literal |
| Atomicity | `TestProjectorAckJournalAtomicity` | success: one `ack` row, prior g0, `activated_at` equal to the generation's; planted abort after the insert and a deferred commit-time failure: 0 rows, work `running`, generation `pending`, scope unchanged; superseded target: `ErrWorkSuperseded`, 0 rows; re-ack: one row, same seq and prior |
| Prior | `TestProjectorAckJournalPrior`, `TestProjectorAckJournalPriorLive` | superseded g0; first activation NULL; two active generations (planted by dropping the one-active index) NULL |
| Outcomes | `TestProjectorAckJournalOutcomes`, `TestProjectorAckJournalSupersededGenerationIsRefused` | (1,1,t) journaled, (1,0,t) exists, (1,0,f) gate_skipped, (0,0,f) refusal, (1,1,f) error |
| Inversion RED/GREEN | `TestProjectorAckJournalInversion` | ungated, backfill: seq g0=2 > g1=1, link state g0 while g1 is active; ungated, sweeper: seq g0=7 > g1=6, state g0; gated, both: the pass skipped, state g1 |
| Journal held | `TestProjectorAckJournalGateHeldByPass` | with the pass holding the key the Ack activates, writes no row, returns while the key is still held, counts one `gate_skipped`; the next pass journals it and it links as the state |
| Key sharing | `TestAckJournalGateAndJournalPassExcludeEachOther` (links) | an Ack holding the gate makes the pass skip; two Acks share it; a held pass makes it miss |
| Lock fences (pg_locks) | `TestProjectorAckJournalLockFences` | A: the paused Ack holds `advisory 7127/0/2 ShareLock` and no other advisory lock; the link writer on its scope and another scope, a pass (skipped) and retention return; 0 ungranted locks; retention keeps the paused scope's superseded generation and prunes the other's. B: with a link transaction's cursor, generation and slot locks held, the Ack completes and journals. C: with retention holding the scope row, the Ack waits on `transactionid:ShareLock` with the flag off and on alike |
| Contention | `TestProjectorAckJournalContention` | see below |
| Equivalence | `TestProjectorAckJournalEquivalence` | the G1 fixture activated by three Acks and the same history as backfill rows give byte-equal digests: deltas `5bb07c40…`(14), links `32160a21…`(3), buckets `ad773603…`(12), state `47b0e495…`(9); the Ack journal is `g0 NULL`, `g1<-g0`, `g2<-g1`; 0 oracle mismatches |
| Cost gate | `TestProjectorAckJournalCost` | see below |
| Telemetry | `TestProjectorAckJournalRecordsOutcome` | one count per outcome after commit; the gate-skip log carries `scope_id`, `generation_id`, `outcome`; flag off records nothing |
| Wiring | `TestBuildIngesterProjectorServiceWiresChangedSinceJournal`, `TestBuildBootstrapProjectorWiresChangedSinceJournal`, `TestEnabledParsesTheSwitchOnce` | off by default and for `false`, on for `true`; one parse for the three binaries |

### Contention

16 Ack workers, 24 scopes, 10 rounds, two generations per scope per round
(480 Acks), with one journal loop, four link writers and generation retention
running. Commit order comes from a deferred constraint trigger that numbers
each journal row at commit (`aj_audit`).

| Run | Acks ok | Deadlocks | Commit-order violations | Duplicates | Orphans | Scopes off their active generation | Rows by source |
| --- | --- | --- | --- | --- | --- | --- | --- |
| uniform, first run | 480 | 0 | 0 | 0 | 0/0/0 | 0/24 | ack 441, backfill 24, sweeper 12 |
| mixed (every second Ack flag off), first run | 480 | 0 | 0 | 0 | 0/0/0 | 0/24 | ack 197, backfill 24, sweeper 210 |
| uniform, final commit (`pg_stat_statements` server) | 480 | 0 | 0 | 0 | 0/0/0 | 0/24 | ack 426, backfill 24, sweeper 28 |
| mixed, final commit (`pg_stat_statements` server) | 480 | 0 | 0 | 0 | 0/0/0 | 0/24 | ack 182, backfill 24, sweeper 235 |
| mixed, ungated (one local run, not shipped) | 480 | 0 | 4 | 0 | 0/0/0 | 1/24 | ack 240, backfill 24, sweeper 237 |

The last row is a one-off local edit that pointed the flag-on queue at the
ungated statement; it shows the audit sees the inversion under load. The
shipped test runs only the gated statement.

### Cost gate (deterministic; timing-proof rules, rule 2)

One backend runs every Ack (single-connection pool), alternating the flag over
4 rounds of 200 Acks. WAL records and buffer hits come from
`pg_stat_get_backend_wal` and `pg_stat_get_backend_io` (PostgreSQL 18), which
count only that backend; `pg_stat_statements` is gated too when preloaded. The
gate is on the on-minus-off difference of the per-Ack means. Absolute levels
move with the state of the database (they differ between a run alone and a
run inside the whole package suite), the differences do not. Four runs, two
per server, the last two at the final commit inside the full package suite:

| Run | Statements client / `pg_stat_statements` (off = on) | WAL records off, on (diff) | Buffer hits off, on (diff) | Seeded RED caught |
| --- | --- | --- | --- | --- |
| with `pg_stat_statements`, alone | 9 / 14 | 39.97, 44.67 (+4.70); `pg_stat_statements` 42.90, 47.56 (+4.67) | 122.80, 133.91 (+11.11); `pg_stat_statements` 136.76, 147.81 (+11.05) | WAL +17.05, hits +64.97 |
| without it, alone | 9 / not loaded | 39.97, 44.67 (+4.70) | 122.80, 133.91 (+11.11) | WAL +17.05, hits +64.97 |
| with `pg_stat_statements`, suite | 9 / 14 | 39.64, 43.90 (+4.26); `pg_stat_statements` 42.44, 46.69 (+4.24) | 114.99, 127.27 (+12.28); `pg_stat_statements` 128.89, 140.47 (+11.59) | WAL +15.71 |
| without it, suite | 9 / not loaded | 39.96, 44.71 (+4.74) | 220.65, 231.83 (+11.18) | WAL +17.05, hits +205.22 |

Bounds: statements equal, WAL records at most +6, buffer hits at most +40 per
Ack. The seeded RED is a planted statement, derived from the shipped one, that
writes three more rows per Ack. Wall time, reported and not gated: the on/off
ratio of the mean Ack per round was 1.162 (SD 0.546), 1.292 (SD 0.123), 0.998
(SD 0.358) and 1.140 (SD 0.116) in the four runs, at load1 21-51 (runs alone) and 33-43 (suite runs) on 16 CPUs.
No quiet-host wall gate was run (rule PD).

## Not checked

- Quiet-host wall cost of the flag-on Ack (rule PD needs load1 below 8 at the
  start, the end and throughout); the coordinator decides whether to run it.
- A built-binary replay with the flag on (BIN1, #7401).
- Fleet cost on a deployed environment.
- Whether the infrastructure change that enables the flag in dark mode has
  merged; this work changes no environment.

## Commands

```bash
export ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN='postgres://postgres:...@127.0.0.1:<port>/postgres?sslmode=disable'
cd go && go test ./internal/storage/postgres/ -count=1 -v \
  -run 'TestProjectorAckJournal|TestProjectorAckWithJournal|TestAckJournalStatements'
ESHU_POSTGRES_TEST_DSN=... go test ./internal/storage/postgres/freshness/links/ -count=1 \
  -run 'TestAck|TestJournalPassIsExclusive|TestEnabled'
go test ./cmd/ingester/ ./cmd/bootstrap-index/ ./cmd/reducer/ -count=1
```

Performance Evidence: the deterministic cost gate above (statements per Ack
equal at 9 client and 14 `pg_stat_statements`, WAL +4.26 to +4.74 and buffer
hits +11.11 to +12.28 per Ack over four runs against bounds of +6 and +40,
seeded RED caught each time), the
contention run (480 Acks, 16 workers, 24 scopes, 0 deadlocks and 0
commit-order violations), and unchanged flag-off statements. The read path is
unchanged (11-18 s on the 12k-file scope).

Observability Evidence: `eshu_dp_changed_since_ack_journal_total{outcome}`
(`journaled`, `exists`, `gate_skipped`), recorded once per committed flag-on
Ack, and the INFO log "changed-since ack journal skipped while a journal pass
held its key; the sweeper journals the generation" with `scope_id`,
`generation_id` and `outcome`. The reducer's journal pass skip, backlog, lag
and orphan signals (`eshu_dp_changed_since_link_backlog`,
`eshu_dp_changed_since_link_lag_seconds`,
`eshu_dp_changed_since_ledger_orphans`) show whether skipped activations are
journaled later.
