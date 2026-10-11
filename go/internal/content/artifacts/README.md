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
- `HydrationLimit`, `ListLimit`, `ReadChunkSize`, `ReadChunksInFlight`,
  `SelectHydrationIndexes` — the caps and the single definition of the
  hydration set
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
  (`name_scope.go`). `TestNameScopeTableMatchesTheRowConstructors` reads the
  call sites and fails on a kind that is missing or in both families.
- A change to an extractor's output changes what a stored read model holds.
  Run `TestNameFreeRowsEqualBuildWithName` and the hydration differentials.
- Do not raise `ReadChunkSize` or `ReadChunksInFlight`, parallelize across
  repositories, or read a whole repository in one statement without a
  measurement on a byte-heavy repository: one statement detoasts serially.
- A statement is a constant here, and a plan test in the query package
  explains the exact text the reader sends. Edit the text only with a plan.

## Content is fetched only for the consumed set (#7126)

The per-repository function `LoadConfigPaths` fetches file content only for
files an extractor reads. The selection is the first
50 candidates in `relative_path` order, as before; the rest is what is read
for them:

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
   bytes.
3. `planKustomizeResourceReads` runs the kustomize walk's own
   `collectKustomizeResourceFiles` over the files and names the resource files
   the kustomizations reference; only those are fetched in a second pass.

Both content reads (steps 1 and 3) go through `Reader.ReadFiles`,
one statement per chunk of 10 paths (`chunkedRead`,
`hydration_chunked.go`), with at most 8 chunk reads of one
repository in flight (#7126 PR 2). A repository holds at most five chunks per
read, because the cap is 50 files. Repositories are still visited one after
another. The statement is `relative_path = ANY($2)`, equality on the repository
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
still reads one file per statement. The related repositories, which dominate
the statement count, go through the needed-content pass above.

The output of `BuildConfigPaths` is identical to reading every
candidate. One read-path difference is intended: a read failure on a file no
extractor reads no longer fails the repository, because that read is not
issued. Any read the extractors need still fails the repository load.

`ReadStats` carries the counts and durations of one repository's load. The
query package's `LoadDeploymentArtifactOverviewWithStats` sums them into the
stage statistics the repository story and context handlers log on the
`deployment_artifacts` stage line.

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

## Related docs

- `docs/internal/design/7126-ingest-time-config-artifacts.md`
- `docs/internal/design/7126-ingest-time-config-artifacts-protocol.md`
- `go/internal/query/repositoryartifacts/README.md`
