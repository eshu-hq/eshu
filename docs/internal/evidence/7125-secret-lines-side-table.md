# #7125 hardcoded-secret investigation: derive findings at write time

`POST /api/v0/code/security/secrets/investigate` and the MCP tool
`investigate_hardcoded_secrets` took 33 s median (49 s max) unscoped on ops-qa,
and 3.9 s even scoped to the largest repository. This change moves the detection
from every read to the content write: migration 131 adds a
`content_file_secret_lines` side table that Postgres triggers keep current, and
`InvestigateHardcodedSecrets` reads only that table. The design and its
rejected alternatives are in the arbiter ruling for #7125; this page records the
proof for the implementation. A second design step (bulk-load gate, finalizer,
and a readiness-gated read) exists because the write-time derivation measured too
expensive for a bootstrap; the sections "Write-path cost", "Bulk-load gate and
finalizer", and "Merge bar" record why and how it was proven.

## Classification

- Wrong layer before: the read scanned about 92% of `content_files` per call
  (bitmap heap recheck 33.6 s plus a 15.6 s per-file line split on ops-qa,
  145k files). No index can help: the discriminating part of the pattern has no
  trigrams, and the line split remains even with a perfect file prefilter.
- Change layer: schema DDL, statement-level triggers, and one read query.
  Accuracy first: the response must be identical for the same request.

## Metric boundary

- Read: `postgres.query` span, `db.operation=investigate_hardcoded_secrets`,
  and the Postgres `EXPLAIN (ANALYZE)` Execution Time of the same statement.
- Write: the `upsert_files` content-writer stage, that is the batched
  `INSERT INTO content_files ... ON CONFLICT DO UPDATE`, whose Execution Time now
  includes the two statement triggers (EXPLAIN reports each trigger's time).

## Accuracy proof (row equivalence, identical order)

`go/internal/query/content_reader_security_secrets_live_test.go`
(`TestHardcodedSecretSideTableDifferentialLive`), PostgreSQL 18.6, real
`ContentWriter` writes, real bootstrap schema:

- The frozen legacy query is `hardcoded_secret_legacy_golden_test.go`, lifted
  mechanically from the pre-change builder at b7a0d0e367 (it still interpolates
  `hardcodedSecretSQLPattern` and `hardcodedSecretSQLSuppressionPredicate()`).
- 28 argument sets: the ruling's 16 (default, limits 11 and 201, offsets 50 and
  100 and past the end, one and two kinds, include-suppressed, repo filter with
  padding, language, absent language, grant array, repo over grant, repo plus
  kind plus suppressed, limit 5000 with and without suppressed, rare kinds) plus
  the 8 unscoped limit-only sweep sets from the issue (10 to 2000).
- Fixture: all six kinds; an `sk_live` line the pattern matches but the
  classifier drops; a short value that does not match; CRLF; lowercase `akia`;
  placeholder lines; all four suppressed path fragments; NULL and non-NULL
  language; mixed-case and non-ASCII paths (collation order); a 5,000-character
  line; 240 generated files for paging.
- Result, every state IDENTICAL on all 28 sets, with the side table equal to the
  derivation recomputed from `content_files` (EXCEPT ALL both ways = 0 / 0):

| state | legacy rows compared |
| --- | --- |
| initial load | 995 |
| changed upsert | 970 |
| unchanged re-upsert (side rows not rewritten: xmin snapshot equal) | 970 |
| language-only update (to and from NULL) | 978 |
| tombstone delete (writer batch delete, FK cascade) | 802 |
| primary-key move (relative_path and repo_id) | 802 |
| set-based delete (retention-shaped) | 703 |
| re-create after delete | 884 |

- Seeded violations, both make the harness fail and are restored afterwards:
  deleting one side row (parity missing = 1, differential diffs), and disabling
  the UPDATE trigger across a changed upsert.
- Mutation checks run against the migration: dropping the backfill fails the
  migration test; dropping the language comparison from the UPDATE trigger fails
  the language-only step.
- `TestContentFileSecretLinesRetentionPruneCascadesLive` runs the real
  `pruneContentFilesForGenerationsQuery`: 3 files pruned, their side rows gone,
  a file retained by another generation kept, parity 0 / 0. The whole
  `PruneSupersededGenerations` store cannot run on a bootstrapped schema today
  because its row-count query names a missing table (#6809); that is unrelated
  and untouched here.
- `TestContentFileSecretLinesMigrationBackfillsAndReappliesLive`: migration on a
  pre-populated `content_files` backfills exactly the expected rows, and
  applying the file a second time changes nothing.
- Hermetic bindings (`hardcoded_secret_migration_binding_test.go`): the migration
  contains `hardcodedSecretSQLPattern` byte for byte twice, the six CASE arms of
  the legacy classifier in order, and a `suppressed` expression equal to
  `hardcodedSecretSQLSuppressionPredicate()` with the column prefixes stripped;
  five seeded Go-only edits (pattern branch, suppression fragment added or
  removed, classifier arm, dropped file prefilter) each turn the guard red.

## Concurrency proof

`go/internal/storage/postgres/content_file_secret_lines_concurrency_live_test.go`:

- Three upserters over the same 300 keys with different content, one session
  deleting key ranges in lock order, and one running the real retention prune
  for 6 s: 178 to 294 upserts, about 1,000 to 2,000 deletes, and 14,000 to
  33,000 prune statements per run with no error and no deadlock, and parity
  0 / 0 afterwards.
- A held upsert transaction: a concurrent upsert of the same key waits on the
  `content_files` row lock (`pg_stat_activity` shows `wait_event_type = Lock`),
  and after commit the side table holds only the second writer's findings.
- Trigger functions are VOLATILE plpgsql, so each statement takes a fresh READ
  COMMITTED snapshot; there is no writer fence because the triggers also cover
  older binaries.

## Read-path proof

Performance Evidence: local PostgreSQL 18.6, 155,000 `content_files` rows
(about 95 MB) with 19,046 side rows (the ruling's shim had 19,369; ops-qa has
10,161), `EXPLAIN (ANALYZE, BUFFERS)` Execution Time, alternating legacy and new,
box load average 22 to 50 from other agents. The corpus is small next to ops-qa's
1.1 GB, so the legacy figures here understate the ops-qa cost (49.3 s default,
3.9 s repo-scoped, measured by the arbiter, read-only); the new path does not
depend on content size.

| shape | legacy on this fixture | side table median (min to max, n=20) |
| --- | --- | --- |
| default, limit 26 | 429 to 449 ms | 0.18 ms (0.15 to 0.35) |
| repo scoped, repo with 672 findings | 18.5 to 19.5 ms | 0.17 ms (0.16 to 0.24) |
| repo scoped, repo with none | 1.4 to 1.7 ms | 0.10 ms (0.09 to 0.23) |
| rare kind, limit 201 | 380 ms | 1.38 ms (1.19 to 1.52) |
| offset 10000, limit 201 | 423 to 429 ms | 4.92 ms (4.04 to 5.42) |

`TestHardcodedSecretInvestigationGenericPlansUsePrimaryKeyLive` forces
`plan_cache_mode = force_generic_plan` (pgx statement caching can reach generic
plans) on a 20,000-file load and requires, for default, repo, grant, kind,
language, and include-suppressed-with-offset shapes, a `Limit` root over
`Index Scan using content_file_secret_lines_pkey`, with no `content_files`,
`Seq Scan`, full `Sort`, or `Bitmap` node. An Incremental Sort over the
primary-key prefix (the inert `finding_kind` tiebreak) streams and stops at
LIMIT. A hermetic guard also fails if the query text ever names `content_files`
or `regexp_split_to_table`.

## Write-path cost

Performance Evidence: the write-time derivation is a per-file regex over the file's
whole content, and it is not free. Measured on the remote reference host (idle AWS
r7a.4xlarge, 16 vCPU, PostgreSQL 18.6, `postgres:18-alpine`
`sha256:d3e1620b530c944afa6e887d22eb899824da68e19c52024bf98f5220c88a65b2`,
`shared_buffers=2GB work_mem=64MB`) with `TestContentFileSecretLinesWriteCostLive`
(records-only `ContentWriter.Write`, 12,000 real files, 69 MB, 5 interleaved rounds,
alternating first mover, commit `8984ba1db8` against a pre-131 schema):

| regime | phase | no triggers (median) | triggers (median) | delta | per file |
| --- | --- | --- | --- | --- | --- |
| deferred indexes (bootstrap) | insert | 12.504 s | 19.764 s | +7.260 s (+58.1 %) | +0.605 ms |
| deferred indexes (bootstrap) | changed upsert | 12.972 s | 27.487 s | +14.515 s (+111.9 %) | +1.210 ms |
| deferred indexes (bootstrap) | unchanged re-upsert | 12.831 s | 13.957 s | +1.127 s (+8.8 %) | +0.094 ms |
| trigram GIN present | insert | 22.623 s | 29.979 s | +7.356 s (+32.5 %) | +0.613 ms |
| trigram GIN present | changed upsert | 24.238 s | 35.938 s | +11.700 s (+48.3 %) | +0.975 ms |
| trigram GIN present | unchanged re-upsert | 24.912 s | 25.731 s | +0.819 s (+3.3 %) | +0.068 ms |

The relative stop threshold (+10 % on the bootstrap content-write stage) fails on
insert alone in the bootstrap regime, and the per-file cost is the same in both
regimes (0.61 ms), so it is trigger work and not index interaction. The earlier
statement-level ratios (0.18 to 0.25 ms per file) missed the transition-table
materialization and the update path; do not quote them. The local wall-clock
medians of the same test on a shared host are not evidence (9.5 to 30 s spread on
one phase) and are not quoted.

### Where the changed-path cost goes (theory tested, disproved)

The arbiter's first theory for the changed path was that the old-versus-new
`content IS NOT DISTINCT FROM` comparison and the DELETE dominate, and that fencing
on `content_files.content_hash` would remove most of it. It was tested with a shim
before any code (PostgreSQL 18.6, the same 12,000 real files, 66 MB, copied into
scratch tables, `EXPLAIN (ANALYZE, TIMING OFF)`, three repetitions each):

| step over 12,000 files | time |
| --- | --- |
| derive only (file regex, line split, classify) | 2,388 to 2,763 ms |
| `content IS NOT DISTINCT FROM` compare, all rows changed | 11 to 13 ms |
| `content IS NOT DISTINCT FROM` compare, all rows unchanged | 131 to 161 ms |
| `content_hash` compare, all rows changed | 7 to 9 ms |
| `content_hash` compare, all rows unchanged | 6 to 9 ms |

The compare is 0.01 ms per file against 0.2 ms for the derivation, so a hash
fence removes under 5 % of the changed-path trigger cost. It would also trade the
byte comparison for a hash equality the schema does not tie to the content
(`content_hash` is an ordinary `NOT NULL` column any writer may set), so a row
whose content changed under an unchanged hash would keep stale findings. The fence
was not built. Auto-explain of a 500-file changed batch on the same data shows the
same split: DELETE 2 ms, changed-set plus derive 323 ms, remainder of the upsert
statement 255 ms. The changed path costs about one derivation per changed file
plus the transition-table copy of the old and new tuples, and both are inherent to
deriving at write time. The steady-state cost therefore stays at about 0.28 ms per
new or changed file on this hardware (backend CPU, below) and 0.6 to 1.0 ms on the
reference host, and is paid once per changed file by the ingester, not on the
bootstrap critical path.

### Bootstrap: the gate removes the cost

Bootstrap-index does not pay it. Its sessions skip the triggers and a finalizer
rebuilds the table once (next section). What the gate leaves behind is bounded, not
resolved: a residual upper bound of 0.085 ms per file in the noisiest regime, not
resolved on a shared host; the reference-profile merge bar measures it.
Backend CPU seconds (read from `/proc/<pid>/stat`, so wall-clock noise on an
oversubscribed host does not enter) over the same 12,000 files on PostgreSQL 18.6,
local, three regimes interleaved with rotating first mover, four rounds, medians. This
first run measured the insert phase only, and its index regime was not recorded, so
read it as a local-host figure and not as one of the two regimes named in the second
table:

| phase | no triggers | triggers on, gate skips (deferred session) | triggers on, deriving |
| --- | --- | --- | --- |
| insert | 0.240 s | 0.225 s | 3.595 s |

Its no-trigger insert is 0.02 ms per file (0.240 s over 12,000), against 0.17 ms per
file (1.020 s over 6,000) for the same operation in the second table below, an
eightfold difference this note does not explain: the runs differ in file count, index
regime, and host load (the second run's load average was 35 to 47), and single samples
on this host swing up to 3x. The two tables agree on the direction only (deriving costs
0.27 to 0.31 ms per file; a deferred session costs less), so no figure from the first
table is used as a gate-cost bound.

The same harness with a third database (triggers on, writes from sessions carrying the
deferred setting), 6,000 real files, five rounds with rotating first mover, backend CPU medians
(local PostgreSQL 18.6, host load average 35 to 47, so single samples swing up to 3x and only the
medians and the direction are evidence; the wall-clock rows in the same run were unusable):

| regime | phase | no triggers | triggers, deriving | gate skips (deferred session) |
| --- | --- | --- | --- | --- |
| deferred indexes (bootstrap) | insert | 1.020 s | 2.660 s (+0.273 ms/file) | 1.530 s (+0.085 ms/file) |
| deferred indexes (bootstrap) | changed upsert | 2.060 s | 5.580 s (+0.587 ms/file) | 2.150 s (+0.015 ms/file) |
| deferred indexes (bootstrap) | unchanged re-upsert | 1.440 s | 1.950 s (+0.085 ms/file) | 1.180 s (-0.043 ms/file) |
| trigram GIN present | insert | 6.150 s | 8.040 s (+0.315 ms/file) | 6.070 s (-0.013 ms/file) |
| trigram GIN present | changed upsert | 7.980 s | 9.300 s (+0.220 ms/file) | 8.040 s (+0.010 ms/file) |
| trigram GIN present | unchanged re-upsert | 7.370 s | 8.960 s (+0.265 ms/file) | 6.860 s (-0.085 ms/file) |

The gate removes the derivation cost: the gated path is 0 to 0.085 ms per inserted file above the
no-trigger baseline (the transition-tuple copy that runs before the `WHEN`; within noise in three of
six rows) against 0.27 to 0.31 ms per file for the derivation on this hardware, and about 0.6 ms on the
reference host. The residual upper bound is 0.085 ms per file in the noisiest regime, not resolved on a
shared host; the reference-profile merge bar measures it. If that bound were real it would be about 12 s
of Postgres CPU at 145,000 files, so it is not dismissed. The steady-state ingester (no gate) still pays
the derivation: 0.22 to 0.59 ms per changed file in these local runs, and 0.6 to 1.0 ms per file on the
reference host (0.605 ms per new file, 0.975 to 1.210 ms per changed file in the first table of this
section), against 0.28 ms per new or changed file in the local backend-CPU figure.


## Bulk-load gate and finalizer

Design (arbiter ruling for #7125, write-path follow-up). Bootstrap-index opens every
connection with `SET eshu.secret_lines_derive = 'deferred'`
(`DeferredSessionSQL` in `storage/postgres/secret/lines`); migration 131's two `content_files` triggers
carry `WHEN (current_setting('eshu.secret_lines_derive', true) IS DISTINCT FROM
'deferred')` and skip such a session. This is the migration 109 precedent
(`eshu.infra_inventory_writer`, set through the same pgx `AfterConnect` hook):
a per-session gate, not `ALTER TABLE ... DISABLE TRIGGER`, which takes a table lock and
silences every writer at once. A pooler affects the setting in two directions. One that
drops or never forwards it leaves that session deriving: correct, only slower. A
transaction-mode pooler does not reset session settings between clients, so the `SET` can
stay on a server connection later handed to another binary, whose writes then skip
derivation while the state is `ready`: findings are silently missing. bootstrap-index must
not run through a transaction-mode pooler (Eshu's deployment has none). The same leak class
is recorded for `eshu.infra_inventory_writer` in the connection-pooler paragraph of
`6793-infra-read-model-fence.md`, after its lock-hold discussion.

State machine (the `content_substring_index_state` precedent, plus an epoch):
`content_file_secret_lines_state` is `ready` after migration 131 (its backfill made the
table complete) and on a fresh install. `BeginDeferral` runs right after the schema
applies and before any write: one autocommit statement, state `not_built`, `epoch + 1`.
After the pipeline drains, `Finalize(epoch)` claims `building`, rebuilds the table, and
publishes `ready WHERE epoch = $1 AND state = 'building'`, so a finalizer from an
earlier load can never publish over a newer load's skipped writes. A failed run leaves
`failed`. Readers use the side table only in `ready`.

Finalizer: repositories are the partition (four workers, 500-file batches). One batch is
one transaction: `SET LOCAL lock_timeout = 250ms`; a non-locking `SELECT relative_path ...
ORDER BY relative_path LIMIT n` scan of the next key window (keyset cursor = the window's last
key); `SELECT relative_path ... WHERE relative_path = ANY(window) ORDER BY relative_path FOR
SHARE` on exactly those keys; `DELETE` the side rows of exactly the locked files; `INSERT` the derivation of the locked rows; commit. The window scan is one primary-key index-only range read: on a scratch 50,000-file table
(PostgreSQL 18, local) `EXPLAIN (ANALYZE, BUFFERS)` of a 500-key window read 10 shared buffers
in 0.099 ms, against 100 to 800 ms for the batch's derivation, so the extra statement per batch
is noise on the finalizer and takes no lock. Every batch replaces its files'
rows from the locked content, so a rerun converges (idempotent, restart-safe), stale rows
from a rewrite under the gate are removed, and files deleted meanwhile lose their rows to
the foreign-key cascade. It runs concurrently with the content substring index build
(a row lock does not conflict with `CREATE INDEX`'s table `SHARE` lock).

Concurrency analysis. Conflict domain: the `content_files` row of one file, then that
file's side rows. Steady-state writers (triggers on) update a row and, in the same
statement's trigger, delete and insert its side rows. The finalizer's `FOR SHARE`
conflicts with a writer's row update, so a batch either sees a writer's committed
content (READ COMMITTED recheck; the writer's trigger already wrote the matching rows)
or waits for it. A wait cycle is possible only if a writer holds a row the batch needs
while wanting one the batch locked (writers do not sort their batches); the batch's
`lock_timeout` (250 ms, below `deadlock_timeout`) breaks it on the finalizer's side: it
rolls back with nothing applied and retries with backoff, up to 40 attempts, then fails
loudly. `lock_timeout` bounds only the finalizer's own waits, which is what guarantees the
finalizer, not a writer, yields; the finalizer never forms a wait cycle. A writer blocked on
a row a batch holds `FOR SHARE` still waits until that batch commits: the lock, the delete,
and the derivation of up to `BatchSize` files. That is hundreds of milliseconds at 500 files
(500-file derivations measured 100 to 160 ms warm and 382 to 840 ms cold in the arbiter's
probe) and tunable by `BatchSize`. No update is lost or applied twice.

Proof, PostgreSQL 18.6 (`postgres:18-alpine`), all in `go/internal/storage/postgres/secret/lines`
and `go/internal/query`:

| claim | test | RED (seeded violation) | GREEN |
| --- | --- | --- | --- |
| a deferred session derives nothing (insert and update path), an ordinary session does | `TestDeferredSessionWritesDeriveNothingLive` | `WHEN` removed from the insert trigger: `deferred insert produced 40 side rows, want 0` | pass |
| triggers and Go constants agree | `TestMigrationTriggersSkipExactlyTheDeferredSession` | hermetic binding | pass |
| `BeginDeferral` turns readiness off durably and bumps the epoch | `TestBeginDeferralTakesReadinessAndBumpsEpochLive` | | pass |
| finalizer output equals the derivation (EXCEPT ALL 0 / 0), including stale rows left by a deferred rewrite and keyset paging (size 7, three workers) | `TestFinalizeRebuildsSideTableToParityLive` | setup asserts missing and extra both non-zero before; `DELETE` made a no-op: `duplicate key value violates unique constraint` | pass |
| idempotent and restart-safe: crash after three batches leaves `failed` and readers off the table; rerun of the same epoch reaches parity and ready; a finished epoch is a no-op | `TestFinalizeIsRestartSafeLive` | | pass |
| epoch fence: a bulk load that begins mid-finalize is never overwritten by ready | `TestFinalizeIsSupersededByNewBulkLoadLive` | | pass |
| a held live writer: the finalizer waits, times out, retries; the writer's reverse-order update of an earlier row completes within lock_timeout with no error; after commit, parity 0 / 0 with the writer's findings | `TestFinalizeYieldsToAHeldWriterLive` | `FOR SHARE` removed: the finalizer finishes past a held row (`Finalize completed while a writer held a row it needs`) | pass |
| a key move that commits while the finalizer waits on the batch's last row cannot make it skip files: a locking `ORDER BY ... LIMIT ... FOR SHARE` returns the moved row's new key at its old position (probe: `0001,0002,9999,0004,0005`), so a cursor from the last returned key jumps past every unmoved file in between | `TestFinalizeSurvivesAConcurrentKeyMoveLive` | cursor from the locking read's last key: `side table vs derivation EXCEPT ALL = missing 41, extra 0` | pass; the cursor is now the last key of a non-locking scan of the window |
| three steady-state upserters race a four-file-batch finalizer: no error, no deadlock, parity 0 / 0 | `TestFinalizeWithSteadyStateWritersLive` | `FOR SHARE` removed: writers fail with `duplicate key value violates unique constraint "content_file_secret_lines_pkey"` | pass (18 upserts, 90 batches in one run) |
| bootstrap wiring: deferral begins after the schema and before graph, collector, and projector; finalize runs after the pipeline with the same epoch; a finalizer failure fails the run while the index finalizer still completes | `TestRunBeginsSecretLinesDeferralBeforeWritesAndFinalizesAfterThePipeline`, `TestRunReportsSecretLinesFinalizationFailure` | | pass |
| bootstrap pool connections carry the setting | `TestOpenBootstrapDBMarksEveryConnectionDeferredLive` | | pass |
| migration re-apply does not reset a bulk load's state | `TestContentFileSecretLinesMigrationBackfillsAndReappliesLive` | | pass |

## Readiness-gated read

`InvestigateHardcodedSecretsWithSource` reads `content_file_secret_lines_state` (one
primary-key row) and serves from the side table only when `state = 'ready'`. Otherwise
it runs the pre-change corpus scan (`hardcodedSecretLegacyScanQuery` in `content_reader_security_secrets.go`), and the
answer says so: `coverage.read_path = "legacy_scan"`, `coverage.limitations`, the truth
`reason`, the span attribute `eshu.hardcoded_secret.read_source`, and
`eshu_dp_hardcoded_secret_reads_total{source}`. It is never a silent partial answer.

| claim | test | RED (seeded violation) | GREEN |
| --- | --- | --- | --- |
| before ready the read is the legacy scan and identical to the frozen legacy query on all 28 argument sets; after the finalizer, identical from the side table (995 rows, parity 0 / 0); a new bulk load takes the side table away at once; the counter records both sources | `TestHardcodedSecretReadFallsBackUntilReadyLive` | readiness ignored: `read before ready = 0 rows, source "side_table"` | pass |
| the fallback SQL is the frozen pre-change SQL for every request shape | `TestHardcodedSecretLegacyScanQueryMatchesFrozenGolden` | hermetic guard | pass |
| the handler labels a legacy read and does not label a side-table read | `TestHardcodedSecretLegacyScanReadIsLabelledLimitedNotSilent` | annotation removed: `coverage.read_path = "side_table", want legacy_scan` | pass |
| the unit-level gate | `TestContentReaderInvestigateHardcodedSecretsServesLegacyScanUntilReady` | readiness ignored: `query missing ordered fragment "WITH candidate_files AS"` | pass |
| ready-state row differential unchanged | `TestHardcodedSecretSideTableDifferentialLive` (28 argument sets, eight write-path states) | | pass, identical rows in every state |

## Concurrent bulk loads (P2-D1)

The epoch fence protects only the older-finalizer case. Replayed against the real API
(`TestBulkLoadLockKeepsReadyHonestLive/without_lock`): load B runs `BeginDeferral` (epoch e) and writes
repo-b through a deferred pool; load A runs `BeginDeferral` (epoch e+1) and writes repo-a; A's
`Finalize(e+1)` claims and publishes `ready`; B then writes one more file with a finding through its
deferred pool. State is `ready`, the side table is missing B's late findings (parity query non-zero), and
B's `Finalize(e)` claims nothing. Readers trust `ready`, so the security endpoint answers wrongly until a
later bootstrap.

Decision (arbiter ruling for #7125, P2-D1): forbid the overlap instead of tolerating it. bootstrap-index
holds a session advisory lock, key `(5318,1)`, on one pinned pool connection from right after it opens the
database until after both finalizers, on every path (`secretlines.AcquireBulkLoadLock`,
`bulk_load_lock.go`). A second run polls `pg_try_advisory_lock` for `ESHU_SCHEMA_BOOTSTRAP_OWNERSHIP_WAIT`
(default 3 m; the schema-bootstrap wait that follows has the same bound, so the worst case is twice it),
logs `bootstrap.postgres.ownership.waiting` with `lock=bulk_load` and the holder (pid,
application name, connection age), then fails. The lock dies with its backend, so a killed pod frees it at
once; after a node death without a TCP close it lingers until keepalive, which is why the second run waits
with a bound instead of failing instantly. A deferral counter was rejected: a crashed run never decrements
it, and it would legalize concurrent bulk loads in one subsystem while graph writes, generation markers and
the substring finalizer still assume one run.

Lock order is fixed, never reversed: run lock (5318,1), then the schema lock (5318,0) inside
`withSchemaBootstrapLock`, released before `BeginDeferral`; the substring finalizer's transaction-scoped
advisory lock is taken later, inside the finalize phase. `db-migrate` takes only (5318,0) and never waits on
the run lock. Both waiters poll a try-lock while holding nothing, so no waiter is ever in the Postgres
wait-for graph: no cycle is possible. The pinned connection is one slot of the pool for the whole run, so
`commitLaneReserve` grows by one (projection workers + 2, minimum 3); `database/sql` never closes an in-use
`*sql.Conn` for `ConnMaxLifetime`, so the session survives the run.

Proof:

| claim | test | seeded violation | GREEN |
| --- | --- | --- | --- |
| the run lock is exclusive across sessions, the refusal names the holder's pid and the subject, logs `lock=bulk_load`, and the next acquirer gets it at its first try | `TestBulkLoadLockRefusesASecondLoadLive` | `TryLock` always true: `a second AcquireBulkLoadLock succeeded while the first was held` | pass |
| a killed holder does not strand the lock; its `Release` reports the void exclusivity | `TestBulkLoadLockDiesWithItsSessionLive` | | pass |
| without the lock two overlapping loads publish ready over an underived load (the probe can differ); with it the second load is refused until the first finalized and parity is 0 after each | `TestBulkLoadLockKeepsReadyHonestLive` (`without_lock`, `with_lock`) | `TryLock` always true: `load A acquired the lock while load B was between BeginDeferral and Finalize` | pass |
| lock -> schema -> begin -> pipeline -> finalize -> release; the wait is the schema ownership wait | `TestRunHoldsTheBulkLoadLockFromBeforeTheSchemaUntilAfterFinalize` | lock not wired into `run`: `event "lock" never ran` | pass |
| the lock is released, before the database closes, on a schema failure, a failure after `BeginDeferral`, and a finalizer failure | `TestRunReleasesTheBulkLoadLockOnEveryFailurePath` | lock not wired into `run`: lock and release missing from every path | pass |
| a refused lock runs nothing (no schema, no deferral); a release failure fails the run and joins the other errors | `TestRunRefusedBulkLoadLockRunsNothing`, `TestRunJoinsAReleaseFailureIntoTheRunError` | | pass |
| pool budget holds the pinned connection | `TestCommitLaneReserveHoldsTheBulkLoadLockConnection`, `TestEffectiveCommitLanes` | reserve at workers+1: `commitLaneReserve(0) = 2, want 3` | pass |
| the wait names its subject and lock, and the schema wait text is unchanged | `TestWaitForOwnershipNamesItsSubjectAndLock` | `Subject` and `Lock` fields absent: build fails | pass |

Performance: No-Regression Evidence: the lock adds one pinned connection and three round trips per run
(`pg_backend_pid`, `pg_try_advisory_lock`, `pg_advisory_unlock`); nothing on the content-write or finalize
paths changes, and the unchanged `TestFinalize*Live` timings are the no-regression check. The reference-profile
A/B of the merge bar runs on the branch with the lock, so it measures the shipped shape.

Residual risk: if the pinned lock session dies mid-run (network partition) the lock is gone while the run
continues, and a second run started in that window could reproduce the overlap. It needs two failures at
once and is detected: `Release` returns an error, the run exits non-zero with
`secret_lines.bulk_load_lock_released` absent and `failure_class=secret_lines_bulk_load_lock_release_failure`
set, and a rerun rebuilds the table.

## Merge bar (on hold)

The bar is the bootstrap content-write stage and end-to-end margin on the reference
profile, not a local figure. The initial full-984 Step 2 runs are not accepted
merge proof: fingerprint reaping had variable slow plans, and the old terminal
detector checked fact work without checking unfinished shared intents. Keep this
PR draft until the reaper-plan prerequisite is proven and a matched full-984
baseline/candidate comparison reaches the corrected terminal predicate. Run it
from the reviewed branch by git clone/fetch (never rsync), per the
`eshu-remote-validation` skill:

1. Record the preflight: candidate commit, merge-base `origin/main` commit, image IDs,
   topology profile `accepted_remote_GOMAXPROCS16_parse16_snapshot16_projection8_reducer16_shared4_partitions8_codecall4_pg96_graph_inflight8_timeout120s_entity_phase16`,
   corpus `full-984`, clean volumes, `SELECT count(*) FROM content_files`.
2. Baseline = merge-base commit, candidate = branch tip, same host and same
   `neo4j:2026-community` and Postgres images, interleaved when more than one round. State a time bound
   that includes the observed shared-intent tail and stable terminal polls;
   a run that reaches its deadline without terminal truth is incomplete.
3. Metrics, same start and terminal events as the accepted manifest: sum of
   `duration_seconds` over the `content_write` runtime-stage log lines (stop if candidate
   minus baseline exceeds +10 % or +60 s); the `upsert_files` stage sum for attribution;
   `milestones_seconds.bootstrap_exit` and `phase_durations_seconds.projection` against the
   named accepted baseline with matching corpus, profile, topology, storage state,
   and metric boundaries; the new
   `eshu_dp_bootstrap_pipeline_phase_seconds{bootstrap_phase="secret_lines_finalization"}`
   and content index finalization phases (the finalizer runs beside the index build, so
   `bootstrap_exit` should not grow by its duration); Postgres peak CPU from the sampler.
4. Terminal truth: all 984 repositories ingested; bootstrap exited 0;
   fact work and required shared intents both reached zero open/failed/dead-letter
   rows and remained stable for the terminal polls;
   `content_file_secret_lines_state.state = 'ready'`; parity 0 / 0 and identical
   findings on the candidate database; and default and largest-repository HTTP
   and MCP investigation p95 below 1 s against legacy baseline responses.

## Migration lock window

Migration 131 takes `SHARE ROW EXCLUSIVE` on `content_files` from the foreign key
until its transaction commits, so the backfill blocks content writes (not reads).
About 0.27 ms per file, 38 s at 145k files on ops-qa, linear. A fresh install
backfills nothing. This is the upgrade path only: a bulk load never runs the
migration's backfill on a populated table, it uses the finalizer (batched,
repository-partitioned, row locks only, no table lock). The migration's own single-transaction
backfill on an already-populated table is unchanged: above roughly 500k files it approaches
the 3 minute ownership wait, and a batched variant of it is not built because it is not
needed at ops-qa scale. Operator note: `docs/public/deployment/service-runtimes-bootstrap.md`. The migration
is one implicit transaction, so a failure rolls back completely and the retry is safe.

## Observability

Observability Evidence: the read keeps the `postgres.query` span and its
`db.operation=investigate_hardcoded_secrets` attribute; `db.sql.table` is
`content_file_secret_lines` or `content_files` (legacy scan), the new
`eshu.hardcoded_secret.read_source` attribute says which, and the row count is
`db.rows.hardcoded_secret_findings`. New metrics:
`eshu_dp_hardcoded_secret_reads_total{source}` (an operator alerts on a sustained
`legacy_scan` rate), `eshu_dp_secret_lines_backfill_batches_total{outcome}` (committed
versus retried: contention with live writers; zero committed while `building` means
stuck), `eshu_dp_secret_lines_backfill_files_total` (progress against the corpus size), and
the existing `eshu_dp_bootstrap_pipeline_phase_seconds` with the new
`bootstrap_phase="secret_lines_finalization"`. Logs: `secret_lines.finalize_started`,
`_progress` (every 10 s), `_complete`, `_failed` (with `failure_class=secret_lines_backfill_failure`);
the run lock adds `secret_lines.bulk_load_lock_acquired`, `_refused`, `_released` and
`_release_failed` (with `failure_class=secret_lines_bulk_load_lock_refused` /
`secret_lines_bulk_load_lock_release_failure`) and `bootstrap.postgres.ownership.waiting` with
`lock=bulk_load`.
The durable readiness row `content_file_secret_lines_state` (state, epoch, timestamps) is the
status surface: `SELECT state, epoch FROM content_file_secret_lines_state`. The trigger cost,
where it is still paid (steady-state ingester writes), lands in the existing content-writer
`upsert_files` stage log and in `eshu_dp_postgres_query_duration_seconds`.

## Side finding

`content_files_repo_path_idx` (21 MB on ops-qa) duplicates `content_files_pkey`
(arbiter note; not touched here).
