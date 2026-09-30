# #7447 item 5: a full snapshot removes the content rows an earlier generation left

Root-Cause Evidence: the #7389 reproduction shape at the content store, run live
against PostgreSQL 18.6 with the real `content_store` definitions
(`TestContentWriterFullSnapshotReapsPathsAWriterLeftBehind`). Generation A wrote
`keep.go w.go y.go`; the delta B wrote `x.go` and deleted `w.go`, then was
superseded and never activated; the full snapshot D at `{keep.go, w.go, y.go}`
left `content_files = [keep.go w.go x.go y.go]`. `x.go` also survived in
`content_entities` and `content_file_references`.

## Why the row stayed

`content_files` has no generation column and reads are latest-wins by
`(repo_id, relative_path)`. At write time `ContentWriter.Write` deleted a row only
for a `Record` marked `Deleted`, and a full snapshot has no tombstone for a path
that only a never-activated generation wrote. Generation retention does have a
`prune_content_files` phase (#7279): it deletes a row whose only live file facts
sit in pruned generations. That needs the superseded generation to pass the
168 hour age gate and fall out of the newest 24 per scope, so the row stays
queryable for at least a week, and indefinitely for a superseded generation that
stays inside the newest 24. The reap removes it at the next full snapshot.

## Change

- `content.Materialization.FullSnapshot` (`go/internal/content/writer.go`): the
  projector's statement that `Records` is the complete file set of `RepoID`. The
  zero value never removes a row.
- `contentFullSnapshot` (`go/internal/projector/runtime/repository_refs.go`)
  sets it. It is true only for a repository-scoped generation that is not a
  delta by the generation flag, that carries the repository fact, and whose
  repository fact does not declare `delta_generation`. That is the marker
  `canonical.BuildMaterialization` reads (`extractDeltaProjectionScope`), so the
  content store and the graph agree on what a delta is. A `delta_generation`
  fact with no listed paths counts as a delta here, the conservative side.
  The scope-kind and repository-fact conditions exist because a
  `terraform_state` scope carries `repo_id` in its metadata and emits no file
  facts: without them an empty `Records` would delete a repository's content.
- `Materialization.RetainedPaths` carries the paths of the generation's file
  facts. The git collector skips a file's content fact when its body cannot be
  re-read after parsing (any error, not only a missing file;
  `fact_builder.go`, the content re-read loop), while the file fact, and so the
  graph File node, is still emitted. Without `RetainedPaths` the reap would turn
  that silent skip into a delete of a file that still exists. A review found this.
  The writer treats a retained path as present, so only a path in neither the
  content records nor the file facts is removed.
- `ContentWriter.staleContentFilePaths` (`content_writer_reap.go`) reads the
  repository's stored paths, keeps those not in `Records`, and `Write` feeds them
  through the same entities, references, then files batch deletes a `Deleted`
  record uses. `reapStaleFingerprints`, which already runs after the entity
  upsert, then removes their fingerprint side rows. The stale paths are also
  appended to the materialization as deleted records, because the infra read model
  (`infra_resource_entities`) is derived from the paths of the records it is given;
  without that its rows outlive the content they mirror, and nothing marks the
  repository dirty. A review found this.

This mirrors the graph. On a full generation `canonicalNodeRetractFilesCypher`
`DETACH DELETE`s every `:File` of the repository not stamped with the current
generation, keyed by `mat.RepoID`, and takes the bounded delta path only when
`mat.DeltaProjection` is set. Content now trusts a full generation to be complete
for its `repo_id` exactly as far as the graph already does, and no further.

## Theory measured before it was built

The candidate read is `SELECT relative_path FROM content_files WHERE repo_id = $1`,
diffed in Go. Throwaway PostgreSQL 18.6, the real `content_files` DDL, 1.5
million rows over 300 small repositories plus repositories of 10k, 200k and 1M
files, about 1 KB of content per row (table 1851 MB, primary key 165 MB),
`EXPLAIN (ANALYZE, BUFFERS)`:

| repository files | plan | execution, cold then warm |
| --- | --- | --- |
| 1,000 | Index Only Scan, 1 index search | 0.2 ms |
| 10,000 | Index Only Scan, 1 index search | 6.0 ms |
| 200,000 | Index Only Scan, 1 index search | 45 ms, 21 ms |
| 1,000,000 | Index Only Scan, 1 index search | 211 ms, 139 ms |

The existing 500-path chunk delete (`(repo_id, relative_path) IN (...)`) planned as
a nested loop of primary-key index scans, 6.4 ms per 500 rows; 20,000 stale paths
in 40 chunks in one rolled-back transaction took 129 ms.

## Proof

Test-First (all fail without the change, all pass with it, PostgreSQL 18.6):

- `TestContentWriterFullSnapshotReapsPathsAWriterLeftBehind`: the shape above.
  Fails with `content_files = [keep.go w.go x.go y.go], want [keep.go w.go y.go]`.
- `TestContentWriterFullSnapshotReapIsScopedToItsRepository`: another repository's
  files and entities are untouched.
- `TestContentWriterFullSnapshotReapsAcrossDeleteChunks`: 1,200 stale paths, more
  than two 500-row chunks, from `content_files` and `content_entities`.
- `TestContentWriterFullSnapshotOfEmptyRepositoryReapsItsContent`: an emptied
  repository loses its content, as its graph files do.
- `TestContentWriterFullSnapshotReapIsIdempotent`: a replayed full snapshot ends
  in the same tree.
- Projector side, `TestBuildProjectionMarksContentFullSnapshot` (eight cases) and
  `TestBuildProjectionRefScopeNeverMarksContentFullSnapshot`: a full generation and
  a reconciliation full are true; the generation delta flag, a `delta_generation`
  fact with and without paths, a missing repository fact, a missing `repo_id`, and
  a non-repository scope kind that carries a `repo_id` are false.

Added after review: `TestContentWriterFullSnapshotDerivesInfraInventoryForReapedPaths`
(fails before the fix with `infra derive paths = [a.tf], want [a.tf b.tf]`),
`TestContentWriterFullSnapshotKeepsRetainedPaths` (removing the retained-paths loop
makes it fail: `content_entities deleted pairs = [repo-1|gone.go repo-1|skipped.go]`)
and `TestBuildProjectionRetainsFilePathsOnlyForFullSnapshots`.

Guard that passes before and after: `TestContentWriterDeltaNeverReapsUnlistedPaths`
(a delta leaves every path it does not name). Mutation check by the author:
making `staleContentFilePaths` return nothing fails the five reaping tests on
their assertions, including the 1,200-path case, and leaves the guard green.

The projector's clone-path reference implementation in `clone_removal_test.go`
sets the flag the same way, so the borrow-versus-clone equivalence test still
compares like with like.

## Concurrency and edge cases

- Conflict domain: the `content_files`, `content_entities` and
  `content_file_references` rows of one `repo_id`. Only that repository's rows are
  read or deleted.
- Ordering: the stale set is computed from the materialization's own `Records`,
  not from the state of the database, so it can never include a path this
  `Write` is upserting. Deletes run entities, references, then files, the
  tombstone order, before the file upsert; the fingerprint reap runs after the
  entity upsert as before.
- Read and delete are separate autocommit statements. That is safe for the reason
  the entity reap gives: `claimProjectorWorkQuery` allows one unexpired claim per
  `scope_id`, and `git-repository-scope:<repo_id>` is one scope per repository
  (`source_processing.go`), so two `Write` calls for one repository do not overlap.
  A writer whose lease expired but is still running is outside that guard, as it
  already is for the entity reap; the next full snapshot converges the store.
- Retry and idempotency: a crash after some deletes and before the upserts leaves a
  subset; the retry recomputes the same stale set, because it depends only on
  `Records` and the stored paths, and finishes.
- First generation: the read finds nothing stale, so it issues no delete.
- A delta, a reconciliation delta and every non-repository scope never read or
  delete: `staleContentFilePaths` returns before the query.

## Known limits

- Only paths present in `content_files` are enumerated. An orphan `content_entities`
  or `content_file_references` row whose file row is already gone is not found.
  Every writer path deletes the three together, so none is created by the flow
  this fixes; a sweep of existing orphans is a separate concern.
- The one-writer-per-scope guard is the existing one. A writer whose lease expired
  but is still running can read the stored paths after a newer generation has
  upserted and delete that generation's fresh paths; the entity reap and plain
  upserts share this class, and the next full snapshot converges the store.
- No new counter. The signal is the `reap_stale_files` stage log below.
- No end-to-end run through the real ingester, projector and Neo4j. The live proof
  is the content writer against real PostgreSQL; the graph side of the #7389 shape
  is `TestSupersededDeltaOverlaySurvivesNextDeltaLive`.
- The theory numbers come from a synthetic corpus with one path shape per
  repository, on a laptop, warm cache after a first run. They bound the cost of
  the read; they are not a measurement of a full projection.

No-Regression Evidence: no change for a delta or a non-repository scope (no query).
A full snapshot adds one indexed read of the repository's paths, measured above
at 21 ms warm for 200,000 files and 139 ms warm for 1,000,000, and one delete pass
sized by the stale set (about 6 ms per 1,000 stale paths at 500 per statement, from 129 ms for 20,000),
which is zero for a repository with nothing stale. Worker count, batch sizes,
leases and lock order are unchanged.

Observability Evidence: a full snapshot logs the existing
`content writer stage completed` line with `stage=reap_stale_files`,
`stale_file_count`, `duration_seconds`, `scope_id`, `generation_id` and `repo_id`,
next to the sibling `reap_stale_entities` and `reap_stale_fingerprints` stages. An
operator at 3 AM finds a snapshot that removed an unexpected number of files by
`stale_file_count`; `deleted_count` keeps counting tombstones only.
