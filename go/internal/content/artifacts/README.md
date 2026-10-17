# Content artifacts

## Purpose

`content/artifacts` derives the config-path rows of one repository from its
stored files (`content_files`) and holds the SQL that reads them. It exists so
the live query path and a derive that runs when content is written share one
per-repository function and one definition of each `content_files` statement
(#7126). A copy of either would drift in `ORDER BY`, the `artifact_type`
mapping, the blank predicate or the extractors, and a stored answer would then
differ from the live answer for the same content.

## Ownership boundary

This package owns the extractors (Kustomize, Docker Compose, Terraform and
Terragrunt HCL with its locals walk, Ansible), the needed-content hydration
plan, the per-repository function, the total row order, the name step
(clean, strip) and the name-scope table, and the four `content_files`
statements with their row mappers.

It does not own the related-repository graph read, the access filter, the
per-request merge across repositories, the statistics the stage logs, the
controller, runtime, workflow and CloudFormation loaders, or any database
connection, span or metric. `go/internal/query/repositoryartifacts` owns those
and adapts `querycontract.FileContent` to `File`.

## Exported surface

- `File`, `Reader` — the file row and the read port of the per-repository
  function
- `LoadConfigPaths`, `Repository`, `ReadStats` — the per-repository function
  and its per-repository counters
- `BuildConfigPaths`, `ConfigPathRows`, `ConfigPathsKey` — the extractors over
  hydrated files
- `CompareConfigPaths`, `SortConfigPaths`, `MergeConfigPaths` — the total row
  order and the one-dedup merge
- `CleanRelativePath`, `StripRepositoryName`, `NameScoped`, `EvidenceKinds` —
  the clean step, the strip step and the name-scope table
- `ListStatement`, `GetFileStatement`, `GetFilesStatement`,
  `NonBlankPathsStatement`, `FileScanTargets`, `RelativePathScanTargets`, `SpaceCharacters` —
  the shared `content_files` statements and row mappers
- `HydrationFloor`, `AllowanceWholeWindow`, `ListLimit`, `ReadChunkSize`,
  `ReadChunksInFlight`, `BlankCheckChunkSize`, `SelectHydrationIndexes` — the
  floor, the allowance sentinel, the list window, the statement sizes and the
  single definition of the hydration set
- `ExtractorVersion` — the version of the stored rows' output; see
  "Extractor version and its guard" below
- `IsDockerComposeArtifact`, `IsDockerComposeFilename`,
  `AnsibleInventoryEvidencePath` and its siblings,
  `NormalizeConfigArtifactExpression`, `DecodeYAMLMaps` — path rules the query
  package's other loaders share

See `doc.go` for the full contract.

## Dependencies

- `gopkg.in/yaml.v3` — Kustomize and Docker Compose decoding
- `prefilter` (subpackage) — derives the literal gate of each HCL regular
  expression, so a scan is skipped when its required literal is absent
- Standard library only otherwise. No query, storage or telemetry import.

## Telemetry

None. The package emits no metric, span or log. `ReadStats` carries counts and
durations for the caller to log. The query package's `deployment_artifacts`
stage log line and the content store's `postgres.query` spans are unchanged.

## Gotchas / invariants

- A stored path goes through `StripRepositoryName` only. `CleanRelativePath` is
  not idempotent when the cleaned result has leading or trailing whitespace; a
  second clean changed 8,943 of 200,000 random strings.
- A new extractor branch that reads `file.Content` must add its selector to
  `configArtifactReadsContent` (`hydration.go`) and to the unread-class corpus
  in `hydration_differential_test.go`.
- A new evidence kind must be added to `nameScopedByEvidenceKind`
  (`name_scope.go`). `TestNameScopeTableMatchesTheRowConstructors` reads every
  call of `appendConfigArtifactRow` and `addRow` and every `evidence_kind` row
  literal with `go/parser`, and fails on a kind that is missing, in both
  families, or not a string literal (a variable, a wrapper parameter, a
  constructor used as a value). Pass the kind to a constructor as a literal.
- A change to an extractor's output changes what a stored read model holds.
  Run `TestNameFreeRowsEqualBuildWithName` and the hydration differentials,
  and bump `ExtractorVersion` with its pin (`TestExtractorOutputIsPinned`).
- Do not raise `ReadChunkSize` or `ReadChunksInFlight`, parallelize across
  repositories, or read a whole repository in one statement without a
  measurement on a byte-heavy repository: one statement detoasts serially.
- A statement is a constant here, and a plan test in the query package
  explains the exact text the reader sends. Edit the text only with a plan.

## Content is fetched only for the consumed set (#7126)

The per-repository function `LoadConfigPaths` fetches file content only for
files an extractor reads. The selection is the first `allowance` candidates in
`relative_path` order (see "The allowance and the list window" below); the rest
is what is read for them:

1. `configArtifactReadsContent` names the files an extractor reads by itself:
   kustomization files, Docker Compose artifacts, `terragrunt.hcl`/`.tf`/`.hcl`
   files, and a conventional Ansible playbook name the path rules leave to the
   content heuristic. Its single source is the selectors the extractors branch
   on (`isKustomizationIndexPath`, `IsDockerComposeArtifact`,
   `classifyHCLConfigFile`, `ansiblePlaybookReadsContent`); a new extractor
   branch that reads content must add its selector there.
2. The other selected files keep an empty `Content`. The loader drops a blank
   file before building, and a path-only row (var files, Ansible inventory,
   vars, role and task paths) exists only for a non-blank file, so they get one
   `Reader.NonBlankPaths` statement per repository. It decides
   `strings.TrimSpace(content) != ""` where the content lives and returns no
   bytes. The paths go in sorted order, `BlankCheckChunkSize` (500) per
   statement, one statement after another, so a repository can issue more
   than one blank check: at most 10 for a full 5,000-file window.
3. `planKustomizeResourceReads` runs the kustomize walk's own
   `collectKustomizeResourceFiles` over the files and names the resource files
   the kustomizations reference; only those are fetched in a second pass.

Both content reads (steps 1 and 3) go through `Reader.ReadFiles`,
one statement per chunk of 10 paths (`chunkedRead`,
`hydration_chunked.go`), with at most 8 chunk reads of one
repository in flight (#7126 PR 2). A repository issues ceil(selected reads /
10) chunks per read. Repositories are still visited one after another. The statement is `relative_path = ANY($2)`, equality on the repository
and path key, so it uses the `content_files_repo_path_pattern_idx` index. A
chunk, not a whole repository, because one statement detoasts its rows on one
backend: a single statement for a 7.4 MB repository ran 2.4 times slower than
the eight-way per-file reads it replaces.

Row semantics are those of `GetFileStatement`. A path with no stored row keeps
the list row, which the loader drops as blank. List rows that share a path read
the row once and each receive it. Rows are assigned by `RelativePath`, never by
position. A failed chunk fails the repository and leaves the rows unchanged.
Chunks launch in list order. The launch decision and the recording of a
failure run under one mutex, and a read frees its slot in the same critical
section that records its failure, so no chunk is launched once a failure is
recorded or the context is done. A chunk launched before the failure runs to completion, and its
statement may begin after the failure was recorded. The error returned is that
of the lowest-numbered failing chunk whatever the interleaving. The earlier
per-file reader returned whichever failure arrived first.

`HydrateRepositoryCandidateFiles` in the query package (used by the controller,
runtime, workflow and compose/GitHub Actions loaders for the anchor repository)
reads the whole window through the same chunked shape as the needed-content
pass above, at ten paths per statement (the #7951 follow-up). The related
repositories, which dominate the statement count, go through the
needed-content pass above.

The output of `BuildConfigPaths` is identical to reading every
candidate. One read-path difference is intended: a read failure on a file no
extractor reads no longer fails the repository, because that read is not
issued. Any read the extractors need still fails the repository load.

`ReadStats` carries the counts and durations of one repository's load. The
query package's `LoadDeploymentArtifactOverviewWithStats` sums them into the
stage statistics the repository story and context handlers log on the
`deployment_artifacts` stage line.

## The allowance and the list window (#7951)

Until #7951 the load read only the first 50 candidates of a repository and
said nothing about the rest. Now `LoadConfigPaths` takes an `allowance`:

- A positive allowance selects that many candidates in list order. The live
  query load passes `HydrationFloor` (50) plus what is left of the request's
  extra-candidate budget, so every repository gets at least the old selection
  and the rows are a superset of the old rows
  (`TestLiftedCapIsMonotoneOverGeneratedTrees`).
- `AllowanceWholeWindow` (-1) selects every candidate in the window. A
  content-write derive passes it, so stored rows never carry a budget cut.
- Any other negative allowance is an error, so a miscomputed budget cannot
  read as an unbounded one.

The list statement reads `ListLimit+1` rows. A row past `ListLimit` clips the
list to `ListLimit` files and sets `ReadStats.ListTruncated`; the clipped
files are never read, and a kustomize resource outside the window is not
read. A supplied list (the anchor repository) is used as given.

`ReadStats.CandidateCount` is the candidate pool of the window and
`CandidatesHydrated` the part the allowance took; the query package turns them
into the request's disclosure counts and reasons.

The blank check is chunked at 500 paths because a measured PostgreSQL 18 plan
of `NonBlankPathsStatement` keeps an index scan on
`content_files_repo_path_pattern_idx` at 500 paths and moves to a bitmap heap
scan at about 1,171 paths or more, at a size that depends on the table
statistics. No size gave a sequential scan. The live plan tests in the query
package (`content_reader_wide_window_live_test.go`) pin 500 and 4,950 paths.

## HCL scan prefilter (#7126)

`extractHCLConfigAssetRows` runs six regular-expression scans per `.tf` /
`.hcl` file (dependency `config_path`, `read_terragrunt_config`, include
`path`, `find_in_parent_folders`, `file(`/`templatefile(` and the module
`source`) plus the locals walk. Each regular expression scans the whole file
even when it cannot match, and most hydrated HCL (provider lock files, plain
resource files) holds none of the required literals. The `prefilter`
subpackage derives, from each pattern's own syntax tree, the literal fragments
a match must contain; `hclScanPrefilters` builds one set over the six
patterns and `planHCLScans` skips each scan whose requirement is absent. The
locals walk is skipped when the content has no case-sensitive `locals`
keyword or when no scan that resolves expressions through it can run.

This sits behind the needed-content selector above. `classifyHCLConfigFile`
decides which files are read at all; `planHCLScans` decides which scans run on
content that was read. A file the selector does not read keeps an empty
`Content`, and its rows do not depend on content
(`TestHCLPrefilterAgreesWithTheNeededContentSelector`). The locals walk lives in
`hcl_locals.go`.

The output is unchanged. The extractor still runs the same patterns on the
same helpers in the same order for every file that passes its gate, and a
gate answers false only when the pattern provably has no match. The proof is
the equality differential in
`hcl_prefilter_test.go` against a frozen copy of
the pre-prefilter extractor, the pinned derivation in
`TestHCLPrefilterDerivationIsPinned`, and the mutation sweep in
`hcl_prefilter_mutation_test.go`. The sweep pins
its few language-equivalent survivors by name with an equivalence argument, and
fails on an unpinned survivor or a pinned one that now dies.
`BenchmarkHCLScanShippedVsGated` times the frozen extractor against the gated
one on six content shapes at 2, 12 and 86 KB. A change to one of the six
patterns changes its derived gate; update the pinned derivation and re-run the
sweep in the same change.

## Extractor version and its guard (#8155)

`ExtractorVersion` (`version.go`) versions the output of the extractors: the
stored rows `(raw_path, relative_path, evidence_kind, name_scoped)` that a
content-write derive keeps. The derive stores the version on each
repository's state row, and a reader trusts a stored row only at the same
version. The constant starts at 1.

**Bump rule.** A change that can alter those rows for any input needs a
bump: an extractor, a path rule, the clean step, the hydration selection, the
list window, the dedup or the name-scope table. Bump by one and, in the same
change, append `{Version, CorpusDigest, OutputDigest}` to `extractorPins` in
`version_guard_test.go`. The failure message prints the exact row. A change
that leaves every row unchanged keeps the version: a bump makes every stored
row stale, so each repository is derived again.

**The guard.** `TestExtractorOutputIsPinned` runs the derive's own call
(`LoadConfigPaths` with no name, no supplied list and `AllowanceWholeWindow`,
as in `storage/postgres/config/artifacts/derive.go`) over the committed corpus
in `testdata/guard`, through its own in-memory store. It maps the rows as the
derive does, sorts them by repository label, `raw_path`, `relative_path`,
`evidence_kind` and `name_scoped`, and hashes every field framed as
`<byte length>:<bytes>`. It fails when:

| Condition | Message starts with |
|---|---|
| The snapshot bytes changed | `guard corpus changed` |
| The output changed at the pinned version | `extractor output changed` |
| The version changed but the output did not | `... the output did not change, so revert the bump` |
| The version was bumped without a new pin row | `ExtractorVersion was bumped to N without updating the pinned pair` |
| An evidence kind has fewer than 3 rows | `guard corpus coverage` |

`TestCheckExtractorPins`, `TestCheckGuardCoverage` and
`TestGuardIsSelfContained` are the seeded RED/GREEN pairs of the three
checks. Production row order is not in the digest: the stored rows are a set,
and `CompareConfigPaths` has its own tests.

**The corpus.** `testdata/guard/fixtures.jsonl` holds the config-artifact
candidates of the S4 set (each `tests/fixtures` directory and child directory,
plus the 26 recorded S0 repositories). `testdata/guard/generated.jsonl` holds
the hydration tests' mutant corpus, `randomTree` seeds 1 to 150, and a small
supplement for Docker Compose secrets and `read_terragrunt_config`. The
generator writes a raw Go line citation in a fixture comment (a
`go/internal` `.go` path, a colon and a line number) with ` line ` in place
of the colon, because the doc-citations gate refuses a new one under `go/`. At
version 1 it is 297 repositories, 5,549 files and 2,021 rows, about 1 MB in
two files under the 1,024 KB large-file hook. The snapshot is frozen, so a
fixture or helper edit elsewhere cannot move the pin, and the guard's sources
must not read those inputs (`TestGuardIsSelfContained`). The S4 live test in
`storage/postgres/config/artifacts` (`TestDeriveStoredRowsEqualLivePathLive`,
220 repositories through the Deriver against Postgres) stays the broader
stored-equals-live proof. This snapshot is only the version-guard corpus.

**Re-snapshot procedure.** Change the corpus only in a commit that changes no
extractor:

1. With the committed snapshot, run
   `go test ./internal/content/artifacts/ -run TestExtractorOutputIsPinned -count=1`.
   It must pass. A failure here is an extractor change, so bump instead.
2. Regenerate:
   `go test ./internal/content/artifacts/ -run TestRegenerateExtractorCorpus -count=1 -args -update-extractor-corpus`.
   The generator keeps only candidate files and fails unless each
   repository's rows over the kept files equal its rows over every file, and
   unless each repository stays under `ListLimit`, each file under 256 KiB and
   each snapshot file under 1,000 KiB. Its output is byte-identical on a rerun.
3. Set the last `extractorPins` row's `CorpusDigest` and `OutputDigest` to the
   values the generator logs, at the same version.

## Related docs

- `docs/internal/design/7126-ingest-time-config-artifacts.md`
- `docs/internal/design/7126-ingest-time-config-artifacts-protocol.md`
- `go/internal/query/repositoryartifacts/README.md`
