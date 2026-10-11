# Neutral config-artifact package: proof (#7126, PR 3a)

PR 3a moves the per-repository config-artifact function, its extractors and the
`content_files` statements out of `go/internal/query/repositoryartifacts` into
`go/internal/content/artifacts`. A derive in `storage/postgres` can then call
the same code. The change does not alter extraction. One order rule changes:
`evidence_kind` is now the fourth sort key. This note holds the proof.

## What moved

| Concern | Before | After |
|---|---|---|
| Extractors (kustomize, compose, HCL and its `prefilter`, Ansible) | `query/repositoryartifacts` | `content/artifacts` |
| Needed-content plan, chunked read | `query/repositoryartifacts` | `content/artifacts` (`LoadConfigPaths`) |
| `content_files` SQL: list, single read, batched read, non-blank | inline in `query/content_reader*.go` | one constant and one row mapper each in `content/artifacts/statements.go` |
| Related-repository read, grant filter, merge, stage statistics | `query/repositoryartifacts` | unchanged, calls the neutral package |
| File type | `querycontract.FileContent` | `artifacts.File`, adapted in `repository_config_artifacts.go` |

## Order and tie count

The live loader sorted on `path`, `source_repo`, `relative_path`. Rows that
share those three fields had no defined order. The sort now adds
`evidence_kind` as the fourth key and keeps a stable tie-breaker.

`TestNameFreeRowsEqualBuildWithName` runs 42,948 name and file-set pairs
(684,948 rows: recorded cases, a kitchen-sink tree, mutants, `tests/fixtures`
and 400 generated trees, each bound to several names):

| Measure | Value |
|---|---|
| Adjacent row pairs that tie on `path`, `source_repo`, `relative_path` | 54,003 |
| Adjacent row pairs that tie on all four keys | 0 |
| Builds whose row order differs from the pre-PR order, replayed with the old sorts | 92 builds, 184 rows |

The 92 builds are the only observable change: rows with equal first three keys
now come out in `evidence_kind` order. The set of rows is unchanged.

## Name step

`normalizeRepoScopedPath` is now the clean step (`CleanRelativePath`) and the
strip step (`StripRepositoryName`). The table `nameScopedByEvidenceKind` holds
the scope of the 16 kinds: 10 HCL and compose kinds scoped, 5 Ansible and 1
kustomize kind not scoped.

| Check | Result |
|---|---|
| Strip of the cleaned path equals a frozen copy of the pre-split function, 200,000 random paths and names | equal on every input |
| Name-free build plus strip step equals the named build, 42,948 pairs | 0 differing rows, 0 order differences |
| Scoped rows (162,609 observations) / unscoped rows (522,339) | 0 / 0 differing |
| Raw rows that collapse to one path at read | 11, all in planted trees |
| A second clean of a cleaned path differs from the first | 8,943 of 200,000 random strings, 0 over the corpus |
| Guard: the table agrees with both row-constructor call sites | pass |

Seeded mutations, each caught:

| Mutation | Caught by |
|---|---|
| Strip every kind (10,068 differing rows) | `TestNameFreeHarnessCatchesWrongApplyName` |
| No dedup on the named key (11 differing rows) | same |
| Keep the raw order (386 order-only differences) | same |
| `appendConfigArtifactRow` skips the strip for `docker_compose_env_file` (8 differing rows) | `TestNameFreeRowsEqualBuildWithName`, `TestNameTouchPoints` |
| Table value of `docker_compose_env_file` flipped to unscoped | `TestNameScopeTableMatchesTheRowConstructors` |
| A new kind added through a call wrapped over several lines in a scratch copy of `hcl.go` (the review probe) | `TestNameScopeTableMatchesTheRowConstructors` RED, clean tree green. The guard reads the syntax tree, so the wrapped call is seen |
| A variable kind, a wrapper that forwards its parameter, a constructor used as a value, a computed row-literal kind, a wrong arity | `TestRowConstructorScanFailsOnWhatItCannotRead` (each makes the scan report a problem) |

## Equality of the load

The PR 1, PR 1b and PR 2 differentials moved with the code and run against the
neutral package: the needed-content pass against a frozen full-read pass on 400
generated trees, the chunked reader against the per-file reader, the HCL gates
against the frozen extractor, and the mutation sweeps. The query package keeps
`TestConfigArtifactLoadMatchesTheAnchorFullPass`, which ties the production
load to `HydrateRepositoryCandidateFiles`.

Commands, run on the branch tree before its benchmark-and-note commit, all exit
0. That tree is named by content, not by a commit that a rebase would change:
its cumulative patch against its merge base has `git diff <merge-base> <tree> |
git patch-id --stable` equal to `cf904153567ead8433fbbad7d2abc8e4fff37f00`, on a
base that holds `6b761d2145`:

```text
cd go && go build ./... && go vet ./internal/query/... ./internal/storage/postgres/... ./internal/content/...
cd go && go test ./internal/content/... ./internal/query/... ./internal/queryplan/... -count=1
cd go && go test -race ./internal/content/artifacts/... ./internal/query/repositoryartifacts/... -count=1
```

The live PostgreSQL proofs of the statements (`TestGetFileContentsMatchesSingleReadsLive`,
`TestGetFileContentsPlanUsesAnIndexLive`, `TestNonBlankRepoFilePathsMatchesTrimSpaceLive`,
`TestNonBlankSpaceSetMatchesGoOverEveryRuneLive`) skip without a disposable
database. They now read the shared constants, so they bind the same text the
reader sends. They did not run for this change.

## Performance

No-Regression Evidence: `BenchmarkLoadDeploymentArtifactOverview` runs the
config-artifact stage through the exported loader for one anchor and 60 related
repositories (40 files each, 14 padded Terraform files with locals and joins,
two Terragrunt files, tfvars, a kustomization with two resources, a compose
file, Ansible paths), in-memory reads, 3,660 `config_paths` rows. Baseline is
the tree of `6b761d2145` (`git archive` into a scratch directory). After is
the branch tree with that patch-id plus the benchmark file. Six interleaved rounds, the first
mover alternating, `-benchtime 3s`, one binary per arm, Go 1.27.1 on an
18-thread arm64 laptop with `load1` 12 to 21 (absolute times are not
comparable to any other host).

| Arm | ns/op median (range) | B/op | allocs/op |
|---|---|---|---|
| before | 269.0 ms (265.4-288.5) | 24.55 MB | 232.6 k |
| after | 268.0 ms (262.2-274.1) | 25.28 MB | 228.7 k |

The ranges overlap, so the speed is unchanged within noise (-0.4%). The
file-type adapter copies the file list once per repository: +3.0% bytes per
op, -1.7% allocations. The row count is identical in both arms.

Benchmark Evidence: the numbers above, `go test -bench
BenchmarkLoadDeploymentArtifactOverview` in
`go/internal/query/repositoryartifacts`. The concurrency shape (chunks of 10
paths, at most 8 in flight, lowest failing chunk wins, launch decision under
the mutex) moved unchanged, with its tests.

## Observability

No-Observability-Change: the move adds no metric, span or log. The
`deployment_artifacts` stage line, its `read_*` counters and the
`postgres.query` spans of `ContentReader` are unchanged. The statement
counting wrapper still sits under the adapter, so `Statements` and
`HydratedBytes` count every read. An operator reads the same fields at 3 AM
as before. PR 3b adds the derive signals named in the decision record.

## Not checked

- The live PostgreSQL statement proofs above (no disposable database in this
  lane).
- A rebuilt-binary replay on the QA environment. PR 3a changes no behavior
  there beyond the tie order, and the read path stays `live`.
