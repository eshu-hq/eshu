# Repository artifacts package

## Purpose

`repositoryartifacts` holds the file-content artifact readers for the
repository handler family (Issue #6060, lane B): config artifacts (Ansible,
Compose, HCL, Kustomize), deployment artifacts, runtime artifacts
(Dockerfile), controller artifacts, CloudFormation artifacts, and workflow
artifacts (GitHub Actions), plus the shared candidate-file hydration and
the config/deployment/workflow artifact loaders behind the repository story
responses.

## Ownership boundary

This package owns content-derived artifact reads, not handler orchestration
and not graph reads. It imports only the standard library, `querycontract`,
and content-parsing libraries. It never imports `repository`,
`repository/readmodel`, or the query root. The `repository` package and a
handful of staying root stayers consume its exported loaders and
predicates.

## Exported surface

The exported surface is described in [doc.go](doc.go). Loaders and
predicates the `repository` package or staying root stayers call are
exported; per-family artifact shaping stays unexported. Cross-package test
pins go through `testutil` or `querycontract`.

The static workflow evidence and the repository-scoped CI/CD evidence have
listing entry points (`StaticWorkflowArtifactEvidence`,
`LoadRepositoryScopedCICDEvidence`) and `...FromFiles` entry points that take
the file list the caller already read. The repository story uses the latter so
one `ListRepoFiles` read serves the semantic overview and every later stage
(#7126); the two forms return identical evidence for the same files. The listing
entry points read `RepositorySemanticEntityLimit+1` rows and clip to the limit;
the `...FromFiles` entry points take the clipped list plus a `filesTruncated`
flag carrying the caller's sentinel (#7619). The candidate pool is unknown
exactly when that sentinel row exists, so a repository with exactly 5,000 files
is a complete scan. A truncated scan carries
`candidate_pool_status=unknown_at_limit`: no observed workflows
means `state=unknown`, while positive evidence keeps its observed count. The
20-path display cap and 50-file image hydration cap remain separate. Stories
preserve the candidate marker and add `static_workflow_coverage_unknown` to
`missing_evidence` only for a capped scan; this does not backfill the typed
API's other historical missing-evidence entries into uncapped stories.

## Content is fetched only for the consumed set

The config-artifact load (`LoadSharedRepositoryConfigArtifacts`) fetches file
content only for files an extractor reads (#7126). The selection is the first
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
   `ContentStore.NonBlankRepoFilePaths` statement per repository. It decides
   `strings.TrimSpace(content) != ""` where the content lives and returns no
   bytes.
3. `planKustomizeResourceReads` runs the kustomize walk's own
   `collectKustomizeResourceFiles` over the files and names the resource files
   the kustomizations reference; only those are fetched in a second pass.

The output of `BuildRepositoryConfigArtifacts` is identical to reading every
candidate. One read-path difference is intended: a read failure on a file no
extractor reads no longer fails the repository, because that read is not
issued. Any read the extractors need still fails the repository load.

`LoadDeploymentArtifactOverviewWithStats` returns `ConfigArtifactReadStats`
(related repository count, files listed, hydrated and skipped, content
statements, hydrated bytes, list, hydrate and parse seconds). The repository
story and context handlers log them on the `deployment_artifacts` stage line.

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

The output is unchanged. The extractor still runs the same patterns on the
same helpers in the same order for every file that passes its gate, and a
gate answers false only when the pattern provably has no match. The proof is
the equality differential in
`repository_config_artifacts_hcl_prefilter_test.go` against a frozen copy of
the pre-prefilter extractor, the pinned derivation in
`TestHCLPrefilterDerivationIsPinned`, and the mutation sweep in
`repository_config_artifacts_hcl_prefilter_mutation_test.go`. The sweep pins
its few language-equivalent survivors by name with an equivalence argument, and
fails on an unpinned survivor or a pinned one that now dies.
`BenchmarkHCLScanShippedVsGated` times the frozen extractor against the gated
one on six content shapes at 2, 12 and 86 KB. A change to one of the six
patterns changes its derived gate; update the pinned derivation and re-run the
sweep in the same change.

## Dependencies

Standard library, `querycontract`, the `prefilter` leaf subpackage, and
content-parsing libraries, plus `testutil` in tests. No handler packages, no
graph drivers.

## Verification

Run focused `repositoryartifacts` tests, then `repository` and root
`query` suites. Artifact predicates feed access decisions: keep the
fail-closed behavior and bounded limits exactly as they are.
