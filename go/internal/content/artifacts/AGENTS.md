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
- The selection takes an allowance (#7951). Never reintroduce a fixed cap in
  `SelectHydrationIndexes` or `LoadConfigPaths`: a live load passes the floor
  plus the request budget, a derive passes `AllowanceWholeWindow`. Keep the
  list sentinel (`ListLimit+1`, clip, `ListTruncated`) and the candidate
  counts in `ReadStats`; the query package's disclosure depends on them. The
  monotonicity differential (`monotonicity_test.go`) and its seeded pair must
  stay green.
- The blank check sends at most `BlankCheckChunkSize` (500) paths per
  statement. Raise it only with a plan proof at the new size: the custom plan
  leaves the index scan at about 1,171 paths.
- The HCL extractor skips a regex scan only when `prefilter` proves the pattern
  has no match. Do not hand-write literals for a gate: derive from the compiled
  pattern. A new or edited HCL pattern needs its pinned derivation and mutation
  sweep updated in the same change, and the equality differential against the
  frozen shipped extractor (`hcl_prefilter_test.go`) must stay green.

- `ExtractorVersion` versions the stored rows. A change that can alter any
  row (raw_path, relative_path, evidence_kind, name_scoped) for any input
  bumps it by one and appends its pin row to `extractorPins`
  (`version_guard_test.go`) in the same change. Never re-pin a digest at an
  unchanged version to make `TestExtractorOutputIsPinned` pass. The pin row
  also records the hydration window (`ListLimit`, `AllowanceWholeWindow`):
  a moved window at an unchanged version fails the guard, because no corpus
  repository is large enough to move a digest. The digests fail only when a
  change moves a stored row over the frozen corpus; they do not see inputs
  the corpus lacks, `storedRows` in `storage/postgres/config/artifacts`, the
  `content_files` statements, `artifact_type` or files above the 256 KiB
  pre-filter bound (README, "What the guard does not see"). A change to one
  of those still bumps.
- `testdata/guard` changes in one of two ways only. A re-snapshot at the same
  version, in a commit with no extractor change. Or a bump that adds the
  input that shows its change to `supplementSnapshotInputs` and regenerates,
  in one change, appending `{Version: N+1, CorpusDigest: <new>,
  OutputDigest: <new>, Window: <current>}`. The guard's sources must not read `tests/fixtures`
  or the hydration test helpers; only `version_snapshot_test.go` does.
- The version is monotonic. Never lower `ExtractorVersion` or delete a pin
  row. A `git revert` of a bump PR is not a rollback: `mayWrite`
  (`storage/postgres/config/artifacts/derive_write.go`) refuses a lower
  extractor version over a state row that a higher-version binary wrote.
  Undo an extractor change with a new bump.

## Verification

Run `go test ./internal/content/artifacts/... -count=1`, then
`go test -race` on this package and `./internal/query/repositoryartifacts`,
then the root `query` and `repository` suites. Run
`scripts/verify-package-docs.sh` whenever this package changes.
