# #6696 azure collector package move: no-regression evidence

In-tree prep only: `go/internal/collector/azurecloud/*` (17 root non-test
Go files plus tests, testdata, and the `runtime` subtree) moves to
`go/internal/collector/cloud/azure/*`, package `azurecloud` to `azure`.
Importers repointed without aliases or shims (`go/cmd/collector-azure-cloud`
files including a testdata path fix, `runtime` internal references).
Renamed `azureruntime` to `runtime`; no importing file uses stdlib `runtime`, so no shadowing occurs.
A new docs-only parent trio `collector/cloud/{doc.go,README.md,AGENTS.md}`
provides the prep namespace. No repo cutover (cutover follows #4047/#6707,
not this issue). AWS/GCP untouched.

## No-Regression Evidence (#6696):

- Baseline: `origin/main` at `c2ed7585d` (post-#6779); the old-path azure
  tree is byte-identical between `f89b05014` and `c2ed7585d`, so the timing
  below (measured on a clean main checkout) is a valid baseline for both.
- After: branch `feat/6696-azure-cloud` post-rebase.
- Backend/version: no live backend exercised. Unit tests only (live Azure
  tests gate behind credentials and skip); no NornicDB, Postgres, or Docker
  in these packages. Toolchain `go1.27.1 darwin/arm64` both sides, same
  machine, serial runs. Wall times are same-machine relative readings, not
  reference targets.
- Input shape: `go test -count=1` on the old paths (base) and the new
  paths (branch).
- Baseline measurement: `collector/azurecloud` ok 0.988s,
  `azurecloud/runtime` ok 1.116s.
- After measurement: `collector/cloud/azure` ok 0.260s,
  `cloud/azure/runtime` ok 0.393s, `collector` (ratchet + routing)
  ok 0.574s; factschema module (`factschema`, `aws/v1`, `ociregistry/v1`)
  green.
- Terminal counts: all test-bearing touched packages green, zero failures.
- Query/concurrency proof: every vacated `.go` file pairs to `cloud/azure`
  (R77-R100); added-line scan finds no Cypher/SQL keywords and no new
  goroutine, channel, or error-path lines — collection, retry, and
  normalization logic is byte-identical to base.
- Telemetry/log/status evidence: the set of quoted metric-name literals
  (`eshu_*`, `*_total`, `*_seconds`, `*_bytes`) in added diff lines is
  byte-identical to the set in removed lines (`METRIC-STRINGS-IDENTICAL`);
  `azure.Metrics`/`NewMetrics`/`NopMetrics` identifiers are carried renames
  (`azurecloud.` to `azure.` qualifier), not new signals; `go vet` clean.
- Stale-path proof: `verify-moved-file-refs` passes (52 vacated paths, no
  dangling); console inventory expectations, reducer AGENTS pointer,
  factschema docs, and `azure.New*` comment mentions follow the move. The
  4788 past-run evidence record keeps its as-run command (frozen history).
- Why the change is safe: rename-only nest with compiler-checked importer
  repoints; relocated tests pass in the baseline time band; emitted
  `CollectorKind` value (`"azure"`) and enums are unchanged.

## No-Observability-Change (#6696):

No operator-facing signal changes. No metrics, spans, structured-log
fields, status endpoints, or pprof knobs are added or altered — the metric
literal sets above are identical, the telemetry-coverage azure row repoints
only its path cell, and `CollectorKind` is unchanged.
