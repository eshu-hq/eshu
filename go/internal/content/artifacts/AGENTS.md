# AGENTS.md - content/artifacts

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary and proof requirements.
3. `docs/internal/design/7126-ingest-time-config-artifacts.md` (section 3.3)
   and its protocol companion (section 5) for why the package is neutral.

## Invariants

- Neutral leaf: standard library, `gopkg.in/yaml.v3` and the `prefilter`
  subpackage only. Never import `go/internal/query/...`, `go/internal/storage/...`,
  `go/internal/telemetry` or `database/sql`. A caller owns the connection, the
  spans and the metrics.
- One definition per `content_files` statement and one row mapper per
  statement. A reader elsewhere must use `ListStatement`, `GetFileStatement`,
  `GetFilesStatement`, `NonBlankPathsStatement`, `FileScanTargets` and
  `RelativePathScanTargets`, not a copy. Do not add a statement that selects a
  different column list or order for the same read.
- Output must be a pure function of the repository name and that repository's
  files. No clock, environment, network or cross-repository input.
- Row order is `CompareConfigPaths`: path, source_repo, relative_path,
  evidence_kind. Do not sort rows any other way.
- The stored path is the cleaned path. The read-time name step is
  `StripRepositoryName` on name-scoped kinds only, never `CleanRelativePath`
  again.
- Config-artifact content is fetched only for the consumed set (#7126). A new
  extractor branch that reads `file.Content` must add its selector to
  `configArtifactReadsContent` (`hydration.go`) and to the unread-class corpus
  in `hydration_differential_test.go`. The generated tree test and the
  unread-class guard fire only for content in `poisonContents`; a new
  extractor branch must add content that moves it, or the guards cannot see
  the read.
- The content read uses `Reader.ReadFiles` in chunks of 10 paths, at most 8
  chunk reads of one repository in flight (`chunkedRead`). Do not raise either
  number, parallelize across repositories, or read a whole repository in one
  statement without a measurement on a byte-heavy repository: one statement
  detoasts serially. A new read of config-artifact content goes through
  `configHydrationPolicy.fetch`, so the per-file reader the equality tests use
  as an oracle (`perFileFetch`) can compare it. Keep the failure rule: the
  lowest-numbered failing chunk wins, a failed pass leaves the rows unchanged,
  and the launch decision stays under the mutex that records a failure (a
  chunk is launched only while no failure is recorded and the context is
  live).
- The HCL extractor skips a regex scan only when `prefilter` proves the pattern
  has no match. Do not hand-write literals for a gate: derive from the compiled
  pattern. A new or edited HCL pattern needs its pinned derivation and mutation
  sweep updated in the same change, and the equality differential against the
  frozen shipped extractor (`hcl_prefilter_test.go`) must stay green.

## Verification

Run `go test ./internal/content/artifacts/... -count=1`, then
`go test -race` on this package and `./internal/query/repositoryartifacts`,
then the root `query` and `repository` suites. Run
`scripts/verify-package-docs.sh` whenever this package changes.
