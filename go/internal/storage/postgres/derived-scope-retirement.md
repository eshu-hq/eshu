# Derived Scope Retirement

Issue #10054. A repository's operator retirement also removes the scopes derived
from it. Today that is one family: `go-reachability://<repository id>`. The Go
planner reads only active repository scopes, so without this rule the derived
scope outlived its repository with its evidence active and nothing to end it.

The rule lives in the retirement path (`repository_retirements_*`), because the
derived scope is residue of the repository. It is not a planner query and not a
reducer rule: the planner cannot scan a repository with no commit, and the
reducer must not read repository lifecycle state.

## The mapping

`ingestion_scopes.partition_key` cannot find a derived scope. It holds the
planner's fairness key (`scanner_worker:<instance>:go_reachability`), shared by
every repository one scanner instance scans. A predicate through it would erase
all of them. The mapping is the scope id, built from the repository id and
compared for equality in `retirementScopesOfReposSQL` and
`retirementScopesOfRepoSQL` (`repository_retirements_derived.go`). No statement
matches a prefix when selecting scopes, so `repository:r_1` cannot reach
`go-reachability://repository:r_10`. Budget breakdowns use
`retirementScopeRepoIDSQL` to charge a derived scope to its repository.

`oci://` SBOM targets also use a URI-shaped id, but they key an image, not a
repository. A new family derived from a repository id must extend all three
fragments and `derivedParentRepoID`.

## Sequence

```text
scanner host                    operator trigger
------------                    ----------------
commit tx
  shared key: parent  <---+     mark tombstone (commits)
  shared key: fairness    |     fence tx
  read parent tombstone   |       exclusive key: parent   (waits for commits)
    open row -> refuse    +------ scope rows FOR NO KEY UPDATE, derived included
  upsert scope                    LOCK TABLE fact_work_items (1 s, restarts)
  generation, facts, work         supersede generations, NULL pointers, mark work
commit                            tombstone, assertion, commit
                                purge tx
                                  tombstone FOR UPDATE
                                  scope rows FOR NO KEY UPDATE NOWAIT
                                  leftovers, ledger pre-counts, scope delete
                                  tombstone complete, commit
```

## Concurrency note: a claim in flight

A claimed scan commits minutes after it was planned, and its partition key never
names the repository, so the folded scope gate cannot see the tombstone. Two
pieces close the race.

- **Commit gate.** `gateDerivedParent` reads the parent's open tombstone after the
  shared barrier. Any open row refuses, `complete` included: only the repository's
  own commit may readmit it. A refused commit rolls back and drains its stream, so
  the claim finishes and writes nothing. After readmission a scan is admitted again.
- **Parent key.** A first scan's scope row is uncommitted, so the fence's row locks
  cannot see it. The commit therefore holds the parent's shared key, and the fence's
  exclusive key waits until the row is visible. Without it the commit lands after
  the purge and the scope outlives the repository.

The two orders the race can take both end with no derived scope:

| The commit takes the parent key | Gate read sees | Result |
| --- | --- | --- |
| before the mark commits | no tombstone | The fence waits, sees the row, and the purge removes it. |
| after the mark commits | the open tombstone | Refused, nothing written. |

Lock order. A commit takes its shared keys in sorted order, the order exclusive
callers use: a maintenance leader holds the parent and waits for the fairness key,
so a commit holding the fairness key first would deadlock with it
(`TestDerivedCommitAndMaintenanceLeaderDoNotDeadlockLive`). The fence keeps its
order: exclusive key, scope rows, `fact_work_items` table lock, `scope_generations`,
pointer, work rows. The purge never queues behind an Ack on the derived scope row:
`NOWAIT` reports `scope_lock_busy` and the next pass retries the fenced row. Other
scopes send the single barrier statement and the folded gate they always sent
(`TestRepositoryCommitKeepsOneBarrierAndNoExtraRead`).

Retry and idempotency. The fence restarts inside its deadline and a second fence
moves nothing. A purge of a complete tombstone no-ops. A stale Ack after the fence
is refused by its lease fence and cannot reactivate a generation.

## Preview and ledger

`RetirementStore.Preview` resolves the scope set with the predicate the purge
locks. `Scopes`, `DerivedScopes`, and `DerivedFacts` compare with the ledger's
`ingestion_scopes`, `derived_ingestion_scopes`, and `derived_fact_records`. A
repository with no derived scope writes no derived ledger entry and runs no extra
statement. Preview is a store seam: no route calls it yet (`query/admin/retirement`
names the dry-run route as follow-up work).

## Re-trigger

The fence's NULL pointer and the scope delete fire the
`supply_chain_impact_winners_dirty_log` capture triggers (migration 272), so the
keyed winners tick recomputes what the derived scope fed. A finding held by another
scope keeps its Go verdict until that holder re-derives (#10077). A readmitted
repository is planned fresh by the Go planner's `has_go_module` branch.

## Evidence

No-Regression Evidence: the only hot-path change is in
`IngestionStore.commitScopeGeneration`. A non-derived scope adds one
`strings.CutPrefix` per commit and sends the statements it always sent, pinned by
`TestRepositoryCommitKeepsOneBarrierAndNoExtraRead` with fixture scope ids across
the look-alike forms (backend: scripted transaction, no live database). A derived
commit adds one shared advisory lock and one read of `repository_retirements`
through the partial open-repo index (`repo_id` equality, `LIMIT 1`). Conflict
domain: per repository, with the parent's advisory key; no worker, lease, batch, or
queue setting changes, and repositories stay independent. The operator-only fence,
purge, capture, and preview statements gain an equality arm on the
`ingestion_scopes` primary key beside the existing `partition_key` predicate. The
before and after `EXPLAIN (ANALYZE, BUFFERS)` of those statements, 100,000 scopes
on the real schema, is `TestRetirementDerivedStatementPlansLive`, enabled by
`ESHU_RETIREMENT_EXPLAIN=1`.

Observability Evidence: `eshu_dp_repository_retirement_derived_scopes_purged_total`
counts derived scopes a committed purge removed.
`eshu_dp_repository_retirement_derived_commits_refused_total{outcome}` counts
refused scans with the closed outcomes `retiring` and `retired`; a steady `retired`
rate means claims outlived the retirement. The WARN `repository ingest refused:
retiring` now names the parent in `repo_id` instead of the fairness key, the INFO
`repository retirement purged` gains `derived_scopes_deleted`, and the tombstone's
`rows_deleted` ledger names the derived rows. Statement timing stays on
`eshu_dp_postgres_query_duration_seconds` and its query spans. See
[Retirement Trigger And Purge Signals](../../../../docs/public/reference/telemetry/retirement-purge.md).
