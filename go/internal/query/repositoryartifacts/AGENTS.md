# AGENTS.md - repositoryartifacts

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary and proof requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Invariants

- Leaf package: standard library, `querycontract`, its own `prefilter`
  subpackage, and content-parsing libraries only. Never import `repository`,
  `repository/readmodel`, or the query root.
- Config-artifact content is fetched only for the consumed set (#7126). A new
  extractor branch that reads `file.Content` must add its selector to
  `configArtifactReadsContent` (`repository_config_artifacts_hydration.go`) and
  to the unread-class corpus in
  `repository_config_artifacts_hydration_differential_test.go`. The generated
  tree test and the unread-class guard fire only for content in
  `poisonContents`; a new extractor branch must add content that moves it, or
  the guards cannot see the read.
- The config-artifact load reads content with `ContentStore.GetFileContents`,
  10 paths per statement and at most 8 chunk reads of one repository in flight
  (`chunkedRead`). Do not raise either number, parallelize across repositories,
  or read a whole repository in one statement without a measurement on a
  byte-heavy repository: one statement detoasts serially. A new read of
  config-artifact content goes through `configHydrationPolicy.fetch`, so the
  per-file reader the equality tests use as an oracle (`perFileFetch`) can
  compare it. Keep the failure rule: the lowest-numbered failing chunk wins and
  a failed pass leaves the rows unchanged.
- Artifact predicates feed access decisions (`filter...ForAccess` callers);
  keep the fail-closed behavior and bounded limits exactly as they are.
- The HCL extractor skips a regex scan only when `prefilter` proves the
  pattern has no match (#7126). Do not hand-write literals for a gate: derive
  from the compiled pattern. A new or edited HCL pattern needs its pinned
  derivation and mutation sweep updated in the same change, and the equality
  differential against the frozen shipped extractor must stay green.

## Verification

Run focused `repositoryartifacts` tests, then `repository` and root `query`
suites. Run `scripts/verify-package-docs.sh` whenever this package changes.
