# Repository Dependency And Lockfile Coverage

Vulnerability impact in Eshu is only as strong as the repository dependency
evidence that feeds it. This page tracks which package-manager manifests and
lockfiles are parsed into `content_entity` dependency facts today, which are
still gaps, and what the safety rule is when evidence is missing.

This is the public coverage map for issue
[#571](https://github.com/eshu-hq/eshu/issues/571). It is generated from the
in-code matrix in
[`go/internal/parser/json/dependency_coverage.go`](https://github.com/eshu-hq/eshu/blob/main/go/internal/parser/json/dependency_coverage.go);
any change to the parser surface must update that matrix in the same PR or the
guard tests fail.

## Safety Rule

Missing dependency evidence is neither safe nor affected. It is missing.

The supply-chain impact reducer
([`go/internal/reducer/package_consumption_correlation.go`](https://github.com/eshu-hq/eshu/blob/main/go/internal/reducer/package_consumption_correlation.go))
admits package consumption only when a `package_registry.package` fact and a
Git `content_entity` dependency fact agree on ecosystem and normalized package
name. When the repository side has no manifest dependency fact for a package,
the reducer must return zero consumption decisions for that repository, and
the readiness envelope on
[`GET /api/v0/supply-chain/impact/findings`](http-api/evidence-and-supply-chain.md)
must report `missing_evidence: ["owned_packages"]` rather than letting
absence of evidence look like absence of risk. Guard tests:

- `go test ./internal/reducer -run 'TestBuildPackageConsumptionDecisionsRejects'`
  proves that registry identity alone cannot produce a consumption decision
  and that a `content_entity` row with the wrong `config_kind` is also
  rejected.
- `go test ./internal/parser/json -run 'TestDependencyCoverageGapsDoNotEmitDependencyRows'`
  proves that parsing a JSON-format gap file does not smuggle a fake
  dependency row into the fact store.
- `go test ./internal/parser -run 'TestDependencyCoverageGoSumDoesNotEmitConsumptionRows'`
  proves the gomod parser keeps go.sum as checksum-only evidence: parsing it
  through the engine MUST NOT produce `config_kind=dependency` rows.

## How To Read The Matrix

- **Status `covered`** means the file pattern is parsed end-to-end into a
  `content_entity` dependency row by the parser entrypoint listed in
  `Source`, locked in by
  `TestDependencyCoverageCoveredFilesEmitDependencyRowsThroughEngine` in
  `go/internal/parser/dependency_coverage_engine_test.go` (which dispatches
  through the real parser engine and therefore exercises every adapter —
  JSON, gomod, ruby, nuget_project, rust/cargo, pythondep, etc.).
  JSON-owned entries additionally carry per-package fixtures in
  `TestDependencyCoverageCoveredJSONFilesEmitDependencyRows` in
  `go/internal/parser/json/dependency_coverage_test.go`.
- **Status `gap`** means the file is not yet parsed into a dependency row;
  the reducer cannot use it as repository evidence; readiness must report
  the missing family. The matrix still names the file so the gap is visible
  to operators and reviewers.
- The `Identity`, `Exact`, `Range`, `Scope`, `Dev/Runtime`, `Chain` columns
  describe what each `covered` parser emits. Repository identity and source
  path are always added by the surrounding `content_entity` envelope
  (`repo_id`, `relative_path`) and are not parser-specific.

## Coverage Matrix

| Ecosystem | File | Kind | Status | Identity | Exact | Range | Scope | Dev/Runtime | Chain | Source | Notes |
|-----------|------|------|--------|----------|-------|-------|-------|-------------|-------|--------|-------|
| cargo | Cargo.lock | lockfile | covered | ✓ | ✓ | — | ✓ | — | ✓ | `go/internal/parser/rust/cargo_dependencies.go` | Cargo lockfiles emit exact crate versions and dependency paths only when the lock graph proves a package is reachable from a workspace root. |
| cargo | Cargo.toml | manifest | covered | ✓ | — | ✓ | ✓ | ✓ | — | `go/internal/parser/rust/cargo_dependencies.go` | Cargo manifests emit dependency rows for direct, dev, build, target-specific, workspace-inherited, and renamed package dependencies; transitive chains require Cargo.lock. |
| composer | composer.json | manifest | covered | ✓ | — | ✓ | ✓ | ✓ | — | `go/internal/parser/json/language.go` (`dependencyVariables`, composer) | `require` and `require-dev` sections emit content_entity rows. |
| composer | composer.lock | lockfile | covered | ✓ | ✓ | — | ✓ | ✓ | — | `go/internal/parser/json/composer_lock.go` | `packages` and `packages-dev` arrays emit exact-version rows with a `lockfile` flag so the reducer can join manifest ranges to installed PHP versions without dropping the dev/runtime split; transitive chain is not yet derived. |
| go | go.mod | manifest | covered | ✓ | — | ✓ | ✓ | — | — | `go/internal/parser/gomod/parser.go` (`Parse` for go.mod) | `require`/indirect/`replace`/`exclude` directives emit content_entity rows. go.mod is a manifest, not a lockfile: require versions are the MVS minimum-version requirement (and the version the resolver would select when no transitive dep forces a higher one), not a resolver-locked exact installed version, so the matrix tracks them under `Range`. Replacement targets surface as `resolved_module_path`/`resolved_version` on each require row; `replace` and `exclude` rows use distinct `config_kind` values so they never get admitted as consumption. Go has no dev/runtime split, and module-graph chains beyond direct/indirect are not stored in go.mod. |
| go | go.sum | lockfile | gap | — | — | — | — | — | — | `go/internal/parser/gomod/parser.go` emits checksum-only rows (`config_kind=dependency_checksum`, `ambiguous=true`) | go.sum records every module version any tool has ever verified, not the currently selected version, so checksum-only ambiguity prevents claiming exact observed installed evidence. The gomod parser emits `dependency_checksum` rows for audit corroboration but the consumption reducer treats go.sum as missing evidence until paired with a go.mod require. |
| gradle | build.gradle | build | covered | ✓ | — | ✓ | ✓ | ✓ | — | `go/internal/parser/gradle/parser.go` | Groovy DSL `dependencies` blocks (implementation, api, runtimeOnly, compileOnly, testImplementation, platform/enforcedPlatform, buildscript) emit content_entity rows. `project()/files()/fileTree()` are skipped; unresolved `${var}` interpolations stay marked `unresolved`. |
| gradle | build.gradle.kts | build | covered | ✓ | — | ✓ | ✓ | ✓ | — | `go/internal/parser/gradle/parser.go` | Kotlin DSL `dependencies` blocks emit content_entity rows mirroring the Groovy parser; configuration closures (e.g., `version { strictly(...) }`) without a top-level coordinate version stay `partial`. |
| maven | pom.xml | manifest | covered | ✓ | — | ✓ | ✓ | ✓ | — | `go/internal/parser/maven/parser.go` | `<dependencies>` and `<dependencyManagement>` emit content_entity rows with `groupId:artifactId` identity. Local `<properties>` resolve at parse time; parent-POM references and unsatisfied `${property}` references stay marked `partial`/`unresolved`. |
| npm | package.json | manifest | covered | ✓ | — | ✓ | ✓ | ✓ | — | `go/internal/parser/json/language.go` (`dependencyVariables`, npm) | `dependencies`, `devDependencies`, `optionalDependencies`, and `peerDependencies` emit content_entity rows with runtime/dev/optional/peer scope labels. |
| npm | package-lock.json | lockfile | covered | ✓ | ✓ | — | ✓ | ✓ | ✓ | `go/internal/parser/json/package_lock.go` | Lockfile v3 and v1 emit exact-version rows with `dependency_path`/`dependency_depth`, `direct_dependency`, and runtime/dev/optional/peer scope where npm records it. |
| npm | pnpm-lock.yaml | lockfile | covered | ✓ | ✓ | — | ✓ | ✓ | ✓ | `go/internal/parser/nodelockfile/pnpm.go` | pnpm v6+ lockfiles emit exact-version rows with `package_manager: npm` plus `package_manager_flavor: pnpm`, `dependency_path`/`dependency_depth`, `direct_dependency`, and runtime vs dev scope split; workspace, file, link, and portal entries stay out of remote evidence. |
| npm | yarn.lock | lockfile | covered | ✓ | ✓ | — | — | — | ✓ | `go/internal/parser/nodelockfile/yarn_classic.go` and `yarn_berry.go` | Yarn classic v1 and Yarn Berry lockfiles emit exact-version rows with `package_manager: npm` plus `package_manager_flavor: yarn`, `dependency_path`/`dependency_depth`, and `direct_dependency`; yarn.lock alone does not preserve runtime/dev/optional/peer importer scope, and unsupported non-npm Berry protocols remain audit-only rows. |
| nuget | *.csproj | manifest | covered | ✓ | — | ✓ | ✓ | ✓ | — | `go/internal/parser/nuget_project_language.go` | PackageReference rows preserve requested versions, resolved MSBuild properties, unresolved-property partial evidence, and PrivateAssets dev/test signals. |
| nuget | packages.lock.json | lockfile | covered | ✓ | ✓ | — | ✓ | — | ✓ | `go/internal/parser/json/nuget_lock.go` | NuGet lockfiles emit exact resolved versions and direct/transitive dependency paths when the lockfile proves them. |
| pypi | Pipfile | manifest | covered | ✓ | — | ✓ | ✓ | ✓ | — | `go/internal/parser/pythondep/pipfile.go` | `[packages]` and `[dev-packages]` emit content_entity rows; inline `{git=…}`, `{path=…}`, `{url=…}` entries surface as `vcs_dependency`, `path_dependency`, or `url_dependency`. |
| pypi | Pipfile.lock | lockfile | covered | ✓ | ✓ | — | ✓ | ✓ | — | `go/internal/parser/json/pipfile_lock.go` | `default` and `develop` sections emit exact-version rows (leading `==` stripped). Entries with `git`/`path`/`file` source keys surface as vcs/path/url provenance. |
| pypi | poetry.lock | lockfile | covered | ✓ | ✓ | — | ✓ | ✓ | — | `go/internal/parser/pythondep/poetry_lock.go` | Each `[[package]]` array entry emits one exact-version row; attached `[package.source]` `type=git|directory|url` swaps the row to vcs/path/url so registry-version semantics are not invented. |
| pypi | pyproject.toml | manifest | covered | ✓ | — | ✓ | ✓ | ✓ | — | `go/internal/parser/pythondep/pyproject.go` | PEP 621 `[project] dependencies`, `[project.optional-dependencies]`, `[tool.poetry.dependencies]`, `[tool.poetry.dev-dependencies]`, `[tool.poetry.group.*.dependencies]`, and `[tool.hatch.envs.*.dependencies]` all emit content_entity rows. VCS/path/url forms surface with non-`dependency` `config_kind`. |
| pypi | requirements.txt | manifest | covered | ✓ | — | ✓ | ✓ | ✓ | — | `go/internal/parser/pythondep/requirements.go` | pip requirements files (`requirements.txt`, `requirements-*.txt`, `requirements_*.txt`) emit rows with extras, environment markers, and a `dev_dependency` flag derived from dev/test filename suffixes. VCS/path/URL/editable/malformed entries surface as separate `config_kind` values. |
| rubygems | Gemfile | manifest | covered | ✓ | — | ✓ | ✓ | ✓ | — | `go/internal/parser/ruby/bundler_gemfile.go` | Literal `gem` declarations emit RubyGems rows with group scope and git/path source metadata; dynamic Ruby is skipped. |
| rubygems | Gemfile.lock | lockfile | covered | ✓ | ✓ | — | ✓ | — | ✓ | `go/internal/parser/ruby/bundler_lockfile.go` | Bundler lockfiles emit exact versions and dependency chains only when `DEPENDENCIES` and `specs:` indentation prove them. |
| swift | Package.resolved | lockfile | covered | ✓ | ✓ | — | — | — | — | `go/internal/parser/json/swift_package_resolved.go` | Swift Package.resolved v2 remote source-control pins emit exact-version rows with source namespace and SwiftPM identity; branch, revision-only, local, path, and unsupported pins remain non-evidence. |

## Implications For The Reducer And Readiness Envelope

- Covered files give the reducer a positive consumption decision when a
  package-registry identity matches. PR
  [#565](https://github.com/eshu-hq/eshu/pull/565) used npm/package.json
  evidence plus Maven registry identity to validate the log4j-core impact
  story end-to-end.
- npm manifests and lockfiles distinguish JavaScript/TypeScript runtime, dev,
  optional, and peer evidence where the source format supports it.
  `package.json` rows remain range-only evidence, so supply-chain impact
  reports them as possible/incomplete until paired with exact lockfile or
  SBOM evidence. `package-lock.json` rows carry exact installed versions
  plus npm-recorded scope flags. pnpm lockfiles preserve importer-side
  runtime/dev/optional/peer scope. Yarn classic and Berry lockfiles do not
  include importer scope by themselves, so Yarn rows keep exact versions and
  dependency chains but leave scope empty unless a paired manifest supplies
  it. Unsupported Yarn Berry protocols such as `patch:` are emitted as
  `unsupported_dependency` audit rows and are not admitted as precise
  consumption truth.
- Maven `pom.xml` and Gradle `build.gradle` / `build.gradle.kts` emit
  repository-side dependency facts with `groupId:artifactId` identity that
  the reducer joins to Maven package-registry identities, so JVM impact
  reads no longer depend on registry-side evidence alone. Versions resolved
  from local `<properties>` or Groovy `def`/`val`/`ext` blocks are reported
  as `resolved`; values that need parent POMs, Gradle source-set
  evaluation, or version catalogs stay `partial`/`unresolved` and keep the
  raw declaration in `value` so callers can spot the gap.
- Cargo `Cargo.toml` rows preserve manifest package names, renamed package
  identity, workspace-inherited dependency ranges, target-specific sections,
  and dev/build/runtime scope. Cargo `Cargo.lock` rows preserve exact resolved
  crate versions and dependency paths only when the lockfile root graph proves
  the transitive relationship.
- SwiftPM `Package.resolved` rows preserve exact versions only for remote
  source-control pins with `state.version`. The row identity is the source
  namespace plus SwiftPM package identity, for example
  `github.com/apple/swift-argument-parser`, with `package_manager: "swift"`.
  `Package.swift`, branch-only pins, revision-only pins, local pins, and path
  dependencies remain missing evidence. Swift advisory ingestion and impact
  matching are separate contracts documented in
  [Security Intelligence Source Coverage](security-intelligence-source-coverage.md).
- Go repositories now emit owned-package evidence from `go.mod`. Require,
  indirect-require, replace, and exclude directives become content_entity
  rows; the consumption reducer admits only the require/indirect rows
  (`config_kind=dependency`) and leaves replace/exclude rows as
  audit-only evidence. `go.sum` stays a gap because it records every
  module version any tool has verified rather than the currently selected
  version, so checksum-only ambiguity must remain explicit until paired
  with a go.mod require.
- Gap files turn into evidence-incomplete readiness states. The MCP and
  HTTP supply-chain reads must keep distinguishing
  `evidence_incomplete` from `ready_zero_findings` so callers cannot
  mistake a Maven-only or NuGet-only repo for "no vulnerabilities."
- When a new parser graduates a gap into a covered entry, the PR MUST
  update the matrix, add a covered-fixture entry to
  `TestDependencyCoverageCoveredFilesEmitDependencyRowsThroughEngine` in
  `go/internal/parser/dependency_coverage_engine_test.go` (and, when the
  adapter lives in the JSON package, to
  `TestDependencyCoverageCoveredJSONFilesEmitDependencyRows`), and add
  (or extend) a reducer test proving the new evidence path produces a
  consumption decision.

## Performance Evidence

No-Regression Evidence: baseline coverage before this slice was the
existing in-memory npm, Composer, RubyGems, NuGet, Go module, JVM, and
PyPI parser/reducer path; after the rebased change, `go test
./internal/parser/json ./internal/parser/nodelockfile
./internal/parser/gomod ./internal/parser/maven ./internal/parser/gradle
./internal/parser/pythondep ./internal/parser ./internal/reducer -count=1`
and `go test ./...` pass on Go 1.26.3 darwin/arm64. Input shape is
fixture-only manifests and lockfiles (`package.json`,
`package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`, `composer.json`,
`composer.lock`, `Gemfile`, `Gemfile.lock`, `.csproj`,
`packages.lock.json`, `go.mod`, `go.sum`, `pom.xml`, `build.gradle`,
`build.gradle.kts`, `requirements.txt`, `pyproject.toml`, `Pipfile`,
`Pipfile.lock`, and `poetry.lock`). Terminal runtime counts stay bounded
to parser dependency rows and reducer decisions asserted in tests: no
queue rows, graph rows, or Postgres rows are written by these paths.

`go test ./internal/parser -run 'TestParseNuGetProject|TestDependencyCoverage' -count=1`
proves PackageReference extraction, MSBuild property handling, malformed
XML rejection, and the engine-level matrix gate (every covered entry —
including pom.xml, build.gradle, build.gradle.kts, and the PyPI
requirements/pyproject/Pipfile/Pipfile.lock/poetry.lock fixtures — parses
through the real engine into dependency rows). `go test
./internal/parser/maven -count=1` and `go test ./internal/parser/gradle
-count=1` exercise the JVM parser fixtures in isolation; both packages
allocate per-call only — no shared state, locks, or worker pools — so
they add no concurrency surface to the ingest pipeline. `go test
./internal/parser/json -run
'TestDependencyCoverage|TestParseComposerLock|TestParseComposerManifestAndLockfile|TestParseNuGet|TestParsePipfileLock|TestParsePackageJSONEmitsRuntimeDevOptionalAndPeerScopes|TestParsePackageLockEmitsExactVersionScopeFlags'
-count=1` runs the matrix invariants plus the covered/gap parser fixtures
(npm, Composer, RubyGems, NuGet, Maven, Gradle, and Pipfile.lock), including
JavaScript/TypeScript package.json range scopes and package-lock exact-version
scope flags; it is a pure in-memory fixture path and adds no Cypher, queue, or
storage work. `go test ./internal/parser/nodelockfile -count=1` runs the Yarn
classic, Yarn Berry, and pnpm-lock.yaml fixtures (direct, transitive, scoped,
optional/peer importer scope, workspace/local, malformed,
unsupported-protocol, and multi-version-per-name) in the same
single-digit-second range. `go test
./internal/parser/gomod ./internal/parser/pythondep -run '.' -count=1`
covers go.mod require/indirect/replace/exclude rows, the go.sum
checksum-only ambiguity contract, the scanner-error / malformed-state
safeguard for go.sum, and the new PyPI parser fixtures (pins, ranges,
extras, markers, dev/runtime scope, VCS, path, URL, editable,
malformed). `go test ./internal/reducer -run
'TestBuildPackageConsumptionDecisions(Rejects|Matches|Normalizes|Preserves|Admits|Accepts|Keeps|.*Ruby|MatchesNuGet|PreservesNuGet|.*JVM|.*Maven|.*Gradle|AdmitsPyPI)'
-count=1` exercises positive consumption admission (Composer lockfile
evidence, RubyGems Bundler lockfile admission, NuGet lockfile and
project signals, yarn and pnpm flavors, the Go module require/indirect
case, Maven/Gradle manifest coordinates, and the new PyPI manifest
admission case), git/path ambiguity rejection, and the safety-rule
negatives (go.sum checksum-only ambiguity, replace/exclude
non-admission, malformed go.mod, PyPI VCS provenance non-admission)
without touching Postgres or the graph backend. `go test ./internal/reducer
-run 'TestBuildPackageConsumptionDecisions(AdmitsYarnLockEvidence|AdmitsPnpmLockEvidence|RejectsUnsupportedYarnBerryFeature)|TestBuildSupplyChainImpactFindings(ProvesNPMFamilyLockfileExactVersions|KeepsNPMDevScopeVisible)'
-count=1` proves npm-family lockfile consumption and impact parity across
package-lock, Yarn classic/Berry, and pnpm, including dev-scope propagation
and unsupported-lockfile rejection.

No-Regression Evidence: Cargo coverage is guarded by
`go test ./internal/parser -run 'TestCargoDependencyCoverageMatrixMarksCargoFilesCovered|TestDefaultEngineParsePathCargo' -count=1`,
`go test ./internal/parser/json -run 'TestDependencyCoverageMatrixIsStableAndExhaustive|TestDependencyCoverageCoveredFilesEmitDependencyRows' -count=1`,
and `go test ./internal/reducer -run 'TestBuildPackageConsumptionDecisions(MatchesCargoRenamedPackage|KeepsCargoLockfileWithoutProofUnchained)|TestPackageCorrelationWriterPersistsCargoLockfileEvidence|TestBuildSupplyChainImpactFindings(UsesCargoLockfileVersion|MarksCargoLockfileVersionKnownFixed|KeepsCargoManifestVersionRangeOnly)' -count=1`.
These are in-memory parser and reducer fixtures; they do not claim queue,
graph-backend, or hosted-runtime performance.

No-Regression Evidence: Swift dependency evidence is guarded by
`go test ./internal/packageidentity -count=1`,
`go test ./internal/parser/json -run 'TestDependencyCoverageMatrixIsStableAndExhaustive|TestDependencyCoverageCoveredJSONFilesEmitDependencyRows|TestSwiftPackageResolvedEmitsOnlyVersionedRemoteDependencies' -count=1`,
and
`go test ./internal/parser -run TestDependencyCoverageCoveredFilesEmitDependencyRowsThroughEngine -count=1`.
These fixtures prove SwiftPM identity normalization, exact-version
Package.resolved row emission, fail-closed branch/revision/local pin handling,
and parser-engine exact-name dispatch. They do not by themselves prove hosted
runtime collection, queue behavior, graph writes, or deployed readback.

No-Observability-Change: this change is parser fixture work and reducer
truth assertions. It introduces no new metric instrument, span, log key,
queue, reducer lane, graph write, or runtime worker. Operators continue to
diagnose dependency-evidence coverage through the existing
supply-chain impact readiness envelope (`missing_evidence` family
`owned_packages`, `package.consumption` evidence-source count) and the
existing `reducer_supply_chain_impact_finding` fact payload. Composer
lockfile rows carry the same `lockfile: true` metadata bit used by the
npm `package-lock.json` parser, so the reducer treats lockfile
directness uniformly: when no explicit dependency chain is present, the
decision surfaces `direct_dependency: null` rather than guessing. Maven
and Gradle dependency rows flow through the same `content_entity` ingest
path as npm/Composer/RubyGems rows and reuse the existing parser-stage
histograms.

No-Regression Evidence: JavaScript/TypeScript vulnerability parity for issue
`#997` is guarded by `go test ./internal/parser/json -run
'TestParsePackageJSONEmitsRuntimeDevOptionalAndPeerScopes|TestParsePackageLockEmitsExactVersionScopeFlags'
-count=1`, `go test ./internal/parser/nodelockfile -run
'TestParseUnsupportedYarnBerryFeatureIsRecorded|TestParsePnpmLockfilePreservesOptionalAndPeerImporterScopes'
-count=1`, and `go test ./internal/reducer -run
'TestBuildPackageConsumptionDecisionsRejectsUnsupportedYarnBerryFeature|TestBuildSupplyChainImpactFindingsProvesNPMFamilyLockfileExactVersions|TestBuildSupplyChainImpactFindingsKeepsNPMDevScopeVisible'
-count=1`. These fixtures prove package.json range-only npm scopes,
package-lock exact installed scope flags, Yarn unsupported-protocol
audit rows, pnpm optional/peer importer scopes, precise exact-version
findings, dev-scope readback, and fail-closed unsupported lockfile
handling without graph, queue, Postgres, or hosted runtime work.

No-Observability-Change: the `#997` parity work changes parser row metadata
and reducer admission filters only. It adds no metric instrument, span, log
key, queue, reducer lane, graph write, route, MCP tool, scanner worker, or
runtime configuration knob. Operators continue to diagnose the path through
existing parser-stage timing, package-consumption correlation facts,
`reducer_supply_chain_impact_finding`, `match_reason`, `dependency_scope`,
`missing_evidence`, and the supply-chain impact readiness envelope.

No-Regression Evidence: NuGet/.NET vulnerability parity for issue `#1015` is
guarded by `go test ./internal/parser ./internal/reducer ./internal/query -run
'TestParseNuGetProjectKeepsAmbiguousMSBuildPropertyPartial|TestBuildPackageConsumptionDecisions(SeparatesNuGetLockfileRequestedRange|PreservesNuGetPartialMSBuildEvidence)|TestBuildSupplyChainImpactFindings(MatchesNuGetBracketRange|MarksNuGetFixedVersionSafe|KeepsNuGetLockfileRequestedRangeSeparate|KeepsNuGetUnresolvedPropertyPartial|FailsClosedForMalformedNuGetRange)|TestBuildSupplyChainImpactReadinessClassifiesNuGetReadyZeroFindings'
-count=1`, `go test ./internal/parser ./internal/parser/json ./internal/reducer
./internal/query -run
'TestParseNuGet|TestBuildPackageConsumptionDecisions.*NuGet|TestBuildSupplyChainImpactFindings.*NuGet|TestEvaluateNuGet|TestBuildSupplyChainImpactReadiness.*NuGet|TestParseNuGetPackagesLockJSON'
-count=1`, and `go test ./internal/reducer -run
'TestBuildPackageConsumptionDecisions(SeparatesNuGetLockfileRequestedRange|PreservesNuGetPartialMSBuildEvidence)|TestPackageConsumptionPayloadPersistsNuGetVersionEvidence|TestBuildSupplyChainImpactFindings(MatchesNuGetBracketRange|MarksNuGetFixedVersionSafe|KeepsNuGetLockfileRequestedRangeSeparate|KeepsNuGetUnresolvedPropertyPartial|FailsClosedForMalformedNuGetRange)'
-count=1`. These tests prove ambiguous MSBuild properties fail closed instead
of producing fake installed versions, NuGet `packages.lock.json` keeps exact
installed versions separate from requested ranges, PackageReference
PrivateAssets/dev/test evidence remains independent scope context, NuGet
bracket ranges and fixed-version known-safe behavior are matched with explicit
`match_reason`, malformed ranges remain possible affected with missing-evidence
reasons, and ready-zero NuGet evidence stays distinct from unsupported or
evidence-incomplete states.

No-Observability-Change: the `#1015` parity work changes in-memory parser rows,
package-consumption fact payload fields, reducer version matching, and query
readiness fixtures only. It adds no metric instrument, span, log key, queue,
reducer lane, graph write, route, MCP tool, scanner worker, or runtime
configuration knob. Operators continue to diagnose NuGet coverage through the
existing parser-stage timing, `reducer_package_consumption_correlation`,
`reducer_supply_chain_impact_finding`, `observed_version`, `requested_range`,
`vulnerable_range`, `match_reason`, `missing_evidence`, `dependency_scope`, and
the supply-chain impact readiness envelope.

No-Regression Evidence: Java Maven and Gradle vulnerability parity for issue
`#1011` is guarded by `go test ./internal/parser/maven
./internal/parser/gradle -count=1` and `go test ./internal/reducer -run
'TestBuildSupplyChainImpactFindings(ProvesJVMManifestParity|KeepsUnresolvedJVMVersionsIncomplete)|TestBuildSupplyChainImpactFindingsExplainsMavenRangeAndFixedVersion'
-count=1`. The fixtures prove `pom.xml` dependencies,
`dependencyManagement`, `build.gradle`, and `build.gradle.kts` rows join
through `groupId:artifactId` Maven identity to advisory evidence, Maven
bracket ranges classify affected and known-fixed versions, and unresolved JVM
manifest versions keep `observed_version` blank with explicit missing-version
evidence instead of becoming affected or safely fixed.

No-Observability-Change: the `#1011` parity work changes in-memory parser
fixtures and the exact-manifest-version guard only. It adds no metric
instrument, span, log key, queue, reducer lane, graph write, scanner worker,
route, MCP tool, or runtime configuration knob. Operators continue to diagnose
JVM impact through existing parser-stage timing, package-consumption
correlation facts, `reducer_supply_chain_impact_finding`, `match_reason`,
`requested_range`, `observed_version`, `missing_evidence`, and the
supply-chain impact readiness envelope.

No-Regression Evidence: Python/PyPI vulnerability parity for issue `#999` is
guarded by `go test ./internal/parser/pythondep -run
TestParseRequirementsKeepsPEP508DirectReferencesOutOfRegistryEvidence -count=1`,
`go test ./internal/reducer -run
'TestBuildSupplyChainImpactFindings(ProvesPyPILockfileExactVersions|KeepsPyPIManifestRangesPossible)'
-count=1`, and `go test ./internal/query -run
TestPostgresSupplyChainImpactReadinessQueryShape -count=1`. These fixture tests
prove PEP 508 direct references stay out of registry-version evidence, PyPI
lockfile versions produce precise affected and known-fixed findings through the
PEP 440 matcher, manifest-only ranges stay possible with missing installed
version evidence, and repository readiness no longer reports PyPI dependency
rows as unsupported target evidence.

No-Observability-Change: the `#999` parity work changes parser row
classification, in-memory reducer version matching, and the supported
package-manager allowlist in the existing readiness SQL. It adds no metric
instrument, span, log key, queue, reducer lane, graph write, route, MCP tool,
scanner worker, or runtime configuration knob. Operators continue to diagnose
the path through parser-stage timing, `package.consumption` readiness counts,
`reducer_supply_chain_impact_finding`, `match_reason`, `missing_evidence`, and
the supply-chain impact readiness envelope.

No-Regression Evidence: Go module vulnerability parity for issue `#998` is
guarded by `go test ./internal/parser/gomod ./internal/reducer -run
'Test(BuildPackageConsumptionDecisions.*Go|BuildSupplyChainImpactFindings.*Go|ClassifyGoVulnerabilityReachability)' -count=1`.
The fixtures prove `go.mod` require and indirect rows remain the owned
dependency evidence admitted by package consumption, replacement metadata keeps
the declared `requested_range` separate from the resolved `observed_version`,
`go.sum` checksum-only rows do not prove an installed vulnerable version, Go
OSV SEMVER ranges classify affected and known-fixed versions, malformed Go
advisory ranges fail closed with explicit missing evidence, and govulncheck
reachability is read back only after module evidence anchors the repository.

Performance Evidence: the Go parity path is reducer-local over the same fact
envelope already loaded for supply-chain impact. It adds one per-envelope Go
reachability index keyed by `(osv_id, module_path, repository_id)` and does not
introduce filesystem scans, advisory network calls, queue work, graph writes,
Postgres writes, or scanner workers. The focused reducer fixtures remain
in-memory and bounded to the package, advisory, module-evidence, and
call-reachability rows supplied by each test.

No-Observability-Change: the `#998` parity work reuses existing fact kinds and
readback fields: `package.consumption`, `vulnerability.go_module_evidence`,
`vulnerability.go_call_reachability`, `reducer_supply_chain_impact_finding`,
`match_reason`, `runtime_reachability`, `requested_range`, `observed_version`,
`EvidencePath`, and `missing_evidence`. It adds no metric instrument, span, log
key, queue, reducer lane, route, MCP tool, graph write, scanner worker, or
runtime configuration knob.
