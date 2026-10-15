# Shared-Snapshot Export Qualification

Issue #7957. PostgreSQL 18.6 and earlier carry two defects on the
shared-snapshot path the code-topic reader uses: a snapshot exported during
recovery could omit subtransaction XIDs (`ef89c839cdc8`, backpatched to
`REL_18_STABLE` as `74c9832ca610`), and an importing session's xmin could race
VACUUM (`ca99016f939d`, backpatched as `3977cde272bc`). The guarded reader now
refuses `BeginReadOnlySnapshotSet` with `db.ErrSnapshotExportUnqualified`
unless the reader build is PostgreSQL 18 at or above 18.7 or an exact
`server_version` declared in `ESHU_POSTGRES_READ_SNAPSHOT_EXPORT_BUILDS`. The
query layer answers the refusal as a permanent `503` with
`details.reason=snapshot_export_unqualified`. There is no fallback.

## Conflict domain and concurrency

- Shared state: one `snapshotExportQualification` per single reader pool and
  per fleet member. It holds an `atomic.Pointer` to an immutable verdict.
  Dial validators and every borrow's `checkReader` publish to it, and only when
  the build changed. The snapshot-set fast path and the pool-metrics scrape
  read it with one atomic load. No lock is taken and no lock order changes.
- The refusal happens before the per-Access snapshot gate and before any
  allocator reservation, so a refused set holds no gate token, permit, or
  member slot. Each borrowed set connection is also checked against its own
  identity before `BEGIN`. A stale "qualified" verdict therefore cannot export
  on an unqualified build. On the fleet site that member is excluded for the
  call and a qualified sibling serves the set. The exclusion is kept out of the
  setup-failure join, so a later sibling admission wait keeps its capacity
  classification
  (`TestFleetPerBorrowExclusionKeepsSiblingCapacityClassification`).
- Race proof: `go test -race` over the package. It includes
  `TestFleetConcurrentSetsReadsAndVerdictFlipsAreRaceFree`, which runs sets,
  plain reads, and forced verdict flips concurrently and asserts the undeclared
  member never exports and no reservation leaks. It also includes
  `TestSnapshotExportQualificationHolderFlipsAtomically` and
  `TestSnapshotSetDenialAndCancellationLeaveNoReservation`, which check gate,
  permit, allocator, and pool state after a refusal, a cancel, and a served
  set on both export sites. The new tests also passed `-race -count=20`.

## No-Regression Evidence:

Statement counts on the touched path are unchanged. `server_version` and
`server_version_num` ride the existing identity statements:
`physicalMetadataSQL` for dials and bootstrap, and the two `checkReader`
statements for borrows. A borrow still issues one identity statement plus the
replica replay probe. A dial still issues the same metadata statements.
`TestFleetReaderStagesAreChildrenOfTheRequestSpan` still pins one
`reader_identity` observation per borrow. A set refused on the pre-borrow
fast path (the pool or member verdict already unqualified) issues zero
statements, where it used to issue 4 identity checks, 4 replay probes,
4 `BEGIN`s, 1 `pg_export_snapshot()`, and 3 `SET TRANSACTION SNAPSHOT`s. A set
refused by the per-connection recheck (a stale qualified verdict) has already
borrowed and fenced the connection whose identity check revealed the
unqualified build; it releases that connection and any earlier ones before a
`BEGIN`, so it issues their identity and replay statements but no `BEGIN`, no
export, no import, and no business SQL.

Allocations. Interleaved A/B of `go test -c` binaries: base `261ca79b8` and
head `e0193346f`, five alternating `-test.count 1` runs each, on a shared
16-core arm64 host at load average 55 to 68 on 2026-10-15 (run
`ab-bench`, scratch output not committed):

| Benchmark | allocs/op base -> head | B/op base -> head | ns/op median base -> head |
| --- | --- | --- | --- |
| `ReaderQueryObserve/nil_observer` | 60 -> 55 | 2858 -> 2987 | 9754 -> 10558 |
| `ReaderQueryObserve/legacy_observer` | 60 -> 55 | 2861 -> 2987 | 11129 -> 10843 |
| `ReaderQueryObserve/otel_request_span` | 135 -> 130 | 12918 -> 13046 | 34929 -> 36917 |
| `ReaderQueryObserveAccumulator/nil_observer` | 62 -> 57 | 2976 -> 3103 | 10423 -> 11290 |
| `FleetReservationClassification/available` | 11 -> 11 | 776 -> 776 | 2118 -> 2013 |
| `FleetReservationClassification/contended` | 16 -> 16 | 776 -> 776 | 1133558 -> 1136417 |

The ns/op medians are noise-bound on this host. Per-run ranges overlap fully,
for example `nil_observer` base 5702 to 14195 and head 5917 to 15124. So the
timing column is not a speedup or slowdown claim. The allocation counts are
deterministic. The review base `7be0f4ce7` has an empty diff from `261ca79b8`
for `go/internal/runtime/postgres` and `go/internal/storage/postgres/db`
(`git diff 261ca79b8 7be0f4ce7 -- <both dirs>` prints nothing), so these
numbers carry over to the rebased branch. Two regressions found during the run were fixed before this
measurement. First, adding the eligibility check made `memberOrder`
non-inlinable, which moved its order slice to the heap on every fleet borrow
(+1 alloc, +8 B). The set-only filter now lives in `runFleet`. Second, the two
extra scan destinations escaped separately (+2 allocs), and scanning the
version into `int` went through `strconv`. The identity row now scans into one
value with an `int64` build number. The remaining ~128 B/op comes from the two
extra columns the benchmark's fake driver builds.

`BenchmarkFleetSnapshotSetup` and `BenchmarkFleetStalledFirstMember` need an
owned primary and two physical standbys. They skipped here, so they are
NOT_CHECKED.

## Observability Evidence:

- `eshu_dp_postgres_reader_snapshot_export_qualified` (0/1, unlabeled for a
  single pool, `member_ordinal` per fleet member) is read from the atomic
  verdicts in the existing pool-metrics callback, with no SQL at scrape
  (`TestSnapshotExportQualifiedGaugeFollowsAtomicVerdicts`).
- `eshu_dp_postgres_reader_snapshot_export_denials_total{reason}`, where reason
  is one of `no_declaration` or `undeclared_build` and anything else becomes
  `unknown` (`TestSnapshotExportDenialCounterHasOnlyClosedReason`).
- The `postgres.reader.snapshot_export_qualification` log fires once per pool
  or member at Open (INFO or WARN). The `postgres.reader.snapshot_export_denied`
  log is a WARN per refused request. The
  `postgres.reader.snapshot_export_declaration` WARN covers a bare-label
  declaration.
- The `postgres.query` span records `code_topic.execution_mode=denied` and
  `code_topic.denial_reason=snapshot_export_unqualified` with Error status. The
  request span is marked Error through `WriteGraphReadError`.

## Live proof

The fixture is a disposable `postgres:18` primary and a `pg_basebackup`
streaming physical standby on host ports 28110 and 28111. Both report
`server_version` `18.6 (Debian 18.6-1.pgdg13+2)`, which is 180006. That is an
unpatched 18.6, equivalent to the CI `postgres:18.6-bookworm` mirror. The
standby ran `log_statement=all`.

- `TestSnapshotExportRefusedOnUndeclaredStandby` passed for the single pool
  and the fleet. The standby log for that window holds no `BEGIN`, no
  `pg_export_snapshot`, and no `SET TRANSACTION SNAPSHOT`.
- `TestSnapshotExportDeclaredStandbySharesOneSnapshot` passed for the single
  pool and the fleet. The log shows one export and three imports of the same
  snapshot ID per set.
- The existing snapshot-set suites (`TestReadSnapshotSet*`) passed with the
  fixture build declared.
- `TestCodeTopicSnapshotExportQualificationEndpointPostgresLive` passed. The
  undeclared request was refused on the pre-borrow path: a 503 with zero
  borrows, and no business, export, or
  import statement reached the standby. The declared request got a 200 on
  `parallel_shared_snapshot`.
- `TestCodeTopicFleetCapacityEndpointPostgresLive` passed with the fixture
  build declared.
- `TestInvestigateCodeTopicGuardedParallelPostgresLive` failed all five
  subtests at head `b76337a28` ("PostgreSQL reader snapshot failed to begin")
  and passed all five once its Access declared the fixture build.
- The opt-in `TestSnapshotExportConsistencySmoke` skipped undeclared. It
  passed when the unpatched build was declared, which proves only its
  mechanics. Its build-safety purpose needs a qualified image (#260).

Base-versus-head live count. `go test ./internal/runtime/postgres
./internal/query -count=1 -p 1 -v` ran against the same fixture with both the
`ESHU_READER_TEST_*` and the `ESHU_TEST_CONTENT_INDEX_POSTGRES_*` variables
set, on base `7be0f4ce7` and on the branch after the guarded-proof fix below.
Base: 3003 pass, 94 skip, 6 fail. Head: 3026 pass, 95 skip, 6 fail. The six
failures are identical on both: `TestAccessStreamingRoleAndReplayFailClosed`,
`TestAccessReplayDeadlineBeforeBusinessSQL`,
`TestReadStoreStreamingSnapshotAndCancellation`,
`TestReaderMembersRejectPrimaryAsStandby`,
`TestReaderOpenKeepsHealthyMemberWhenFirstMemberBlackholes`, and
`TestAccessWrongDatabaseFailsBeforePoolExposure`. They need fixture
preparation this harness does not do and are unrelated to this change.

An earlier version of this note listed only those six because it ran the query
live tests one at a time. That missed
`TestInvestigateCodeTopicGuardedParallelPostgresLive`, which failed at head:
all five subtests were refused, including the reader-pressure fallback,
because the refusal is checked before capacity. It now declares the fixture
build. Its ledger class is `scheduled`, so CI would not have caught it.

## Live-suite declaration sweep

Every test that builds a `runtime/postgres` Config or Access was checked, by
`rg 'runtimepostgres\.(LoadConfig|Open)|LoadConfig\(|Open\(ctx|Open\(context'`
over `*_test.go` in `go/internal`, `go/cmd`, and `scripts`. Each one either
declares the fixture build or never reaches `BeginReadOnlySnapshotSet`.

| Test site | Reaches a snapshot set | Disposition |
| --- | --- | --- |
| `runtime/postgres/access_test.go` `testAccess` | yes (`read_snapshot_set_test.go`, status and restart suites) | declares |
| `runtime/postgres/access_test.go` other `Open` calls (wrong topology, budgets) | no | n/a |
| `runtime/postgres/reader_members_test.go`, 3 `Open` calls including `openFleetAccess`, which `reader_begin_timeout_test.go` uses | yes | declares |
| `runtime/postgres/singleton_test.go` `access`, `legacy`, `fleet` | yes | declares; `fleet` reuses the declared Config |
| `runtime/postgres/singleton_test.go` `wrong` | no; `Open` must fail | n/a |
| `runtime/postgres/reader_fleet_bench_test.go` | yes | declares |
| `runtime/postgres/snapshot_export_standby_test.go` | yes | deliberate undeclared and declared halves |
| `runtime/postgres/snapshot_export_smoke_test.go` | yes | declaration from the environment; skips when unqualified |
| `runtime/postgres/legacy_writer_budget_test.go`, `read_store_test.go`, `reader_qualification_test.go`, `topology_test.go`, `writer_candidates_test.go` | no | n/a |
| `query/code_topic_fleet_endpoint_live_test.go` | yes | declares |
| `query/code_topic_parallel_member_loss_test.go` | yes | declares |
| `query/code_topic_parallel_guarded_live_test.go` | yes | declares (fixed in this revision) |
| `query/code_topic_snapshot_export_endpoint_live_test.go` | yes | deliberate undeclared and declared halves |
| `scopedtoken/identity_postgres_restart_test.go` | no; writer only | n/a |

No test under `go/cmd` or `scripts` opens a `runtime/postgres` Access.
