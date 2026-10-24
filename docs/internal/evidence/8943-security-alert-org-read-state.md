# Security-Alert Org Read State (#8943)

An empty security-alert answer for a repository of an organization target used to
read `complete` when the collector never read that repository: the org stream is
a window of `max_pages` x 100 alerts, and the #8805 fan-out cannot read every
allowlisted repository (past the 100-repository cap, after a failed read, after a
rate limit or another stop). A repository with no alert has no fact, so nothing
recorded the gap. This note records the proof for the collector-owned record that
closes it: the theory checked before the build, the write path, the retention
behavior, the concurrency proofs, and the read path.

## What changed

- Migration 217 adds `security_alert_target_read_states` (one row per org target
  generation) and `security_alert_unread_repositories` (one row per unread
  repository, with its closed reason). Both cascade from `scope_generations`.
- `unread.Writer` (`storage/postgres/security/alerts/unread`) writes them in the
  commit transaction of the generation, through the new
  `collector.GenerationStateWriter` and
  `IngestionStore.CommitClaimedScopeGenerationWithState`.
- The collector builds the state for every org claim from the fan-out outcomes
  it already holds. A repository read in full is absent from it.
- The empty-answer probe reads the active generation's unread rows for the
  requested repositories and reports `target_incomplete` with the stored reason.

## Theory checked before building

The design rests on three claims, each checked on PostgreSQL 18.6 in a scratch
database (`ESHU_POSTGRES_DSN` container, no repo files touched).

1. The read is index point reads, not a scan, at the worst plausible size.
   Seeded: 50 targets x 24 retained generations x 4,900 unread repositories =
   5,880,000 child rows, vacuumed and analyzed. Query shape: `unnest(scope ids)`
   to `ingestion_scopes` (primary key) `.active_generation_id` to the parent
   (primary key) to the child rows `repository = ANY(requested)` (primary key
   `(generation_id, repository)`).

   | Read | Execution | Buffers |
   | --- | --- | --- |
   | 1 target, 3 requested slugs | 0.660 ms | 8 hit, 7 read (cold) |
   | 50 targets, 1 requested slug | 2.370 ms | 205 hit, 97 read (cold) |

   Both used `Index Scan using u_pkey` with one search per target. No new index
   is needed beyond the two primary keys.
2. Retention can ride the foreign key. `DELETE FROM scope_generations` of one
   generation with 4,900 child rows cascades through both tables in 1.3 ms
   (trigger time included in the statement). Sixteen other tables already do the
   same.
3. The writer is cheap next to the claim that produces it. One statement inserts
   5,000 unread rows in 27.4 ms (14,921 shared hits). A claim that fans out sends
   at least 100 provider requests, so this is noise.

Child rows rather than an array: the allowlist has no size bound, so a
`text[]` column would be detoasted whole on every empty answer; the primary key
lets the read ask about only the requested repositories. The parent keeps
`unread_reasons` (a few distinct tokens) so a read with no repository anchor
needs no child row.

## Write path and retention

Live proofs against a disposable PostgreSQL 18.6 database per test
(`ESHU_POSTGRES_DSN`, bootstrap schema, claim-fenced `IngestionStore`), under
`-race`:

```text
go test -race ./internal/storage/postgres/security/alerts/unread -run Live -count=1 -v
--- PASS: TestLiveCommitPersistsReadStateWithGeneration
--- PASS: TestLiveReplayOfPendingGenerationReplacesUnreadRepositories
--- PASS: TestLiveFailedStateWriteRollsBackTheGeneration
--- PASS: TestLiveGenerationRetentionRemovesReadStateWithItsGeneration
--- PASS: TestLiveRetentionPruneRunsAlongsideClaimFencedCommitsOfTheSameTarget
--- PASS: TestLiveSecondClaimOfTheSameTargetKeepsItsOwnState
--- PASS: TestLiveClaimsRacingOnOneGenerationIDLeaveOneConsistentState
ok  .../unread  17.798s
```

- Atomicity: a state that fails validation after the generation row was written
  leaves no `scope_generations`, state, child, or `fact_records` row.
- Replay: the same pending generation id committed again with a different
  freshness hint replaces the child rows; no row of the first set survives.
- Retention: `PruneSupersededGenerations` prunes the old generations and their
  state, and `RowsPruned` reports both tables (2 parent and 4 child rows in the
  fixture). The active generation's rows stay. The row-count query counts both
  tables (`generation_retention_row_counts_sql.go`), and the catalog guard
  `TestGenerationRetentionRowCountsCoverCascadeTablesLive` failed before the arms
  existed, naming `security_alert_target_read_states` and
  `security_alert_unread_repositories`. The probe pin moved from 36 to 38 tables.
- Writer against retention: 8 claim-fenced commits of new generations of the
  same target ran together with a looping retention pass over 12 old
  generations (40 unread rows each): 15 passes, 258 ms, 12 generations pruned, no
  commit or pass failure, no deadlock (`40P01`), every new generation holds
  exactly its own 25 rows, and no old row is left.
- Second claim of the same target: 4 concurrent commits of distinct generations
  each keep their own state. The scope row lock serializes the commits; the
  state rows are keyed by generation, so they never touch one another.
- Two claims racing on one generation id (6 rounds): the parent `unread_total`
  always equals the child count and one writer's whole state wins.

Sensitivity: with the child delete removed (insert `ON CONFLICT DO NOTHING`),
the replay test fails with `unread rows after replay = 4, want exactly 1` and
the racing test fails with `unread_total 7, child rows 31`.

## Read path

`TestSecurityAlertEmptyAnswerReportsAnUnreadRepositoryAsIncomplete` runs 11
cases on the list, count, and inventory routes (33 subtests):

| Case | Result |
| --- | --- |
| Requested repository unread, each of the six stored reasons | `target_incomplete`, that reason, `derived`, `fresh` |
| Stored reason this build does not know | `org_repository_read_incomplete` |
| Other repositories unread, requested one read in full | `complete`, `exact`, `fresh` |
| Nothing unread | `complete` |
| Another covering target read it in full | `complete` |
| Terminal failure on the covering target | the terminal reason wins |

With the unread classification disabled, 21 of the new subtests fail and the 12
control subtests still pass.
`TestSecurityAlertUnanchoredEmptyAnswerReportsAnyUnreadRepository` covers the
read without a repository anchor. `TestSecurityAlertUnreadRepositoriesLive` runs
the real query: it follows the active generation, ignores a superseded
generation's rows, returns nothing for a generation written before the carrier,
and returns only the requested repositories' rows.

Plan on the live schema (50 targets x 4 generations x 2,000 unread
repositories, 400,000 child rows, one requested slug):

```text
Index Scan using security_alert_unread_repositories_pkey on security_alert_unread_repositories u
  Index Cond: ((generation_id = rs.generation_id) AND (repository = ANY ('{acme7/repo-7}'::text[])))
  Index Searches: 50   Buffers: shared hit=151
Execution Time: 0.471 ms
```

The probe still reads the configuration and the target states once each (two
reads), so it stays inside the four-read bound from the #8803 ruling and keeps
its 1 s deadline.

## Observability

Performance Evidence: the added read is 0.47 ms for 50 targets on the live
schema and 2.4 ms on 5.9 million rows, only on an empty answer. The writer adds
one parent upsert, one child delete, and one child insert to the commit of an
org generation (27 ms for 5,000 unread rows) and nothing to a repository target
or an unchanged generation.

Observability Evidence: the #8805 counters
`eshu_dp_security_alert_truncated_reads_total` and
`eshu_dp_security_alert_org_fanout_repositories_total` count each truncated read
and fan-out outcome without a repository label. The `security_alert.observe` span
gains `eshu.security_alert.org_read_truncated` beside the existing
`org_fanout.incomplete` unread count. The stored record answers "which targets
are cut short" with the query in the parity doc, and
`eshu_dp_query_security_alert_empty_answers_total` counts the empty answers it
changes by coverage state and first reason. No label or attribute carries a
repository, organization, or target id.

## Not covered

- A repository added to a target after its latest generation reads as covered
  until the next generation commits (unchanged from #8803).
- A repository-scope target writes no state: a truncated read of one repository
  always holds alerts, so an empty answer cannot be a truncation gap.
- The unread reasons are not copied into freshness causes; the answer carries
  no `collector_readiness` envelope and no cause because the collector ran
  healthy.
