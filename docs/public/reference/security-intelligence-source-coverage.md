# Security Intelligence Source Coverage

This page extends [Security Intelligence](security-intelligence.md) with
dependency evidence, advisory-source provenance, provider-alert parity, API/MCP
contracts, CLI behavior, and acceptance gates.

## Repository Dependency Coverage

Vulnerability impact requires repository dependency evidence. The full coverage
matrix of which package-manager manifests and lockfiles produce
`content_entity` dependency facts today, which are still gaps, and the
safety rule that missing evidence is neither safe nor affected, lives in
[Dependency And Lockfile Coverage](dependency-coverage.md). That page is
generated from `go/internal/parser/json/dependency_coverage.go`; the matrix
test guards stop a parser change from drifting away from the documented
contract.

For the supply-chain impact reducer, the practical implications are:

- npm `package.json` and `package-lock.json`, Yarn classic and Yarn Berry
  `yarn.lock`, pnpm `pnpm-lock.yaml` (v6+), PHP Composer `composer.json`
  and `composer.lock`, Ruby Bundler `Gemfile` / `Gemfile.lock`, NuGet
  `.csproj` PackageReference and `packages.lock.json`, Rust Cargo
  `Cargo.toml` and `Cargo.lock`, Go `go.mod`, Maven `pom.xml`, Gradle
  `build.gradle` / `build.gradle.kts`, and PyPI `requirements.txt` /
  `pyproject.toml` / `Pipfile` / `Pipfile.lock` / `poetry.lock` produce
  repository consumption decisions when joined to package-registry
  identity. Yarn and pnpm lockfile rows keep the canonical npm ecosystem
  (`package_manager: "npm"`) so the consumption reducer and the
  owned-package SQL filter both match them as npm evidence, and they
  surface the actual package manager tool as `package_manager_flavor:
  "yarn"` or `"pnpm"` for operators and readiness reads. package.json
  rows preserve runtime/dev/optional/peer manifest scope but remain
  range-only evidence until paired with exact lockfile or SBOM evidence.
  package-lock rows carry exact installed versions plus npm-recorded
  runtime/dev/optional/peer scope. pnpm lockfiles preserve importer-side
  runtime/dev/optional/peer scope. Yarn lockfiles preserve exact versions
  and dependency chains but do not carry importer scope on their own, so
  Yarn runtime/dev/optional/peer scope needs paired manifest evidence.
  Unsupported Yarn Berry non-npm protocols such as `patch:` remain
  audit-only `unsupported_dependency` rows and are not admitted as
  precise consumption truth. Composer
  lockfile rows carry exact installed versions and a `lockfile: true`
  flag, so the reducer reports `direct_dependency: null` rather than
  guessing directness when no manifest range was also observed. Bundler
  git/path sources are preserved as ambiguous source evidence and are
  not admitted as public RubyGems registry consumption. NuGet lockfile
  rows carry exact resolved versions plus dependency path/directness
  when the lockfile proves the chain, while `.csproj` rows preserve
  requested versions, MSBuild property partial evidence, and
  PrivateAssets dev/test signals. Go `go.sum` remains checksum-only
  evidence and does not by itself prove the currently selected module
  version, so the consumption reducer treats it as missing evidence
  until paired with a `go.mod` require. JVM coverage emits
  `groupId:artifactId` dependency rows from Maven and Gradle manifests
  so impact reads no longer fall back to registry-side evidence alone.
  PyPI identity is normalized via PEP 503 so requirements that use mixed
  case, underscores, or dots still join with PyPI advisory ecosystems.
- Cargo manifests preserve direct dependency ranges, dev/build/runtime
  scope, workspace-inherited dependency rows, target-specific dependency
  sections, and renamed package identity. Cargo lockfiles preserve exact
  crate versions and dependency paths only when the lockfile root graph
  proves reachability.
- Files that remain in the gap state, such as `go.sum`, have no
  repository-side dependency parser yet, so their impact reads must
  surface the missing-evidence reason instead of returning
  `ready_zero_findings`.
- VCS, path, URL, and editable dependency entries, including pip
  `-e ./path`, `git+...` URLs, Poetry `{ path = ... }` / `{ git = ... }`,
  and Pipenv `{git=...}`, surface with a non-`dependency` `config_kind`
  (`vcs_dependency`, `path_dependency`, `url_dependency`,
  `editable_dependency`) so the reducer cannot mis-admit unresolved
  provenance as a PyPI registry version.
- When a parser graduates a file from gap to covered, the matrix MUST be
  updated in the same PR, the covered-fixture guard MUST grow a row, and a
  reducer test MUST prove the new evidence path can produce a consumption
  decision.

No-Regression Evidence: Cargo dependency coverage is guarded by
`go test ./internal/parser -run 'TestCargoDependencyCoverageMatrixMarksCargoFilesCovered|TestDefaultEngineParsePathCargo' -count=1`,
`go test ./internal/parser/json -run 'TestDependencyCoverageMatrixIsStableAndExhaustive|TestDependencyCoverageCoveredFilesEmitDependencyRows' -count=1`,
and `go test ./internal/reducer -run 'TestBuildPackageConsumptionDecisions(MatchesCargoRenamedPackage|KeepsCargoLockfileWithoutProofUnchained)|TestPackageCorrelationWriterPersistsCargoLockfileEvidence|TestBuildSupplyChainImpactFindings(UsesCargoLockfileVersion|MarksCargoLockfileVersionKnownFixed|KeepsCargoManifestVersionRangeOnly)' -count=1`.
The fixtures prove parser evidence, coverage-matrix truth, renamed Cargo
package correlation, unproven lockfile-chain suppression, lockfile exact-version
handoff, Cargo.toml range-only impact classification, and exact lockfile-version
impact matching without graph, queue, or hosted runtime work.

No-Observability-Change: Cargo coverage reuses existing parser payloads,
`content_entity` dependency facts, package-consumption correlation facts,
`reducer_supply_chain_impact_finding`, `match_reason`, and the existing
`query.supply_chain_impact_findings` read span. It adds no metric instrument,
span, log key, queue, reducer lane, graph write, scanner worker, or runtime
configuration knob.

No-Regression Evidence: JavaScript/TypeScript vulnerability parity for issue
`#997` is guarded by `go test ./internal/parser/json -run
'TestParsePackageJSONEmitsRuntimeDevOptionalAndPeerScopes|TestParsePackageLockEmitsExactVersionScopeFlags'
-count=1`, `go test ./internal/parser/nodelockfile -run
'TestParseUnsupportedYarnBerryFeatureIsRecorded|TestParsePnpmLockfilePreservesOptionalAndPeerImporterScopes'
-count=1`, and `go test ./internal/reducer -run
'TestBuildPackageConsumptionDecisionsRejectsUnsupportedYarnBerryFeature|TestBuildSupplyChainImpactFindingsProvesNPMFamilyLockfileExactVersions|TestBuildSupplyChainImpactFindingsKeepsNPMDevScopeVisible'
-count=1`. Provider-alert comparison remains a private validation path:
compare sanitized aggregate counts and row-level mismatch classes against
representative Dependabot alerts without committing provider payloads,
repository names, package names, or alert URLs.

No-Observability-Change: the `#997` parity work changes parser row metadata
and reducer admission filters only. It reuses existing `content_entity`
dependency facts, package-consumption correlation facts,
`reducer_supply_chain_impact_finding`, `match_reason`, `dependency_scope`,
`missing_evidence`, and `query.supply_chain_impact_findings`. It adds no
metric instrument, span, log key, queue, reducer lane, graph write, scanner
worker, route, MCP tool, or runtime configuration knob.

## Advisory Source Coverage

Eshu collects advisory source truth from OSV, FIRST EPSS, CISA KEV, NVD CVE
API 2.0, and the GitLab Advisory Database (Gemnasium). Each source is
normalized into the shared `vulnerability.*` fact contract with
`source_confidence=reported` and a source-namespaced stable fact key, so a
GLAD observation of `CVE-2026-0001` coexists with OSV, GHSA, or NVD
observations of the same CVE rather than overwriting them. Reducers join
across sources at admission time and may detect cross-source disagreement on
range, severity, or fixed version.

The GLAD adapter preserves the source `package_slug`, ecosystem, package
name, normalized package ID, PURL, raw and structured `affected_range`,
human-readable `affected_versions`
and `not_impacted` text, multiple `fixed_versions` (including prerelease and
`+build` branches), CVSS v2/v3/v4 vectors, CWE IDs, URLs, and the source
advisory UUID. Range evaluation belongs to reducers.

The GLAD adapter is parser-only. Cache and freshness lifecycle for advisory
snapshots is owned by the shared source interface in
[#603](https://github.com/eshu-hq/eshu/issues/603); the GLAD parser is pure
so the cache/freshness owner can wire it later without changing the fact
payload.

No-Regression Evidence: `go test ./internal/collector/vulnerabilityintelligence -run 'TestGitLab|TestParseGitLab|TestNewGitLab' -count=1`
covers GLAD CVE/affected_package/reference envelope construction, GMS
identifier fallback, advisory-identifier validation, package_slug validation,
source-namespaced stable keys against OSV, compact multi-branch range
parsing, prerelease and `+build` fixed-version preservation, blank/empty
constraint rejection, unsupported-operator rejection, source snapshot
generation invariance, GLAD to OSV fixed-version disagreement,
GLAD to NVD CVSS severity disagreement, GLAD to OSV affected-range
disagreement, and shared CVE correlation anchors for cross-source joins.

## Advisory Provenance

Reducer admission consolidates multi-source advisory observations into one
finding per `(cve_id, package_id)` anchor while preserving per-source
provenance. The selected severity, fixed-version, and vulnerable-range source
are recorded alongside every alternate severity and every source-reported
fixed-version branch. Withdrawn advisories never win selection but remain
visible as observations so operators can see why a source was excluded.

Selection rules:

- For OS package classes (`rpm`, `deb`, `apk`, `rhel`, `redhat`, `debian`,
  `ubuntu`, `alpine`, `amazonlinux`, `suse`, `opensuse`, `wolfi`,
  `chainguard`, `oracle`, `rocky`), the matching vendor advisory outranks
  GLAD, GHSA, OSV, and NVD because vendor backports change applicability.
- For language ecosystems (npm, PyPI, Go, Maven, Crates.io, RubyGems,
  Composer, Pub, Hex, Swift, NuGet), GHSA outranks GLAD, OSV-via-OSV,
  PYSEC-via-OSV, RUSTSEC-via-OSV, GO-via-OSV, and NVD.
- An OSV record whose advisory id begins with `GHSA-`, `PYSEC-`, `GO-`,
  `RUSTSEC-`, or `MAL-` is classified by that upstream prefix, so a GHSA
  collected through OSV still ranks as a GHSA observation.
- If the highest-priority source did not publish a CVSS score, the next
  source in priority order supplies the selected severity.

The `provenance` block on `GET /api/v0/supply-chain/impact/findings` and the
MCP `list_supply_chain_impact_findings` tool carries:
`selected_severity_source`, `selected_severity_score`,
`selected_severity_vector`, `selected_severity_label`,
`selected_fixed_version_source`, `selected_range_source`,
`alternate_severities[]`, `fixed_version_branches[]`, and
`advisory_sources[]` (with `source`, `advisory_id`, `source_updated_at`, and
`withdrawn_at`). Raw advisory bodies are not returned.

Source-only advisory evidence is exposed separately through
`GET /api/v0/supply-chain/advisories/evidence` and the MCP
`list_advisory_evidence` tool. That route groups active GHSA, CVE/NVD, OSV,
GitLab Advisory Database, FIRST EPSS, CISA KEV, CWE, affected-package,
affected-product/CPE, range, fixed-version, withdrawn, reference, and
source-disagreement evidence under one canonical advisory identity without
publishing a supply-chain impact finding or implying repository, image,
workload, deployment, or reachability impact.

`GET /api/v0/supply-chain/impact/explain` and the MCP
`explain_supply_chain_impact` tool use the same reducer-owned finding facts
but return one explanation at a time. Callers must provide `finding_id` or an
advisory/CVE plus package, repository, or image digest anchor. The route
hydrates only the finding's `evidence_fact_ids`, returns advisory/source,
component/version, vulnerable-range, fixed-version, dependency-chain,
manifest/SBOM/image/workload/service/environment/provider-alert anchors when
those facts exist, includes an `impact_path` with present and missing hops, and
reports `outcome: no_finding` with readiness when a bounded scope has no
finding. It does not infer reachability or deployment truth from provider
alerts, image tags, workload names, service names, environment names, or
repository names.

Version and range matching is reducer-owned and ecosystem-aware. The supported
matchers are npm and Cargo semver over OSV-style event ranges and GLAD-style
comparator ranges, NuGet semantic versions from exact lockfile or pinned
manifest evidence, plus Maven version/range ordering for Maven bracket and
comparator ranges. Findings preserve `observed_version`, `requested_range`,
`fixed_version`, and `match_reason` as separate fields. Unsupported ecosystems
and malformed installed versions or advisory ranges fail closed as
`possibly_affected` with explicit missing-evidence reasons instead of being
treated as affected or safely fixed.

No-Regression Evidence: after rebasing PR #638 onto `origin/main`
`1afcc154`, focused red tests reproduced the review gaps where
`package_manifest` findings reported missing image/SBOM evidence and
`impact_path` included explanation-only gaps such as `fixed_version`. After
the fix, `go test ./internal/reducer -run
'TestBuildSupplyChainImpactFindingsUsesOwnedLockfileVersion' -count=1` and
`go test ./internal/query -run
'TestBuildSupplyChainImpactExplanationReturnsRuntimePathAndMissingHops'
-count=1` passed on Go 1.26.3 darwin/arm64. The input shapes are in-memory
fixtures: one advisory/affected-package/package-consumption path for the
package-manifest guard, and one finding plus two runtime evidence facts for
the explain path. No graph backend, reducer queue rows, or scanner worker rows
are involved, and `go test ./internal/reducer ./internal/query
./internal/storage/postgres ./internal/mcp -count=1` plus `go test ./...`
passed after the rebase.

No-Observability-Change: the review fix only changes missing-evidence
classification and API response shaping. Operators continue to diagnose the
path with the existing `reducer_supply_chain_impact_finding` payload,
`EvidencePath`, `missing_evidence`, `runtime_reachability`,
`query.supply_chain_impact_findings`, `query.supply_chain_impact_explanation`,
and Postgres query instrumentation.

No-Regression Evidence: `go test ./internal/reducer
./internal/query ./internal/collector/vulnerabilityintelligence
-run 'TestSupplyChainImpact(Preserves|VendorAdvisory|FallsBack|Excludes)|TestPostgresSupplyChainImpactWriterSerializesProvenancePayload|TestDecodeSupplyChainImpactFindingRowPreservesProvenance|TestOSVRecordPreservesWithdrawnTimestamp'
-count=1` proves GHSA-vs-NVD severity provenance preservation, vendor
advisory override for OS package classes, severity fallback when the
highest-priority source lacks a CVSS score, withdrawn-advisory exclusion
with the withdrawal timestamp still surfaced, multiple fixed-version
branches preserved with originating source, payload serialization, query
decoding, and OSV `withdrawn_at` capture.

No-Observability-Change: the provenance work reuses the existing
`query.supply_chain_impact_findings` span, the
`reducer_supply_chain_impact_finding` fact kind, and the
`SupplyChainImpactFindings` reducer counter. No new metric instrument,
span, log key, queue, reducer lane, graph write, or runtime worker is
introduced. Operators continue to use the supply-chain impact API truth
envelope, the existing reducer outcome counters, and the
`vulnerability.cve` / `vulnerability.affected_package` source-fact payloads
to diagnose source coverage.

No-Regression Evidence: `go test ./internal/reducer ./internal/query
./internal/mcp -count=1` covers npm semver affected ranges, Maven vulnerable
ranges, Maven known-fixed classification, range-only manifests, unsupported
ecosystem fail-closed behavior, GLAD not-equal range matching, malformed
installed-version and advisory-range reasons, impact fact serialization, impact
read-model decoding, API result shaping, and MCP pass-through for the
supply-chain impact envelope. The matcher is bounded to the active
`(cve_id, package_id)` affected-package rows already loaded by the impact
reducer plus the owned dependency/SBOM evidence for that package; it does not
scan the public package universe.

No-Observability-Change: the version-matching boundary reuses the existing
`SupplyChainImpactFindings` reducer counter,
`reducer_supply_chain_impact_finding` fact kind, impact `EvidencePath`,
`missing_evidence`, `match_reason`, and the
`query.supply_chain_impact_findings` span. Operators diagnose decisions from
the same impact finding payload and readiness envelope; no new queue,
collector, graph write, metric instrument, or runtime worker is introduced.

No-Observability-Change: the GLAD adapter emits the existing
`vulnerability.cve`, `vulnerability.affected_package`,
`vulnerability.reference`, and `vulnerability.source_snapshot` fact kinds. It
adds no new metric instrument, span, log key, queue, reducer lane, graph
write, or runtime worker. Operators continue to use the existing
`vulnerability.source_snapshot` `source`/`ecosystem`/`response_digest`/
`complete` fields and the readiness envelope on the supply-chain impact API
to diagnose coverage.

## Provider Alert Parity Gate

Provider-hosted alert parity is a validation gate, not a source of public test
data. For supported hosts, private validation may compare Eshu findings against
provider alerts for the same repositories and package ecosystems.

`eshu-collector-security-alerts` is the hosted provider security-alert runtime.
It is claim-driven and currently supports GitHub Dependabot repository alerts
behind explicit credentials and repository allowlists. Targets name a
`token_env` rather than a token value, and every repository must appear in the
target's `allowed_repositories` list before the runtime issues an HTTP request.
Any `api_base_url` override must use HTTPS because the runtime sends the bearer
token to that endpoint. Collection is bounded by `repository_alert_limit` and
`max_pages`, requests GitHub's open-alert view directly, and surfaces
`source_freshness=partial` plus a coverage summary when the open-alert provider
page cap is reached. Provider rate-limit responses are surfaced as retryable
workflow failures.

`security_alert.repository_alert` facts preserve repository-scoped provider
alert state from GitHub Dependabot. The runtime emits only that source fact
kind; provider alerts do not become canonical Eshu impact findings by
themselves. The `security_alert_reconciliation` reducer writes comparison rows
with provider state and Eshu impact state as separate fields:

- `matched` when the alert joins to owned dependency evidence and an Eshu
  impact finding for the same package/advisory.
- `unmatched` when the dependency is owned but no Eshu impact finding exists.
- `stale` when newer owned dependency evidence no longer matches the alert's
  manifest path.
- `dismissed` or `fixed` when the provider reports that state.
- `provider_only` when Eshu has no owned dependency evidence for the alert.

Provider alert reconciliation reads require a repository, provider, package,
CVE, or GHSA anchor. Provider state and reconciliation status only filter an
anchored page; they are not standalone scopes.
List and count responses include a `coverage` object. `state=target_incomplete`
means at least one returned or counted reconciliation came from a truncated
open-alert provider read, so callers must not treat the count as complete.

Eshu should match provider alert counts when it has equivalent owned target
evidence and advisory data. Eshu may exceed provider alert output when it can
add code-to-cloud context, image/runtime impact, or additional advisory sources.
Any mismatch must classify whether the cause is missing target collection,
missing advisory ingestion, version-range matching, unsupported ecosystem,
provider-only behavior, or an Eshu reducer bug.

No-Regression Evidence: `go test ./internal/collector/securityalerts ./internal/collector/securityalerts/alertruntime -count=1`
proves the provider client requests the `state=open` view, preserves cursor
pagination bounds, and marks truncated open-alert reads as partial source
freshness. `go test ./internal/reducer -run
'TestBuildSecurityAlertReconciliations|TestSecurityAlertReconciliationWriterUsesProviderAlertScopeForPackageTriggeredRepair'
-count=1` proves source freshness and collection coverage survive reducer
reconciliation payload publication. `go test ./internal/query -run
'TestSupplyChainListSecurityAlertReconciliations|TestDecodeSecurityAlertReconciliationRowPreservesProviderCoverage|TestSecurityAlertReconciliationAggregate'
-count=1` proves API/MCP-backed list and count responses expose partial
coverage without unbounded page reads.

No-Observability-Change: the fix reuses existing security-alert provider
request counters, fact-emitted counters, fetch-duration histograms,
`security_alert.observe`, `security_alert.fetch`, and the API/MCP response
envelope. No repository name, package name, alert URL, token environment name,
or token value is added to metric labels, status errors, or public docs.

Validation logs may record aggregate counts and mismatch classes. They must not
commit private repository names, package names, alert URLs, or copied provider
payloads to the public repo. Runtime metrics and spans use bounded provider,
status-class, and fact-kind labels; credentials are resolved from environment
variables and must not appear in facts, logs, metric labels, status errors, or
checked-in examples.

## API And MCP Contract

Security reads must be bounded, explainable, and scoped:

- require `limit`, timeout, deterministic ordering, and `truncated` signals for
  list responses;
- require at least one anchor such as repository, package, image digest,
  advisory id, service, workload, environment, or status;
- keep findings separate from readiness and source facts;
- keep provider alert state separate from Eshu impact state;
- return evidence handles and missing-evidence reasons instead of raw full
  source payloads;
- expose exact, derived, possible, known-fixed, unknown, and unsupported states
  without collapsing them into one severity bucket.

The current vulnerability impact route is documented in
[HTTP Evidence And Supply-Chain Routes](http-api/evidence-and-supply-chain.md).

## CLI Contract

The local vulnerability scan command is a thin orchestration layer, not a
second scanner product:

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
and fixture-backed vulnerable/ready-zero runtime proof remain gates before this
is a complete standalone vulnerability scan workflow.

The command runs in scoped mode by default. The CLI derives its scope plan
from the readiness envelope of `GET /api/v0/supply-chain/impact/findings` for
the scanned repository. Scoped mode adds one CLI-side fail-closed guard on
top of the server's classification: when the envelope's aggregate
`freshness` is `stale` or `unknown` and the server still returned a
`ready_*` state, scoped mode downgrades to `evidence_incomplete` and records
`advisory_cache_stale` or `advisory_cache_freshness_unknown` so the operator
never gets a clean answer backed by stale or unclassified source data.
Per-source entries in `readiness.source_snapshots[]` are surfaced in
`scope_plan.source_snapshots` for operator visibility only. The readiness store
aggregates those snapshots globally rather than by requested scope, so they do
not gate scoped fail-closed behavior.

The scope plan exposes observed dependency, advisory, and package-registry fact
counts from `evidence_sources[].fact_count`, the aggregate freshness, the stop
threshold, and diagnostic-only source snapshots. `package_registry_facts` is
usually `0` for repository-scoped runs because registry metadata is counted only
when the request anchors on a specific `package_id`.

Operators who explicitly want broader advisory or package coverage can pass
`--broad`. In broad mode the CLI surfaces `data.scope_mode = "broad"`, sets
`data.scope_plan.mode = "broad"`, records a warning that scoped fail-closed
guards were skipped, and returns the server's readiness verdict unchanged.
Broad mode never converts a stale cache into a clean answer: the envelope
freshness and source-snapshot diagnostics stay visible in the JSON envelope
and the terminal summary still prints `Scope: ... freshness=stale`.

Local performance evidence is attached as `data.scan_performance` on every
run: `started_at`, `completed_at`, `wall_time_ms`, `repository_size_bytes`,
`repository_file_count`, `observed_dependency_facts`, `advisory_facts`,
`package_registry_facts`, `cache_freshness`, `scope_mode`, and
`stop_threshold`. Operators can compare scoped and broad runs of the same
repository to see how much advisory coverage the scoped guard trimmed and
where the scan stopped.

The scanner-style parent report lives at `data.report` in JSON mode. Its
schema version is `eshu.vulnerability_report.v1`, and it keeps the same
readiness envelope, target/package/image/SBOM context, affected-version fields,
evidence fact handles, missing-evidence reasons, unsupported-target coverage,
and remediation metadata separate. Provider payload fields are not copied into
the report. SARIF and VEX-style statements remain separate export formats
tracked outside this parent envelope.

No-Regression Evidence: `go test ./cmd/eshu -run
'TestRunVulnScanRepo(JSONReportPreservesScannerContractAndFindingsExit|JSONReportPreservesTargetPackageImageAndVersionContext|ExitCodesPreserveReadinessClasses|ScopedModeFailsClosedOnUnknownFreshness|TextSummaryRendersBeforeFindingsExit)|TestRenderVulnScanRepoSummaryIncludesReadinessEvidenceAndRemediation'
-count=1` proves the stable JSON report schema, target/package/image and
version-context mapping, evidence fact handles, remediation allowlist,
findings/non-ready/unsupported exit codes, terminal summary rendering before
scanner exit, and scoped fail-closed handling for unknown freshness.
`go test ./cmd/eshu -run
'TestVulnScanRepoCommandRegistersBroadFlag|TestRunVulnScanRepoDefaultScopedModeAttachesScopePlanAndPerformance|TestRunVulnScanRepoScopedModeFailsClosedOnStaleAdvisoryCache|TestRunVulnScanRepoScopedModeIgnoresGlobalStaleSnapshotsWhenEnvelopeFresh|TestRunVulnScanRepoScopedModePassesThroughServerTargetIncomplete|TestRunVulnScanRepoBroadModeSkipsScopeGuards|TestRunVulnScanRepoScopedModeSurfacesEvidenceIncompleteWhenNoOwnedDeps'
-count=1` continues to prove the `--broad` flag, scope plan, performance
block, stale-freshness guard, source-snapshot no-regression, server
`target_incomplete` pass-through, broad mode, and server-classified
`evidence_incomplete` pass-through. The full `go test ./cmd/eshu -count=1`
suite continues to pass with findings stubs that mirror the production
readiness envelope.

No-Observability-Change: the scope plan, performance block, and
`--broad` flag, report envelope, and scanner exit-code mapping are CLI-only
orchestration over the existing `/api/v0/supply-chain/impact/findings`
readiness envelope and the existing `query.supply_chain_impact_findings`
span. No new HTTP route, MCP tool, metric instrument, span, queue, reducer
lane, graph write, or scanner worker is introduced.

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

## Acceptance Gates

Security intelligence work is ready only when applicable source-fact,
reducer-readiness, API/MCP scope, private provider-parity, remote Compose,
Kubernetes, and performance gates pass. The release-cut gate that ties these
proofs together is [Security Intelligence Release Gate](security-intelligence-release-gate.md).
`scripts/security_intelligence_release_gate.sh` aggregates commit, image,
backend, schema, fixture, API readback, remote Compose, and rollout snapshots
into one evidence document before an image cut is accepted.
