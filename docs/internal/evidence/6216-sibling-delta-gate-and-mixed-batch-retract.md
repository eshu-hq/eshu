# 6216 — the sibling delta gate, and the retract a mixed batch used to lose

## What changed

Two coupled defects in the three fenced repo-wide-retract domains that never
picked up the rationale `EXPLAINS` treatment: inheritance, SQL relationships,
and shell exec. Rationale is not touched — it already carries both halves.

**The gate.** `buildInheritanceRefreshIntents`,
`buildSQLRelationshipRefreshIntents` and `buildShellExecRefreshIntents` stamped
`delta_projection: true` whenever the SCOPE-wide `deltaScope.hasDelta` was set,
and paired it with `deltaScope.filePathsByRepoID[repoID]`, which can be empty
for a repository that qualified nothing in that generation. They now gate on
the repository's own path list, the way `buildRationaleRefreshIntents` does.

**The mixed batch.** All three `RetractEdges` branches in
`go/internal/storage/cypher/edge_writer_retract.go` returned as soon as
`collectDeltaFilePaths` reported `hasDeltaScope=true`. They now run the delta
statements and then, for the repositories in the same batch that asked for a
whole-repository refresh, the whole-scope retract as well — the `#5998` review
F6 branch rationale has had since that review.

## The extraction that came with it

Adding the mixed-batch tail pushed `RetractEdges` to 169 non-comment lines,
over the repo's 150-line `funlen` cap. The four fenced domains — inheritance,
rationale, SQL relationships, shell exec — moved into
`retractFencedRepoWideDomain`, which returns `handled=false` for every other
domain so `RetractEdges` falls through to the repo-keyed group unchanged. Each
domain's delta and non-delta branches now sit together instead of being split
across the `repoIDs := collectRepoIDs(rows)` line, which is what let the two
halves of one domain drift apart in the first place.

The extraction is behaviour-preserving, and it was not taken on trust: the
first attempt dropped rationale's non-delta branch, and
`go test ./cmd/reducer` caught it immediately with `unsupported domain for
retract: "rationale_edges"` — the fenced domains must never reach
`buildRetractStatement`. Recorded because a silent version of that mistake is
exactly the failure this issue is about.

The file lands at 486 lines against the 500-line cap. A new file would have
been the tidier home, but `go/internal/storage/cypher` is pinned in
`scripts/lib/dirgate-grandfather.tsv` at 131 files, and that ledger's only exit
is a reviewed pin bump — not something to take unilaterally inside a
correctness fix, and it would collide with the in-flight extraction PRs that
re-pin the same row.

## Why both halves had to land together

They are not two independent tidy-ups. Fixing the gate alone converts one
failure into the other.

A `ProcessPartitionOnce` batch is selected by partition **ID**
(`hashtext(partition_key) % partition_count`), not by partition key, so one
batch routinely carries refresh rows for many repositories. A repository on a
delta generation and a repository on a full generation share a batch as a matter
of course at corpus scale.

- Before either fix, a repository with nothing qualified got
  `delta_projection: true` with an empty `delta_file_paths`. That is the one
  shape `collectDeltaFilePaths` rejects outright — `delta retract requires
  delta_file_paths` — so the partition fails, retries, and dead-letters the
  intent instead of degrading to the repo-wide retract it should have asked for.
- Fix only the gate, and that repository now emits a clean non-delta refresh
  row. It lands in a batch with a delta-flagged sibling, the domain takes the
  delta branch, returns, and the repository's whole-scope retract never runs.
  Nothing else issues it — the refresh intent owns it — so its stale edges
  survive with no error and no dead letter. A loud dead-letter would have been
  traded for a silent wrong graph.
- Fix only the mixed batch, and the empty-delta payload still dead-letters.

## Reachability, stated honestly

The mixed-batch lost retract needs no new emitter shape: a full-generation
refresh row and a delta-generation refresh row in one batch is ordinary
production input, and the fix is proven at the dispatch layer against the real
`RetractEdges`.

The empty-`delta_file_paths` payload is reachable but was **not** observed at
runtime. The reducer test drives the real delta-scope builders and shows the
shape falls out of a repository fact carrying `delta_generation: true` whose
relative paths cannot be qualified. Two collector paths produce that fact:

- `repositoryFactEnvelope` (`go/internal/collector/gitrepo/
  git_content_fact_envelopes.go`) writes `delta_generation` and both delta path
  slices unconditionally for a delta snapshot, but writes `local_path` only when
  `repositoryidentity` resolved one, and never writes `path`. The reducer
  qualifies every relative path against that checkout path, and
  `semanticQualifyDeltaPath` returns `""` for an empty repo path.
- `relativePathsForSnapshotTargets` (`go/internal/collector/gitrepo/
  git_snapshot_delta.go`) resolves each changed target through `EvalSymlinks`
  but deliberately leaves the repo root unresolved in git mode, so on a
  symlinked repos root every target relativizes to a `../`-prefixed path that
  `normalizeSnapshotRelativePaths` drops — while `repository.Delta` stays true,
  because `git_selection_native.go` set it from the pre-relativization delta.

Neither has been reproduced against a live corpus. The dead-letter half is
therefore filed as conditional; the fix stands on its own because the gate it
restores is the one the sibling domain already uses, and because the
mixed-batch fix requires it.

## No-Regression Evidence:

Correctness change, not an optimization. Two costs move, one Go-side and one
Cypher-side; neither is a hot-path regression.

### Go-side: the added collector call on the delta path

Each of the three delta branches now calls
`collectWholeScopeRefreshRepoIDs(rows)` where it previously returned. Same
input shape as the `#6166` measurement so the figures are comparable: a 100-row
batch (`defaultBatchLimit` in
`go/internal/reducer/shared_projection_runner.go`), one refresh row per
repository, 100 distinct repository ids.

VERIFIED — this branch, Go 1.26.6, `darwin/arm64`, Apple M4 Pro, `-12`:

```
cd go && go test ./internal/storage/cypher -run '^$' \
  -bench 'BenchmarkCollect(RepoIDs|WholeScopeRefreshRepoIDs)RationaleNonDeltaBatch' \
  -benchmem -count=5
```

| collector | ns/op (5 runs) | B/op | allocs/op |
| --- | --- | --- | --- |
| `collectRepoIDs` | 2875 / 2844 / 2860 / 2870 / 2863 | 7912 | 9 |
| `collectWholeScopeRefreshRepoIDs` | 3875 / 3844 / 3884 / 4590 / 5207 | 7912 | 9 |

About **4–5 µs per delta batch**, allocation-identical, against a graph `DELETE`
whose own cost `#5998` measured in seconds. The last two samples sit high
because other Go test lanes were running on the same machine; the claim does not
depend on which end of that range is taken.

### Cypher-side: the whole-scope DELETE the mixed batch now issues

No statement text changed. `BuildRetractInheritanceEdgeStatements`,
`BuildRetractSQLRelationshipEdgeStatements` and `BuildRetractShellExecEdges` are
called with the same shape they already receive on the non-delta path; only the
`$repo_ids` binding differs, and `#5998` isolated that this family of `DELETE`
tracks store size rather than bound cardinality.

The added statement is **not** new work per repository per generation. It is the
retract that repository's refresh intent already asked for and that the delta
branch was dropping. In an all-delta batch — the common case —
`collectWholeScopeRefreshRepoIDs` returns empty and the branch returns before
building any statement, so nothing is executed at all.

Concurrency is unchanged. A whole-scope key hashes to exactly one partition, so
a repository's whole-scope retract is still owned by a single partition lease
and cannot race itself; this adds no second writer for that scope.

Not measured: wall-clock drain against a live NornicDB store. The change alters
which repositories a `DELETE` is bound to on one branch, not any statement's
shape, and the binding it adds is one the same statement already carries on the
sibling branch — so there is no new query shape to plan. A live before/after
would be the right proof for a statement rewrite; it is not what this is.

## Observability Evidence:

`No-Observability-Change:` — no metric, span, log field, or status field is
added, removed, or renamed. The whole-scope retract on the mixed path runs
through the same `executeInheritanceRetractStatements` /
`executeSQLRelationshipRetractStatements` / `retractShellExecEdges` helpers as
the non-delta path, so the statements it issues appear in the existing
grouped-write and retract series exactly as they already do.

One deliberate omission: `logWholeScopeRetractSkipped` is **not** called when
the mixed-batch list comes back empty. On the non-delta path an empty list is
anomalous and worth a warning; on the delta path it is the ordinary all-delta
batch, and warning there would fire on nearly every delta cycle. Rationale's
F6 branch made the same choice, and this keeps the four domains consistent.

## Verification

```
cd go && go test ./internal/reducer ./internal/storage/cypher ./cmd/reducer \
  ./internal/replay/... ./internal/projector/... ./internal/storage/... -count=1
```

Mutation-proved rather than accepted on a green suite. Each of the six
production sites was reverted individually; `go vet` exited 0 on every mutant
first, so each red is behavioural and not a compile failure:

| mutant | vet exit | test exit |
| --- | ---: | ---: |
| reducer gate, inheritance | 0 | 1 |
| reducer gate, SQL relationships | 0 | 1 |
| reducer gate, shell exec | 0 | 1 |
| mixed-batch retract, inheritance | 0 | 1 |
| mixed-batch retract, SQL relationships | 0 | 1 |
| mixed-batch retract, shell exec | 0 | 1 |

`subs=6`, six of six red. The per-domain split matters here: these three sites
are near-identical and have been fixed one at a time before, so a single
representative mutant would not have shown that all three variants are guarded.

## Classification

`Correctness win`. No wall-clock claim is made and none is implied.
