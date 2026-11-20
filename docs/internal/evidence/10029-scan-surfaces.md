# #10029 scan provenance and failure-class surfaces: evidence note

Refs #10029, refs #10002. R2 (moved from #10028): the failure class of a
failed govulncheck scan now reaches every read surface, and scan provenance
rides the explain, list, MCP, and console surfaces.

## What changed

- Reducer (`go/internal/reducer/supplychain/core`): new typed
  `schemadecode.DecodeScannerWorkerWarning` seam plus
  `extractGoReachabilityWarningsWithQuarantine`, joined in
  `classifyGoVulnerabilityReachabilityWithQuarantine` by `repository_id`
  (the emitter stamps it on the warning payload) and scoped by
  `relative_path` (#11002): a blank or whitespace-only warning path keeps
  repository scope, while a non-blank path byte-matches (no trimming, no
  case or separator folding) the `go_module_evidence` rows behind each
  finding and joins only the findings with a matching row, rendered as
  `govulncheck scan <reason> at <relative_path>`. Each unfinished
  `go_reachability` scan contributes its scoped warning text to its
  scope's findings, which rides `Reachability.MissingEvidence` through
  the existing writer payload and query decode. Warnings without a
  repository, finished scans (`analysis_status: completed`), and other
  analyzers join nothing. Tombstoned warnings are skipped; malformed ones
  quarantine as `input_invalid`.
- Cross-scope load: `goReachabilityEvidenceKinds` (storage Postgres probe)
  also loads `scanner_worker.warning` so the warning reaches the pass; the
  `ScannerWorkerHandleBinding` kind list names it.
- Console (`go/internal/cli/vulnscan`): `ReportReachability` gains optional
  `scan_level`, `scan_mode`, `scanner_version`, and `trace_count` (`*int`,
  so a reported 0 stays distinct from absent), populated in
  `ReachabilityFromFinding`. Pre-provenance findings keep the prior shape.
  SARIF (`exportspkg.Reachability`) intentionally unchanged: SARIF is
  byte-stable and outside the #10029 surface list.
- Explain/list/MCP: no shape change; the dependency (#10995, merged into
  this branch) already carries provenance through decode. New pinning tests
  prove the finding block shows the keys and the warning class, and that MCP
  passes them through.
- Readiness: new `govulncheck_scanner_state_candidates` CTE unions one
  aggregate partial `govulncheck`/`gomod` source-state row per scope from
  unfinished `go_reachability` warnings naming the request repository, with
  `last_error_class` naming the failure class, scoped by `relative_path`
  (#11002): a module-scoped warning counts only when a `go_module_evidence`
  row in the same scope names that go.mod (the same rows the reducer joins
  through); blank stays repository scope. A scope a stored govulncheck
  row covers keeps that row (`NOT EXISTS` arm); finished scans and other
  analyzers contribute nothing. The envelope reports `govulncheck:partial`
  in its incomplete reasons. Reuses the existing `$11` placeholder; no new
  index (warnings are rare, repository-bounded; the evidence probe rides
  `fact_records_scope_generation_idx`).

## Proof

| Check | Result |
|---|---|
| `go test ./internal/reducer/supplychain/core/ -count=1` | ok (includes 3 new `TestGoWarning*` + updated partial-run test + #11002 `TestGoWarningRelativePathScopesToItsModule`) |
| `go test ./internal/query/supply/chain/impact/ -count=1` | ok (includes 2 new explain/list pinning tests + 2 new live proof tests with disposable Postgres + #11002 `TestReadinessScopesGovulncheckWarningToItsModuleLive`) |
| `go test ./internal/cli/vulnscan/ -count=1` | ok (includes new provenance present/absent test) |
| `go test ./internal/mcp/ -count=1` | ok (includes new MCP pass-through test) |
| `go test ./internal/reducer/schemadecode/ -count=1` | ok |
| `go test ./internal/storage/postgres/ -run TestGoReachability -count=1` | ok |
| `TestReadinessQueryArgumentsMatchPlaceholders` | ok (no new placeholder) |
| `gofmt -l` on touched dirs, `go vet` on the impact package | clean |
| Full `./internal/storage/postgres/` suite | 1 pre-existing env failure (`TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline` needs `ESHU_DEPLOY_CHECKOUT`, absent here; fails identically on base) |

Backend/version: hermetic Go suites plus disposable PostgreSQL 18
(local Docker) for the two readiness live tests; no live backend beyond
that; no Cypher, graph write, queue, lease, worker, batching, migration,
or concurrency change.

No-Regression Evidence: `go test ./internal/reducer/supplychain/core/
./internal/query/supply/chain/impact/ ./internal/cli/vulnscan/
./internal/mcp/ ./internal/reducer/schemadecode/ ./internal/payloadusage/
-count=1` all ok on the rebased head (core 1.048s, impact 0.026s plus
5.6s for the 2 live govulncheck tests on disposable PG, mcp 3.884s);
storage reachability subset and placeholder-contract tests ok;
`go test -race` on the changed packages green via the pre-push floor;
`go vet`/`gofmt`/`git diff --check` clean. Input shape: in-memory
fixtures plus the recorded warning payloads and the seeded live rows.
The hot-path delta is one extra kind on the existing
(scope,generation,fact_kind) probe plus one filtered aggregate CTE that
only scans rare warning rows, so no new index and no measurement beyond
the suite timings is owed; no allocator, lock, or I/O shape changed.

No-Observability-Change: no new metric, span, log key, queue, reducer lane,
graph write, route, or runtime knob. Operators see the new payload keys and
the govulncheck source-state row on existing surfaces.

## Residuals

- `scanner_worker.warning` facts without `repository_id` (pre-contract
  emitters) join nothing on the reducer and bound out of the readiness CTE;
  visible only as the pre-existing generic `govulncheck call-graph evidence
  missing` gap.
- Stored govulncheck source-state rows surface only through configured
  collector targets (pre-existing gate for every source); the synthetic row
  covers the no-stored-row case the criterion names.
