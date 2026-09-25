# #6746: bootstrap-index selector logger wiring — evidence

One-line wiring fix: `buildBootstrapCollector` now passes the bootstrap
logger into `git.NativeRepositorySelector` (`go/cmd/bootstrap-index/wiring.go`),
so the selector's existing clone/fetch progress logs reach the operator
before the first scope commit. No new log call sites, no query, queue,
lease, or concurrency change.

## No-Regression Evidence (#6746):

- Baseline: `TestBuildBootstrapCollectorPassesLoggerToSelector` fails on the
  unfixed wiring (`selector logger not wired`), proving the selector dropped
  the bootstrap logger; `TestBuildBootstrapCollectorSelectorEmitsSelectionProgress`
  fails the same way (no shard progress line in empty logs).
- After: both tests pass; full `go test ./cmd/bootstrap-index/ -count=1`
  passes; `go test ./internal/collector/repo/git/ -run
  TestNativeRepositorySelectorSelectRepositoriesFilesystemPreservesGitHubWorkflows`
  passes; `go vet ./cmd/bootstrap-index/`, `gofumpt`, and `git diff --check`
  are clean.
- Backend/version: no backend touched (pure constructor wiring; no Cypher,
  SQL, graph, or queue read changed), so no backend benchmark applies.
- Input shape: `buildBootstrapCollector` with a discard-handler logger and
  empty-env config; the test asserts pointer identity between the passed
  logger and `selector.Logger`.
- Why safe: nil-in still yields nil-out (existing nil-logger tests
  unaffected), every downstream `s.Logger` use is nil-guarded
  (`selection_native.go`, `selection_progress.go`), and the same logger
  pointer was already passed to the sibling Snapshotter, GitSource, and
  committer. Zero new allocations on any hot path.

## Observability Evidence (#6746):

- Operator signal: previously-silent `syncGitRepositoriesWithLogger` clone/fetch
  progress (shard selection, basename-collision diagnostic, local-refs
  collection) is now emitted through the bootstrap logger before the first
  scope commit, letting an operator distinguish slow-but-active repository
  sync from a blocked dependency.
- Proven by `TestBuildBootstrapCollectorSelectorEmitsSelectionProgress`, which
  runs selection through the bootstrap-wired selector and asserts the
  shard-selection progress line reaches the bootstrap logs, alongside the
  wiring-identity test. No new metric or span was needed.
