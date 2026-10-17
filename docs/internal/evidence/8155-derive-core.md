# Config-artifact derive core (#8155, PR3b-2b)

This note records the design and the evidence of the derive core: the
library function that derives one repository's config-artifact rows and
writes them under the protocol of
[the protocol companion](../design/7126-ingest-time-config-artifacts-protocol.md),
section 3. It has no caller yet. The call from `ContentWriter.Write`, the
walker derive, the counters, the span and the environment variable land in a
later PR3b-2 slice.

## Sequence

A Write derive, a content Write of the same repository and a walker derive of
the same repository. The fence bumps the version at COMMIT (migration 186,
arb-8155-f1). The advisory lock is held only inside step 3.

```mermaid
sequenceDiagram
    autonumber
    participant W as Write derive (D1)
    participant C as Content Write (C2)
    participant K as Walker derive (D2)
    participant PG as Postgres

    Note over W: ContentWriter.Write committed its content (version = n)
    W->>PG: step 1: v0 = coalesce(version, 0) = n (autocommit read)
    W->>PG: step 2: list, chunked reads, no tx, no lock
    C->>PG: content_files upsert; COMMIT bumps version to n+1
    K->>PG: step 1: v0 = n+1
    K->>PG: step 2: list, reads (sees C2's content)
    K->>PG: step 3: BEGIN; pg_advisory_xact_lock(repo)
    K->>PG: read state (none or content_version < n+1)
    K->>PG: delete rows, insert rows, upsert state content_version = n+1; COMMIT
    W->>PG: step 3: BEGIN; pg_advisory_xact_lock(repo)
    W->>PG: read state: content_version = n+1 > v0 = n
    W->>PG: ROLLBACK (outcome superseded)
    Note over PG: state.content_version = version = n+1: readers trust K's rows
```

If D2 had not run, D1 would write rows labelled `n` while the version is
`n+1`. Readers then see `stale_version` and serve the repository live until a
later derive catches up. D1 cannot label rows with a version newer than the
content it read, because it reads `v0` before its list.

## Decisions

| Item | Choice | Source |
|---|---|---|
| Repository lock | `pg_advisory_xact_lock(hashtextextended(repoLockKey(repo_id), 0))` with `repoLockKey` = `'config_artifact_paths:' \|\| repo_id`, the shape of `infra/inventory`'s `repoLockSQL`, taken only through `lockRepository` (`lock.go`), which the failure-only update and the walker reuse | protocol section 3, step 3 (key not specified); coordinator ruling 2026-10-17 |
| Write conditions | no state row or `row_count` NULL (**Proposed**, as the protocol marks it), or `v0 > content_version` with extractor at least the stored one, or `v0 = content_version` with a greater extractor | protocol section 3, step 3 |
| A NULL `content_version` with a non-NULL `row_count` | treated as no successful derive, as `Classify` does | `port.go` |
| Failure-only update | one upsert under the same repository lock in its own short transaction: `failure_count + 1`, `last_error_class`, `last_failure_at`, `last_failure_extractor_version`. A new row carries the derive's `extractor_version`; an existing row keeps its `extractor_version`, `content_version`, `row_count` and rows | protocol section 3, failure semantics |
| Failure-update context | `context.WithoutCancel` of the caller plus its own 5 s bound, so a timed-out derive still records its failure | this slice |
| Deadline | default 10 s, constructor option. It interrupts every statement and is checked again before step 3. A CPU-bound extractor is not preempted: Go cannot stop a goroutine, and abandoning one leaks it. The overrun shows as outcome `timeout` | protocol section 3 |
| Panic | recovered in the derive goroutine (extractors, row mapping, write). A panic inside a chunk-read goroutine of the neutral package is not recoverable here | protocol section 3 |
| `fence_check_failed` | maps to outcome `fence_missing`, as `encoding_check_failed` maps to `encoding_unsupported` (`Encoding` godoc); written into decision record section 3.1, item 7 and the protocol's start-up bullet | coordinator ruling 2026-10-17 |
| `ExtractorVersion` | a required constructor input; the wiring slice supplies it, and the S0 digest guard has not landed | coordinator ruling 2026-10-17 |
| Counting | no metric in this slice. `DeriveRepository` returns a `Result` (outcome, trigger, duration, counts, error class) for the wiring slice to count | issue claim comment |

## Proof

Hermetic tests (`derive_test.go`, a scripted database) pin the outcome
mapping, every branch of `mayWrite`, the statement order and transaction
boundaries, panic recovery, the deadline (a context-aware load and a
CPU-bound overrun), a canceled caller, the start-up short circuits, and that
a failed derive sends no row statement outside a rolled-back transaction.

Live tests on a disposable PostgreSQL 18.6 database
(`derive_*_live_test.go`):

| Claim | Test | Result on 2026-10-17 |
|---|---|---|
| S4: stored rows after the name step equal the live path's rows | `TestDeriveStoredRowsEqualLivePathLive` | 220 repositories (every `tests/fixtures` directory and child, plus the 26 recorded S0 corpus repositories), 43 with rows, 160 live rows, 160 stored rows, differing 0 / 0 |
| Two derives of one repository | `TestDeriveTwoDerivesOfOneRepositorySerializeLive` | the second waits on the advisory lock and is superseded; 20 racing rounds end one `ok`, one `superseded` each |
| A derive racing content writes | `TestDeriveRacingContentWritesNeverLabelsNewerContentLive` | 3 derive loops over 40 writes, never rows older than their label, converges after the writes. The counts depend on the interleaving: one run gave `ok` 38, `superseded` 327 and 72 snapshot checks, another `ok` 41, `superseded` 1,592 and 79 checks |
| Two repositories in parallel | `TestDeriveTwoRepositoriesRunInParallelLive` | repo-b completes while repo-a holds its lock |
| Version read before the list | `TestDeriveReadsVersionBeforeListLive` | a write injected after step 1 leaves `content_version = v0 < version`, `Classify` = `stale_version` |
| Lock only in step 3 | `TestDeriveHoldsTheLockOnlyInStepThreeLive` | during extraction 0 advisory locks and 0 open transactions; 1 lock inside step 3; 0 after commit |
| Older extractor | `TestDeriveOlderExtractorNeverReplacesNewerRowsLive` | superseded at the same and at a newer version; rows and state unchanged |
| Rollback | `TestDeriveRollbackLeavesRowsAndStateLive` | superseded and failed-insert (`sqlstate_22021`) derives leave rows and trust columns unchanged; `failure_count` 1, then 0 after a success |
| Failure-only row | `TestDeriveTimeoutAndPanicRecordFailureOnlyLive` | timeout and panic leave `row_count` and `content_version` NULL, `failure_count` 2; a later derive writes (Proposed clause) |
| One lock key for write and failure | `TestDeriveWriteAndFailureRecordShareOneRepositoryLock` (hermetic), `TestDeriveFailureRecordWaitsOnTheWriteLockLive` | both send `config_artifact_paths:repo-a`; live, the failure update waits on the held write lock, and the held and waiting `pg_locks` keys both equal `hashtextextended(repoLockKey, 0)` |
| Failure update racing a newer write | `TestDeriveFailureRecordRacingNewerWritesLive` | 30 rounds, both orders (13-17 split over three runs): `content_version` always the current, advancing version, rows from that round |
| Start-up checks | `TestDeriveMapsEveryStartupCheckResult` (hermetic) | real `CheckEncoding` and `CheckFence` results: both encoding reasons give `encoding_unsupported`, all fence reasons (`fence_check_failed` included) give `fence_missing`, no statement |

Mutation sweep (each mutation applied alone, the named tests run, the file
restored with `git checkout`):

| Mutation | Killed by |
|---|---|
| M1 equal version allows an equal extractor (`>=`) | `TestMayWriteConditions`, `TestDeriveTwoDerivesOfOneRepositorySerializeLive`, `TestDeriveRollbackLeavesRowsAndStateLive` |
| M2 newer version ignores the extractor | `TestMayWriteConditions`, `TestDeriveOlderExtractorNeverReplacesNewerRowsLive` |
| M3 drop the Proposed failure-only clause | `TestMayWriteConditions`, `TestDeriveTimeoutAndPanicRecordFailureOnlyLive` |
| M4 hold an advisory lock through step 2 (a second key, so the derive does not wait on itself) | `TestDeriveHoldsTheLockOnlyInStepThreeLive` |
| M5 read the version after the list | `TestDeriveReadsVersionBeforeListLive` |
| M6 no deadline check before step 3 | `TestDeriveCPUBoundOverrunIsATimeout` |
| M7 failure update sets `content_version = NULL` | `TestDeriveLoadErrorRecordsFailure`, `TestDeriveRollbackLeavesRowsAndStateLive` |
| M8 failure update on the canceled derive context | `TestDeriveCanceledCallerIsAnError` |
| M9 no panic recovery | `TestDeriveRecoversPanic` |
| M10 failure update locks its own key copy (`repo_id` only) | `TestDeriveWriteAndFailureRecordShareOneRepositoryLock`, `TestDeriveFailureRecordWaitsOnTheWriteLockLive` |
| M11 failure update takes no lock | `TestDeriveWriteAndFailureRecordShareOneRepositoryLock`, `TestDeriveFailureRecordWaitsOnTheWriteLockLive` |
| M12 `fence_check_failed` not mapped to `fence_missing` | `TestDeriveMapsEveryStartupCheckResult` |
| M13 `encoding_check_failed` not mapped to `encoding_unsupported` | `TestDeriveMapsEveryStartupCheckResult` |
| M14 failure update sets `content_version = content_version - 1` | `TestDeriveFailureRecordRacingNewerWritesLive` |
| M15 failure update deletes the rows | `TestDeriveFailureRecordRacingNewerWritesLive` |

15 of 15 killed. The write-transaction timings, the `Performance Evidence:`
and the `Observability Evidence:` markers are in the package README
(`go/internal/storage/postgres/config/artifacts/README.md`, section "Evidence
(PR3b-2b, the derive core)").
