# #10820 census 2e purge: no-regression evidence

Repository retirement now deletes the census `2e` tables that had no deleter,
inside the purge transaction. This note is the tracked record the perf-evidence
gate requires, and it records the rows the purge deliberately leaves and why.

## No-Regression Evidence:

The purge transaction gains these statements, once per repository. Plans are
from PostgreSQL 18.6 (`EXPLAIN (ANALYZE, BUFFERS)` inside a rolled-back
transaction, throwaway database, real migrated schema).

| Table | Statement key | Plan on populated data | Time |
| --- | --- | --- | --- |
| `relationship_evidence_facts` | `generation_id IN (scope_generations UNION relationship_generations of the scopes)` | `Index Scan using relationship_evidence_generation_idx`, 20 generations, 400 of 1,200,000 rows | 6.5 ms |
| `relationship_candidates` | same | `relationship_candidates_generation_idx`, 200 of 600,000 rows | 3.0 ms |
| `projection_decision_evidence` | `decision_id IN (decisions of the repository)` | nested loop, `projection_decisions_repo_run_idx` then `projection_decision_evidence_decision_idx`, 200 of 600,000 rows | 3.7 ms |
| `projection_decisions` | `repository_id = $1` | `projection_decisions_repo_run_idx`, 100 of 300,000 rows | 0.5 ms |
| `service_materialization_generations` | `scope_id = ANY($1)` | `Seq Scan` over 300,000 rows, 100 matched (see residual 5) | 110 ms cold |
| cascade pre-count incl. `service_evidence_snapshots` | grant counts plus snapshots by generation | parallel `Seq Scan` on the service table, index-only on `service_evidence_snapshots_diff_idx` | 20 ms |

The remaining statements (`reducer_readiness_waits`,
`crossplane_satisfied_by_redrive_state`, the redrive target ledger,
`relationship_generations`, `config_artifact_paths`, both `content_file_ref_*`
tables, the two fingerprint tables, the three `function_*` tables,
`infra_resource_entities`, `infra_resource_entity_dirty_repos`,
`package_manifest_consumption_key_dirty_scopes`) each have an index whose
leading column is the predicate column (checked against `pg_indexes` on the
migrated schema), and each is a single-statement delete bounded by one
repository's rows.

- Conflict domain: the rows of the purged scopes and repository. The purge
  stays one transaction per repository, with the same `NOWAIT` scope lock.
  Worker counts, leases, batching, and retries are unchanged.
- Added locks: two transaction-scoped advisory locks per purge, the infra
  inventory repository lock and the config-artifact repository lock. A derive
  holds the same lock for its whole write, so a purge that starts behind a
  derive waits for the derive to commit. The wait is bounded (arbiter ruling):
  the purge sets a 10 s transaction-local `lock_timeout` (the config derive
  deadline) before the two locks, restores the captured value once both are held
  so later deletes keep their prior wait behavior, and on SQLSTATE 55P03 returns
  the designed blocked exit `derive_lock_busy` with a WARN log. Nothing is
  written before that point (only the tombstone claim, the `NOWAIT` scope lock
  and reads), so the rollback is clean and the fenced tombstone stays open for a
  retry. This is a bounded wait with a retry, not serialization: no worker
  count, batch size, or writer changes.
- Statement order and lock-order proof, each against real PostgreSQL by a
  parked-writer probe (`runPurgeBehindWriter`): the writer holds its first lock,
  the purge starts and is observed blocked behind it through `pg_blocking_pids`,
  the writer takes its second lock and commits, and the purge must finish.
  - `relationship_generations`: the claim variant of the activation locks the
    claimant `fact_work_items` row, then writes `relationship_generations`. The
    purge deletes it after the scope delete, whose cascade has locked the work
    rows. Moving the delete before the cascade (after the evidence and candidate
    tables) fails `TestRetirementPurgeRelationshipGenerationsLockOrderLive` with
    `ERROR: deadlock detected (SQLSTATE 40P01)`.
  - Infra advisory lock: derive takes the lock, then rewrites
    `content_entities` and the mirror. Moving the lock after the
    `content_entities` delete fails
    `TestRetirementPurgeInfraAdvisoryLockOrderLive` with a deadlock.
  - Config-artifact advisory lock and the service materialization order
    (generation row, then snapshot rows) pass in their intended order
    (`TestRetirementPurgeConfigArtifactAdvisoryLockOrderLive`,
    `TestRetirementPurgeServiceMaterializationLockOrderLive`).
- Row counts and neighbors: `TestRetirementPurgeCensus2eTablesLive` seeds every
  table for the retired repository and for a second repository, purges the
  first, and requires zero rows for it, the sentinel's rows unchanged, and the
  tombstone ledger count per table. With the infra dirty-mark delete moved before
  the `content_entities` delete (run as a session that is not derive-aware) the
  test fails with `infra_resource_entity_dirty_repos: 1 rows survive the purge`,
  because the `109` row trigger re-marks the repository.
- Guard: `TestRetirementCensus2eRowsHaveACoveringPurgeStatement` fails when a
  census row lists `2e` and no purge statement deletes the table (against origin/main it
  names 28 tables: the 20 purged here, the snapshot cascade child, the 6 rows now ruled
  `kept` or `deferred`, and the winners row); `TestRetirementCensusGuardSeesAnUncoveredRow`
  proves it can fail; `TestRetirementCensusCascadeChildrenAreForeignKeyed`
  proves `service_evidence_snapshots` is an `ON DELETE CASCADE` child.
- Dirty-mark position for `package_manifest_consumption_key_dirty_scopes`: the
  delete sits after the cascade because the `139` trigger can mark the scope
  again. A live mutant that moves it before the cascade did not fail: the
  key-sidecar prune earlier in the transaction sets the sidecar marker, which
  suppresses the trigger. The order is defensive and is pinned by the scripted
  order test only.
- `winners`: `supply_chain_impact_canonical_winners` needs no statement. The `272`
  trigger logs the scope on the purge delete and the keyed resweep removes the
  winners, proved by `TestSupplyChainImpactWinnersResweepRetirementFenceThenPurgeLive`.
  The census row is corrected from `2e` to `reaped`.

## Rows the purge leaves, and why

Arbiter ruling on #10820: two rows are `kept`, four are `deferred` behind open
issues. No row is left without a reason.

### Kept: `config_artifact_content_versions`, `config_artifact_repo_state`

The purge deletes neither row, and that is what keeps the version fence sound.

- The purge's `content_files` delete stages the repository, and the `186`
  constraint trigger `config_artifact_fence_pending_commit` bumps the existing
  version row from N to N+1 at COMMIT. A retired repository does not end with a
  version-1 row; that outcome exists only in the hypothetical delete that
  `TestRetirementConfigArtifactVersionRowIsRecreatedAtCommitLive` runs as a
  trigger probe.
- After the purge the reader sees `state.content_version != version` and
  answers `stale_version`, so it serves live, which is empty. The walker's
  universe is the versions table, so it re-derives the repository to zero rows
  and converges the state to (N+1, row_count 0). Residue is two small rows per
  retired repository and no content.
- A derive's write fence compares its read version `v0` to the state row only,
  and `v0` is read before the lock. A derive that read before the purge can
  therefore still write its pre-purge rows after the purge commits. With the
  version row kept and bumped those rows are stale and the walker removes them.
  With the version row deleted the write is allowed, the repository is outside
  the walker's universe, and the rows are orphaned; on readmission the counter
  restarts at 1 and can equal the orphan state's `content_version`, which makes
  the reader trust pre-retirement rows (the hazard the 7126 rule names).
- Rejected: committing with the constraint trigger forced immediate (it
  contradicts the commit-time lock order ruling and still leaves the
  in-flight derive hazard) and deleting the pending rows (it suppresses the
  version bump the purge's own content delete needs).
- `config_artifact_paths` is purged. Its Reason states what the code does: a
  derive that read before the purge can land a late write, the commit-time bump
  makes that state stale, and the walker removes the late rows. The version
  fence does not reject it.
- If the walker is disabled in a deployment the state row stays `stale_version`
  instead of converging; the reader still serves live, so answers stay correct.
- If the trigger's behavior ever changes, the live test fails and this ruling is
  reopened; it does not license a purge delete.

### Deferred: three projected ledgers, tracked by eshu#10822

`projected_source_edge`, `code_interproc_projected_edge`,
`code_taint_evidence_projected_node`. The ledgers are the only enumeration of
the graph source uids a graph retraction (2c) must remove; the ledger contract
prunes them only after the anchored retract completes, and the narrowed purge
does no graph retraction. Deleting them now strands the edges they name. The
retraction builders have no non-test caller, and #8659 shipped the library only,
so wiring the runner is tracked by #10822 (retraction, then ledger prune, then
cleanup of the retired repository's relationship rows under other
repositories' generations). Whether any `projected_source_edge` row is keyed by a
repository scope at all is for #10822 to settle; if none is, the row is `kept`.

### Deferred: `resolved_relationships`, tracked by eshu#10823

The table is append-only by contract: the deployable_unit_correlation input
fingerprint (#8283) is the sha256 of the sorted `resolved_id` set of active rows,
and `TestResolvedRelationshipsStayAppendOnly` fails on any non-test statement
that deletes from it. Keeping the rows is also the accuracy-preserving choice:
`resolved_id` is a pure function of (generation, source, target, type), so a
delete followed by a re-insert of the same id with different content is
invisible to an id-set fingerprint, and rows that are never deleted cannot do
that.

The purge deletes `relationship_generations`. The rows then drop out of the
fingerprint and of every generation-joined reader (`resolvedInputRowsSQL`, the
relationship read model, the evidence read model). They are not unread:
`repositoryDependencyCount` reads `resolved_relationships WHERE source_repo_id =
$1` with no generation join, so it counts superseded generations today and would
count a readmitted repository's pre-retirement rows. That is a served-accuracy
defect that predates this change, tracked by #10824. Retention for resolved rows
of superseded generations, for every repository and not only retired ones, is
#10823. Evidence and candidates, which carry no append-only guard, are purged.

### Residual: relationship `source_repo_id` (accepted, cites #10822)

The evidence and candidate tables are purged by generation id. Rows the retired
repository sourced under another repository's generation (per-commit backfill
writes other repositories' outgoing evidence under the newly onboarded
repository's generation) and rows where it is only the target are not matched.
No `source_repo_id` predicate is added now:

- The key form is unproven: the catalog id falls back from `repo_id` to
  `graph_id` to `name`, and some evidence sets the source to the deployed
  repository rather than the repository whose content held it.
- A predicate needs an index on two high-write evidence tables, which taxes
  every ingest for a rare operator action.
- Deleting evidence under another repository's active generation, while its
  resolved rows (append-only) and graph edges stay, would leave a served
  relationship with no backing evidence, which is worse than consistent but
  frozen.
- The residual is self-limiting: nothing re-emits evidence sourced by the
  retired repository once it has no active generation. The cleanup belongs with
  the cross-repository work in #10822. Deferred backfill writes under the source
  repository's active generation and skips a source with none, so it cannot add
  rows for a retired repository.

### Known residuals, recorded

- `service_materialization_generations` has no index that serves `scope_id` (the
  only one is partial on `status = 'active'`, and the delete must match
  superseded rows), so its delete and the cascade pre-count scan the table. Cost
  is linear in table size: 110 ms cold at 300,000 rows. The table has no
  retention, and its production row count was not measured. The path is a rare
  operator action (at most 25 repositories per request, one delete and one
  pre-count each), and the index doctrine asks for hot-path evidence before
  adding write cost to every service materialization commit, so no migration is
  added. Trip-wire: if `purge_duration_ms` shows this statement dominating, or
  the table passes roughly 5 million rows, add a `(scope_id)` index with EXPLAIN
  proof then. Legacy rows with a NULL `scope_id` carry no attribution
  (migration 127) and are not matched.
- A `relationship_generations` writer that takes its rows before the work row
  (the plain activation, not the claim variant) takes them in the opposite
  order. The fence leaves no in-flight consumer rows for a fenced scope, so the
  cycle needs a writer that bypassed the fence; this was not reproduced.

## Observability Evidence:

No metric or span is added; the existing purge emits counters and one INFO
completion log, and no per-step timing. The one new runtime behavior that can
stall is the advisory-lock wait, now bounded at 10 s. The completion log
`repository retirement purged` gains `derive_lock_wait_ms`, the elapsed time of
the two lock acquisitions, and `purge_duration_ms`, the whole purge transaction
for the repository (`TestPurgeRetiredLogsTheDeriveLockWait`), so a scan that
grows with a table and a purge that queued behind a derive are both visible. A
purge that times out on a derive lock logs
`repository retirement blocked: derive lock busy` at WARN with `repo_id`,
`retirement_id`, `lock`, and `derive_lock_wait_ms`, and the request reports the
repository as blocked with reason `derive_lock_busy`
(`TestPurgeRetiredReportsDeriveLockBusy`; live parked-holder probe
`TestRetirementPurgeDeriveLockBusyLive`, and
`TestPurgeDeriveLockBudgetRestoresLockTimeoutLive` for the restore). Each
deleted count joins the tombstone `rows_deleted` ledger under its table name, as
every other purged table does. A purge error surfaces through the existing
failure path with `retirement purge delete <table>`, and a lock error with
`retirement purge: lock ... repository`.
`docs/public/reference/telemetry/retirement-purge.md` lists the log fields.
