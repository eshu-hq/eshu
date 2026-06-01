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
  `pyproject.toml` / `Pipfile` / `Pipfile.lock` / `poetry.lock`, and Swift
  Package Manager `Package.resolved` produce repository consumption decisions
  when joined to package-registry identity. Swift rows are exact lockfile-only
  evidence from remote source-control pins; branch, revision-only, local, and
  path pins stay out of precise vulnerability impact. Yarn and pnpm lockfile
  rows keep the canonical npm ecosystem
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

No-Regression Evidence: remote Docker Compose JVM vulnerability parity for
issue `#1065` was rerun after rebasing to `origin/main` `e7f71b75` with a
clean-volume, bounded JVM slice of 3 repositories. The slice emitted 118 Maven
dependency rows and 3 Gradle dependency rows, all classified as resolved; the
Maven package-registry/advisory join produced 1 Maven package-consumption
correlation after the reducer started requesting Maven `groupId:artifactId`
manifest-coordinate candidates. Reducer-owned impact facts materialized as
4 `affected_exact` / `maven_range_match`, 16
`not_affected_known_fixed` / `maven_known_fixed`, and 8
`possibly_affected` / `version_not_in_advisory_range` rows. The scoped
Log4Shell API and MCP-mounted HTTP read both returned
`ready_with_findings`, `fresh`, `truncated=false`, and 2
`not_affected_known_fixed` findings with `match_reason=maven_known_fixed`.
Provider-alert reconciliation was present as 18 aggregate `provider_only/open`
rows for this slice. No private repository names, private package names,
advisory payloads, alert URLs, hostnames, keys, or machine-local paths were
recorded.

Observability Evidence: the same `#1065` run used
`scripts/verify_remote_e2e_runtime_state.sh` in representative mode and
reported healthy API, MCP, ingester, projector, resolution-engine,
workflow-coordinator, hosted collector, and scanner-worker services. The
terminal scoped queue counts were `outstanding=0`, `in_flight=0`,
`pending=0`, `retrying=0`, `failed=0`, `dead_letter=0`,
`reducer_converging=0`, `pending_completeness=0`, and
`blocked_completeness=0`. Fact work items were `209 succeeded`; workflow
work items included scheduled AWS follow-up rows but no retry, failed, or
dead-letter states. Filtered NornicDB logs for `UNWIND MERGE`, `SQLSTATE`,
constraint, panic, fatal, and OOM patterns returned 0 matching lines. The
path reuses existing parser-stage timing, package-consumption correlation
facts, reducer supply-chain impact facts, readiness envelopes,
`query.supply_chain_impact_findings`, runtime health routes, and
remote-E2E verifier status output; no new metric, span, queue, worker, route,
or graph-write shape was introduced.

No-Regression Evidence: Swift Package Manager impact support is guarded by
`go test ./internal/parser ./internal/parser/json -run 'Swift|PackageResolved|DependencyCoverage' -count=1`,
`go test ./internal/collector/vulnerabilityintelligence ./internal/collector/vulnerabilityintelligence/vulnruntime -run 'SwiftURL|SwiftOSV|MapsSwiftOSV' -count=1`,
and `go test ./internal/reducer ./internal/query -run 'Swift|SupplyChainImpactFindingQueryUsesDetectionProfileFilter|DecodeSupplyChainImpactFindingRowBackfillsLegacyPreciseProfile|AggregateQueriesUseListProfileAndSuppressionPredicates' -count=1`.
The fixtures prove exact `Package.resolved` dependency evidence, OSV
`SwiftURL` advisory normalization from source Git URLs, Swift
semver range matching, precise read-model profile behavior, and fail-closed
behavior for unsupported Swift dependency shapes without adding a new queue,
graph write, or runtime knob.

No-Observability-Change: Swift impact uses existing parser dependency facts,
package-consumption correlation facts, `reducer_supply_chain_impact_finding`,
`match_reason`, `missing_evidence`, source snapshot/readiness facts, and the
existing `query.supply_chain_impact_findings` read span. It adds no metric
instrument, span, log key, queue, reducer lane, graph write, scanner worker,
route, MCP tool, or runtime configuration knob.

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
  `chainguard`, `oracle`, `rocky`, `alma`, `centos`), the matching vendor
  advisory outranks GLAD, GHSA, OSV, and NVD because vendor backports change
  applicability. RPM-family installed package evidence must include distro,
  distro version, arch, repository class, and a matching
  `vendor_advisory_source`; third-party or ambiguous vendor-origin rows remain
  source warnings and do not join as vendor-applicable impact truth.
- For language ecosystems (npm, PyPI, Go, Maven, Crates.io, RubyGems,
  Composer, Pub, Hex, Swift, NuGet), GHSA outranks GLAD, OSV-via-OSV,
  PYSEC-via-OSV, RUSTSEC-via-OSV, GO-via-OSV, and NVD.
  Impact-supported version matchers are npm, PyPI, Cargo, Swift, NuGet, and
  Maven; vendor-backed RPM-family OS package facts use the OS package priority
  above. Go, RubyGems, Composer, Pub, Hex, and other matcher-unimplemented
  ecosystems remain source-only, missing, or unsupported evidence until package
  identity, dependency evidence, version matching, advisory ingestion, and
  readback are proven end to end.
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
matchers are npm, Go modules, Cargo, and Swift semver over OSV-style event
ranges and GLAD-style comparator ranges, NuGet semantic versions from exact
lockfile or pinned manifest evidence, Maven version/range ordering for Maven
bracket and comparator ranges, PyPI PEP 440, RPM EVR ordering for
vendor-backed RPM-family OS package facts, and RubyGems installed versions from
Bundler lockfile evidence. Swift support is exact `Package.resolved` evidence
only; OSV `SwiftURL` records use a Git URL as the source package name; Eshu
normalizes that URL to the shared Swift Package Manager identity and can also
use a `PACKAGE` reference as a fallback for source records that only publish a
short package name. RubyGems matching uses the same reducer-owned exact-version
gate as other precise lockfile ecosystems: git/path Bundler dependencies remain
ambiguous source evidence and are not admitted as public RubyGems registry
versions. Findings preserve `observed_version`, `requested_range`,
`fixed_version`, and `match_reason` as separate fields. Unsupported non-OS
package ecosystems do not produce impact findings; readiness surfaces them as
`unsupported_targets[]` with `reason=unsupported_ecosystem` when the bounded
scope has observed dependency evidence. Malformed installed versions or
advisory ranges still fail closed as
`possibly_affected` with explicit missing-evidence reasons instead of being
treated as affected or safely fixed.

No-Regression Evidence: `go test ./internal/collector/ospackagevulnerability
-run 'TestParseRPM|TestBuildEnvelopesEmitsOSPackage' -count=1` and `go test
./internal/reducer -run
'TestBuildSupplyChainImpactFindings(UsesVendorRPMOSPackageEvidence|SkipsAmbiguousRPMOSPackageEvidence)'
-count=1` prove RPM queryformat package evidence preserves EVR, distro,
repository, PURL/BOMRef identity, and joins Red Hat advisories only for
vendor-class RPM rows with matching distro/version/vendor source evidence.

No-Observability-Change: the RPM parser emits existing `vulnerability.warning`
facts for raw rpmdb bytes, third-party repositories, unknown repositories, and
ambiguous vendor origin, and the reducer uses existing supply-chain impact
payload fields, evidence paths, reducer run spans, reducer execution counters,
and API/MCP readiness envelopes. No new metric label, graph write, queue
domain, route, or runtime knob was added.

No-Regression Evidence: Ruby/Bundler vulnerability parity for issue `#1013`
is guarded by `go test ./internal/parser/ruby -run 'TestParseGemfile' -count=1`
and `go test ./internal/reducer -run
'TestBuildSupplyChainImpactFindings.*RubyGems|TestBuildPackageConsumptionDecisions.*RubyGems'
-count=1`. These fixtures prove Gemfile manifest scope, Gemfile.lock exact
versions, dependency-chain propagation, missing-chain non-guessing, git/path
source ambiguity rejection, affected exact-version findings, known-fixed
classification, and four-segment RubyGems version ordering without Postgres,
graph, queue, scanner-worker, or hosted-runtime work.

No-Observability-Change: the Ruby/Bundler parity change reuses existing
parser dependency rows, package-consumption correlation facts,
`reducer_supply_chain_impact_finding`, `match_reason`, `dependency_scope`,
`dependency_path`, `missing_evidence`, and the supply-chain impact API/MCP
readiness envelope. It adds no metric instrument, span, log key, queue,
reducer lane, graph write, scanner worker, route, MCP tool, or runtime
configuration knob.

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
ecosystem fail-closed behavior with no impact finding, GLAD not-equal range
matching, malformed installed-version and advisory-range reasons, impact fact
serialization, impact read-model decoding, API result shaping, and MCP
pass-through for the supply-chain impact envelope. The matcher is bounded to
the active
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
- expose exact, derived, possible, known-fixed, unknown impact statuses and
  unsupported readiness states without collapsing them into one severity bucket.

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

The scope plan exposes:

- `observed_dependency_facts`: `evidence_sources[package.consumption].fact_count`
- `advisory_facts`: `evidence_sources[vulnerability.advisory].fact_count`
- `package_registry_facts`: `evidence_sources[package.registry].fact_count`;
  the readiness store emits this count for an explicit `package_id`, or for
  package metadata joined to the requested `repository_id` through owned
  package-consumption evidence.
- `package_registry_freshness`: `evidence_sources[package.registry].freshness`
  for the scoped package metadata.
- `package_registry_complete`: true only when scoped package-registry metadata
  exists and is fresh.
- `freshness`: `readiness.freshness`, the worst-of per-family aggregate.
- `stop_threshold`: the readiness state the CLI returned to the operator,
  either the server verdict or the scoped downgrade when applicable.
- `source_snapshots`: diagnostic-only per-source cache state from the
  readiness envelope, not gated on.

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
readiness envelope, target/package/image/SBOM context, affected-version fields,
evidence fact handles, missing-evidence reasons, unsupported-target coverage,
and remediation metadata separate. Provider payload fields are not copied into
the report.

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
