# Security Intelligence CLI Contract

## CLI Contract

The local vulnerability scan command is a thin orchestration layer, not a
second scanner product. It preserves one truth model across local CLI, hosted API, MCP, and future service use by rendering stable scanner artifacts from the same reducer-owned readiness and finding envelope:

- resolve one repository or workspace root using the same local source rules as
  the existing scan workflow;
- collect manifest, lockfile, package, and repository evidence through normal
  fact emitters;
- fetch only bounded advisory and package metadata required by observed owned
  packages unless the user explicitly asks for broader coverage;
- support advisory source cache refresh, offline replay, explicit mirror
  fallback, retention cleanup, and update-only source refresh without treating
  cached source data as reducer-owned findings;
- run the same vulnerability impact reducer logic used by hosted Eshu;
- return the same finding, readiness, freshness, evidence-handle, and
  missing-evidence fields as API and MCP reads;
- provide machine-readable JSON and a concise terminal summary;
- cache advisory and package metadata locally with freshness markers so repeat
  developer runs are fast without silently using stale truth;
- fail closed when required evidence cannot be collected, and show whether the
  result is incomplete instead of printing a clean report.

This keeps the developer experience simple while preserving the accuracy rule:
the CLI can be convenient, but it must not produce a result that means
something different from the hosted graph.

The current `eshu vuln-scan repo [path]` implementation covers local root
resolution, local service attach/start when no API is configured, scan
readiness proof, repository-scoped impact reads, JSON envelopes, terminal
summaries, and fail-closed incomplete target behavior. Advisory source cache
state is exposed through readiness metadata. Package metadata cache freshness
is part of the scoped fail-closed guard. The local fixture corpus now uses only
synthetic repository names, package names, and advisory identifiers while
covering vulnerable and ready-zero states for npm, Yarn, pnpm, Go modules, PyPI
requirements/pyproject/Pipfile/Poetry, Maven, Gradle, Composer, Bundler, Cargo,
NuGet, and local/image OS-package fixtures for apk, dpkg, and RPM queryformat
evidence. A separate synthetic monorepo/workspace fixture covers nested npm,
Yarn, pnpm, Go multi-module, Maven multi-module, Gradle multi-project, Cargo
workspace and renamed-package, .NET solution/project, Python
requirements/pyproject/Pipfile/Poetry, Composer, Bundler, and image/SBOM scope
cases. Separate synthetic cases cover malformed lockfile, unsupported
ecosystem, incomplete advisory, incomplete package, ambiguous multi-root
evidence, and stale-cache readiness states without private repositories or live
provider payloads.

The command runs in scoped mode by default. The CLI derives its scope plan
from the readiness envelope of `GET /api/v0/supply-chain/impact/findings` for
the scanned repository. Scoped mode adds one CLI-side fail-closed guard on
top of the server's classification: when the envelope's aggregate
`freshness` is `stale` or `unknown` and the server still returned a
`ready_*` state, scoped mode downgrades to `evidence_incomplete` and records
`advisory_cache_stale` or `advisory_cache_freshness_unknown` so the operator
never gets a clean answer backed by stale or unclassified source data.
Per-source entries in `readiness.source_snapshots[]` are surfaced in
`scope_plan.source_snapshots` for operator visibility. The readiness store
scopes source snapshots and durable source states to requested target anchors,
then the CLI gates on the aggregate `readiness.freshness` verdict so CLI, API,
and MCP callers share one scoped freshness contract.

The scope plan exposes:

- `observed_dependency_facts`: `evidence_sources[package.consumption].fact_count`
- `advisory_facts`: `evidence_sources[vulnerability.advisory].fact_count`
- `package_registry_facts`: `evidence_sources[package.registry].fact_count`;
  the readiness store emits this count for an explicit `package_id`, or for
  package metadata joined to the requested `repository_id` through owned
  package-consumption evidence.
- `package_registry_freshness`: `evidence_sources[package.registry].freshness`
  for the scoped package metadata, or `missing` when dependency facts require
  scoped package-registry metadata but no scoped registry facts are present.
- `package_registry_complete`: true only when scoped package-registry metadata
  exists and is fresh.
- `freshness`: `readiness.freshness`, the worst-of per-family aggregate.
- `stop_threshold`: the readiness state the CLI returned to the operator,
  either the server verdict or the scoped downgrade when applicable.
- `source_snapshots`: per-source cache state from the readiness envelope;
  individual rows are visible for diagnostics, while scoped fail-closed behavior
  is driven by `readiness.freshness`.

The `*_facts` fields are counts of source facts as reported by
`evidence_sources[].fact_count`, not counts of unique packages or advisory
sources. A single dependency or advisory observation can contribute multiple
facts.

Operators who explicitly want broader advisory coverage can pass `--broad`. In
broad mode the CLI surfaces `data.scope_mode = "broad"`, sets
`data.scope_plan.mode = "broad"`, records a warning that the advisory scoped
guard was skipped, and returns the server's advisory readiness verdict
unchanged. Broad mode still fails closed when required package-registry
metadata is stale or missing, and never converts stale package metadata into a
clean answer. The envelope freshness, package-registry freshness, and
source-snapshot diagnostics stay visible in the JSON envelope and the terminal
summary still prints `Scope: ... freshness=stale`.

Local performance evidence is attached as `data.scan_performance` on every
run: `started_at`, `completed_at`, `wall_time_ms`, `repository_size_bytes`,
`repository_file_count`, `observed_dependency_facts`, `advisory_facts`,
`package_registry_facts`, `cache_freshness`, `scope_mode`, and
`package_registry_freshness`, `package_registry_complete`, and
`stop_threshold`. Operators can compare scoped and broad runs of the same
repository to see how much advisory coverage the scoped guard trimmed, whether
package metadata was current, and where the scan stopped.

The scanner-style parent report lives at `data.report` in JSON mode. Its
schema version is `eshu.vulnerability_report.v1`, and it keeps the same
readiness envelope, target/package/image/SBOM context, manifest/source paths
with line anchors when the API provides them, affected-version fields, evidence
fact handles, missing-evidence reasons, unsupported-target coverage, and
remediation metadata separate. Provider payload fields are not copied into the
report.

`eshu vuln-scan repo --export sarif` now writes a SARIF v2.1.0 artifact from
that parent scanner envelope. Vulnerability findings carry package/image target
context, severity, remediation metadata, evidence fact ids, and real source
locations when present. Missing evidence and unsupported targets are preserved
as `eshu.*` SARIF properties, and non-ready states emit a location-free status
result so CI cannot mistake incomplete evidence for a clean scan.

`eshu vuln-scan repo --export vex` writes VEX-style JSON statements from the
same parent scanner envelope. The exporter maps only reducer-owned impact
statuses into statement statuses: `affected_exact` and `affected_derived` are
`affected`, `not_affected_known_fixed` is `not_affected`, and
`possibly_affected` or `unknown_impact` stay `under_investigation`. Readiness
states such as `evidence_incomplete`, `unsupported`, and
`readiness_unavailable` preserve missing evidence, unsupported targets, and
freshness in the document without creating `not_affected` statements. Use the
JSON scanner report instead of VEX when the caller needs the full readiness
envelope, source snapshots, scope counters, scan-performance block, or raw
reducer finding rows to decide whether Eshu had enough evidence to issue a
statement.

No-Regression Evidence: `go test ./cmd/eshu -run
'Test(VulnScanRepoFixtureCorpus|RunVulnScanRepoFixtureMatrix)' -count=1`
proves the local fixture corpus for `vuln-scan repo`: synthetic vulnerable
fixtures exit `3` with reducer-owned finding envelopes, collected fresh
ready-zero fixtures exit `0`, missing advisory evidence exits `4`, missing
package-registry evidence fails closed from a server `ready_zero_findings`
verdict to `evidence_incomplete` and exits `4`, unsupported Pub evidence exits
`5`, stale advisory cache evidence exits `4`, and malformed lockfile evidence
stays incomplete rather than clean. The same test runs JSON and terminal output
for every readiness class and asserts readiness, freshness, missing evidence,
unsupported targets, exit classification, and `scan_performance.wall_time_ms`.
`TestVulnScanRepoFixtureCorpusHasParserBackedDependencyEvidence` additionally
parses each non-OS supported-manager fixture through `internal/parser` so the
local corpus cannot pass with empty placeholder manifests.
`go test ./cmd/eshu -run
'TestRunVulnScanRepo(JSONReportPreservesScannerContractAndFindingsExit|JSONReportPreservesTargetPackageImageAndVersionContext|ExitCodesPreserveReadinessClasses|ScopedModeFailsClosedOnUnknownFreshness|TextSummaryRendersBeforeFindingsExit)|TestRenderVulnScanRepoSummaryIncludesReadinessEvidenceAndRemediation'
-count=1` proves the stable JSON report schema, target/package/image and
version-context mapping, evidence fact handles, remediation allowlist,
findings/non-ready/unsupported exit codes, terminal summary rendering before
scanner exit, and scoped fail-closed handling for unknown freshness.
`go test ./cmd/eshu -run 'TestRunVulnScanRepoVEXExport' -count=1` proves
VEX impact-status mapping, non-ready readiness preservation without
not-affected statements, evidence handles, remediation sanitization, and
private provider payload redaction.
`go test ./cmd/eshu -run
'TestRunVulnScanRepoWorkspaceScopeProof|TestVulnScanRepoWorkspaceScopeFixtureHasParserBackedDependencyEvidence'
-count=1` proves the synthetic monorepo/workspace scope fixture: nested
workspace positives keep manifest/source paths in the scanner report, ready-zero
rows stay clean for unrelated package roots, image/SBOM findings attach only to
the explicit subject/workload/service/environment hop, ambiguous multi-root
evidence remains `evidence_incomplete`, and every nested manifest/lockfile in
the fixture is parser-backed dependency evidence.
`go test ./cmd/eshu -run
'TestVulnScanRepoCommandRegistersBroadFlag|TestRunVulnScanRepoDefaultScopedModeAttachesScopePlanAndPerformance|TestRunVulnScanRepoFailsClosedOnStalePackageMetadata|TestRunVulnScanRepoScopedModeFailsClosedOnStaleAdvisoryCache|TestRunVulnScanRepoScopedModeIgnoresGlobalStaleSnapshotsWhenEnvelopeFresh|TestRunVulnScanRepoScopedModePassesThroughServerTargetIncomplete|TestRunVulnScanRepoBroadModeSkipsScopeGuards|TestRunVulnScanRepoScopedModeSurfacesEvidenceIncompleteWhenNoOwnedDeps'
-count=1` proves the `--broad` flag registration, default scoped scope-plan
and scan-performance attachment with scoped package-registry freshness,
package metadata fail-closed behavior, the envelope-freshness advisory
fail-closed guard, the no-regression that an unrelated globally-stale source
snapshot does not flip a fresh repo-only scan, server `target_incomplete`
pass-through, broad-mode pass-through with the advisory scoped guard
explicitly skipped, and scoped pass-through when the server already classifies
the response as `evidence_incomplete`. The full `go test ./cmd/eshu -count=1`
suite continues to pass with the updated findings-stub responses that mirror
the production readiness envelope.

No-Observability-Change: the scope plan, performance block, `--broad` flag,
report envelope, scanner exit-code mapping, and manifest/source path report
preservation are CLI-only orchestration over the existing
`/api/v0/supply-chain/impact/findings` readiness envelope and the existing
`query.supply_chain_impact_findings` span. No new HTTP route, MCP tool, metric
instrument, span, queue, reducer lane, graph write, or scanner worker is
introduced.

Performance Evidence: the focused CLI tests above run under 0.5s on Go
1.26.3 darwin/arm64 with the local authoritative-owner stubs and synthetic
readiness envelopes (`package.consumption.fact_count=4`,
`vulnerability.advisory.fact_count=120`, one fresh `osv/npm` source
snapshot). The scoped fail-closed path is exercised against an envelope
whose aggregate `freshness=stale`; the CLI downgrades to
`evidence_incomplete`, records `advisory_cache_stale` in
`scope_plan.missing_evidence`, and exits non-zero. The report-contract tests
also exercise aggregate `freshness=unknown`, which downgrades to
`evidence_incomplete`, records `advisory_cache_freshness_unknown`, and exits
non-zero. Repository size, file count, and wall-clock time on the live
`eshu vuln-scan repo` workflow are exposed through `data.scan_performance` for
operators to record per-environment ceilings; the focused tests pin wall-time
only as a non-negative integer because the stubbed scan clock advances one
second per call.
