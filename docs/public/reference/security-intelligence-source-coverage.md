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
  path pins stay out of precise vulnerability impact.
  Hex `mix.exs` and `mix.lock` also produce repository consumption decisions
  for literal registry dependencies, including `hex:` package overrides and
  private organization namespaces from Mix manifest entries, while Mix git
  dependencies stay provenance-only and fail closed. These dependency rows are
  admitted only when joined to package-registry identity. Yarn and pnpm lockfile
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
  provenance as a PyPI registry version. Supply-chain readiness reports these
  rows as `dependency_source` unsupported target evidence with stable reason
  codes such as `vcs_dependency_unsupported` or `path_dependency_unsupported`,
  so a repository with only local or source-control dependencies never looks
  clean.
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
  Impact-supported version matchers are npm, PyPI, Cargo, Pub, Swift, NuGet,
  and Maven; vendor-backed RPM-family OS package facts use the OS package priority
  above. Go, RubyGems, Composer, Hex, and other matcher-unimplemented
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
Repository, service, and workload scoped advisory-evidence requests first
select active reducer-owned impact findings for that scope, then read the
matching advisory source facts by the finding's CVE, advisory, and package
anchors. Provider-alert-only evidence is not used as an advisory-evidence seed.

`GET /api/v0/supply-chain/impact/explain` and the MCP
`explain_supply_chain_impact` tool use the same reducer-owned finding facts
but return one explanation at a time. Callers must provide `finding_id` or an
advisory/CVE plus package, repository, image digest/ref, workload, or service
anchor. The route
hydrates only the finding's `evidence_fact_ids`, returns advisory/source,
component/version, vulnerable-range, fixed-version, dependency-chain,
manifest/SBOM/image/workload/service/environment/provider-alert anchors when
those facts exist, includes an `impact_path` with present and missing hops, and
reports `outcome: no_finding` with readiness when a bounded scope has no
finding or `outcome: ambiguous_scope` when the scope matches multiple reducer
findings. It does not infer reachability or deployment truth from provider
alerts, image tags, workload names, service names, environment names, or
repository names.

Version and range matching is reducer-owned and ecosystem-aware. The supported
matchers are npm, Cargo, Go modules, Pub, Hex, and Swift semver over OSV-style
event ranges and GLAD-style comparator ranges; PyPI PEP 440 versions and
specifier sets including epochs, local versions, compatible releases,
exclusions, and prereleases; Composer constraints for exact, comparator, caret,
tilde, wildcard, stability-flagged, and OR branches; RubyGems exact,
comparator, and pessimistic requirements including prerelease segments; NuGet
semantic versions from exact lockfile or pinned manifest evidence; Maven
version/range ordering for Maven bracket and comparator ranges; RPM EVR
ordering for vendor-backed RPM-family OS package facts; and exact DPKG/APK
fixed-version checks where the distro package identity is explicit. Swift
support is exact `Package.resolved` evidence only; OSV `SwiftURL` records use a
Git URL as the source package name; Eshu normalizes that URL to the shared
Swift Package Manager identity and can also use a `PACKAGE` reference as a
fallback for source records that only publish a short package name. Pub support
requires hosted `pub.dev` parser evidence and exact `pubspec.lock` versions for
precise findings; manifest ranges remain partial evidence. RubyGems matching
uses the same reducer-owned exact-version gate as other precise lockfile
ecosystems: git/path Bundler dependencies remain ambiguous source evidence and
are not admitted as public RubyGems registry versions. SBOM component versions
only participate in exact matching when their PURL or package ID maps to a
supported package ecosystem. Findings preserve `observed_version`,
`requested_range`, the advisory vulnerable range (`vulnerable_range`),
`fixed_version`, malformed or unsupported state, and `match_reason` as separate
truth fields instead of collapsing them into severity or reachability.
Unsupported ecosystems do not produce clean impact findings; readiness surfaces
them as `unsupported_targets[]` with `reason=unsupported_ecosystem` when the
bounded scope has observed dependency evidence. Malformed installed versions,
malformed advisory ranges, unsupported operators, Composer branch aliases, and
`dev-*` branch evidence still fail closed as `possibly_affected` with explicit
missing-evidence reasons instead of being treated as affected or safely fixed.

No-Regression Evidence: `go test ./internal/reducer -run
'TestBuildSupplyChainImpactFindingsMatches(PyPISpecifierSets|GoModulePseudoVersions|ComposerConstraints|RubyGemsPessimisticConstraint)|TestEvaluate(PyPIMatch|ComposerMatch|RubyGemsMatch)|TestBuildSupplyChainImpactFindingsFailsClosedForUnsupportedAndMalformedRanges'
-count=1` proves the cross-ecosystem matcher boundary without changing queue,
graph, or API routing behavior.

No-Observability-Change: the matcher expansion only changes in-memory
supply-chain impact admission over facts the reducer already loads. It adds no
route, MCP tool, queue domain, graph write, worker, runtime knob, metric
instrument, or metric label. Operators continue to diagnose decisions through
the existing `reducer_supply_chain_impact_finding` payload fields
(`observed_version`, `requested_range`, `vulnerable_range`, `fixed_version`,
`match_reason`, and `missing_evidence`), `SupplyChainImpactFindings` reducer
counters, and the `query.supply_chain_impact_findings` span.

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

## Derived Advisory Target Planning

The workflow coordinator can derive bounded OSV package-version targets from
active owned dependency facts when `vulnerability_intelligence`
`derive_from_owned_packages.enabled=true`. Derivation is exact-version only.
It admits npm lockfile evidence, PyPI lockfile evidence from Pipfile.lock and
poetry.lock, Go module versions that remain usable Go module versions such as
`v0.17.0`, resolved Maven and Gradle coordinates, NuGet
`packages.lock.json`, Composer lockfile rows, Bundler lockfile rows, Cargo.lock
rows, Pub lockfile rows, Hex lockfile rows, and Swift `Package.resolved` remote
version pins with a usable source URL. The coordinator keeps canonical Eshu
ecosystems in workflow scope IDs and batches package-version queries into OSV
`/v1/querybatch` work items when the encoded scope remains inside the workflow
tuple limit.

Range-only manifests, aliases, workspace references, VCS/path/local
dependencies, branch refs, malformed versions, missing package names, and Swift
rows without a source URL stay out of exact advisory
collection. They are reported in `workflow_runs.requested_scope_set` as
aggregate skipped-target entries with stable reason codes and one ecosystem per
entry, without package names or versions. Budget exhaustion uses the same
skipped-target shape so representative proofs can show what was left out
without widening admitted work.

OS package and SBOM/component evidence is not promoted by this owned Git
dependency planner. Those target families need a separate source reader over
`vulnerability.os_package` and `sbom.component` facts so the planner can prove
package ecosystem, installed version, subject attachment, and vendor/source
context before issuing advisory queries.

No-Regression Evidence: `go test ./internal/coordinator -run 'TestVulnerabilityIntelligenceWorkPlanner(DerivesOSVTargetsAcrossSupportedEcosystems|ReportsSkippedDerivedTargetReasonsByEcosystem|DerivesOSVTargetsForExactOwnedVersions|DerivesOSVTargetsForExactHexVersions|HonorsFullCorpusDerivedTargetLimit|BatchesDerivedVersionsByPackage|BatchesDerivedQueriesAcrossPackages|KeepsDerivedBatchScopesIndexSafe|EncodesSwiftSourcePackageScopes|SkipsSwiftWithoutSourceURL|EncodesPubPackageScopes|SanitizesSwiftSourceLocations)|TestServiceRunActiveMode(PassesOwnedPackageEvidenceToVulnerabilityPlanner|SurfacesVulnerabilityDerivedBudgetExhaustion|SinglePassVulnerabilityDerivedBudgetDoesNotAdmitNextBucket)' -count=1`, `go test ./internal/workflow -run 'TestVulnerabilityIntelligenceCollectorConfigurationAcceptsDerivedSupportedEcosystems' -count=1`, and `go test ./internal/collector/vulnerabilityintelligence/vulnruntime -run 'TestClaimedSourceResolvesDerived(OSVTarget|OSVTargetBatch|OSVTargetQueryBatchAcrossPackages|SwiftOSVTarget|PubOSVTarget|OSVTargetsAcrossSupportedEcosystems)|TestHTTPProviderMapsSwiftOSVQueriesToSwiftURL' -count=1` prove exact target admission, skipped reasons, rotation, bounds, single-pass planning, and runtime batch resolution without live network calls.

No-Observability-Change: derived advisory target planning reuses existing
workflow runs, work items, claim status rows, `requested_scope_set`, coordinator
reconcile metrics, vulnerability-intelligence observe/fetch spans, observation
counter, fetch-duration histogram, facts-emitted counter, rate-limit counter,
source snapshot facts, and `/api/v0/index-status`. No metric label carries
package names, versions, PURLs, URLs, repository names, or credentials.

## Provider Alert Parity Gate

Provider security-alert collection, reconciliation statuses, private parity proof,
and aggregate-safe validation output are documented in
[Security Intelligence Provider Alert Parity](security-intelligence-provider-alert-parity.md).

## API And MCP Contract

Bounded security intelligence API and MCP read contracts are documented in
[Security Intelligence API And MCP Contract](security-intelligence-api-mcp-contract.md).

## CLI Contract

The local vulnerability scan command and export contracts are documented in
[Security Intelligence CLI Contract](security-intelligence-cli-contract.md).

## Acceptance Gates

Security intelligence work is ready only when applicable source-fact,
reducer-readiness, API/MCP scope, private provider-parity, remote Compose,
Kubernetes, and performance gates pass. The release-cut gate that ties these
proofs together is [Security Intelligence Release Gate](security-intelligence-release-gate.md).
`scripts/security_intelligence_release_gate.sh` aggregates commit, image,
backend, schema, fixture, API readback, remote Compose, and rollout snapshots
into one evidence document before an image cut is accepted.
