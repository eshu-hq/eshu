# AGENTS.md - repositoryartifacts

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary and proof requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Invariants

- Leaf package: standard library, `querycontract`, `content/artifacts`, and
  content-parsing libraries only. Never import `repository`,
  `repository/readmodel`, or the query root.
- The config-artifact extractors, the needed-content hydration plan and the
  shared `content_files` statements live in `go/internal/content/artifacts`
  (#7126 PR 3a). Change them there, under that package's `AGENTS.md`. This
  package adapts `querycontract.FileContent` to the neutral `File` and runs
  the loader.
- A new read of config-artifact content goes through the neutral package's
  per-repository function (`contentartifacts.LoadConfigPaths`), not through a
  new `ContentStore` call in this package. The statement counting wrapper
  (`countingContentStore`) must stay under `contentStoreReader` so every read
  is counted.
- The config-artifact load has no silent bound (#7951). Keep the RepoID sort
  after the grant filter, the floor plus budget allowance, the related-list
  sentinel, and the candidate counts in `ConfigArtifactReadStats`. A new bound
  needs a reason in `BoundReasons` and a field on the stage line. Change the
  budget through `configArtifactLoader`, never an environment variable. The
  checks and their seeded pair are in `candidate_budget_test.go`.
- The anchor-only loaders (controller, runtime, workflow) have no silent bound
  either since the #7951 follow-up: `HydrateRepositoryCandidateFiles` reads the
  whole list window in `GetFileContents` chunks, and the fallback lists read
  `anchorCandidateListLimit+1` rows, clip to the window, and report the clip
  via the propagated truncated flag (#8157). Keep all three, and the
  `anchor_loader_bounds_test.go` bounds that pin them.
- Artifact predicates feed access decisions (`filter...ForAccess` callers);
  keep the fail-closed behavior and bounded limits exactly as they are.

## Verification

Run focused `repositoryartifacts` and `content/artifacts` tests, then
`repository` and root `query` suites. Run `scripts/verify-package-docs.sh` whenever this package changes.
