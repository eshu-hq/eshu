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
  proves that parsing a gap file does not smuggle a fake dependency row into
  the fact store.

## How To Read The Matrix

- **Status `covered`** means the file pattern is parsed end-to-end into a
  `content_entity` dependency row by the parser entrypoint listed in
  `Source`, locked in by
  `TestDependencyCoverageCoveredFilesEmitDependencyRows` in
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
| cargo | Cargo.lock | lockfile | gap | — | — | — | — | — | — | no TOML parser registered in `go/internal/parser/registry.go` | Cargo lockfile is TOML and not yet parsed. |
| cargo | Cargo.toml | manifest | gap | — | — | — | — | — | — | `go/internal/parser/rust/cargo_cfg.go` parses cfg signals only | Cargo manifests are only scanned for cfg/feature signals; dependency tables are not yet emitted as content_entity facts. |
| composer | composer.json | manifest | covered | ✓ | — | ✓ | ✓ | ✓ | — | `go/internal/parser/json/language.go` (`dependencyVariables`, composer) | `require` and `require-dev` sections emit content_entity rows. |
| composer | composer.lock | lockfile | covered | ✓ | ✓ | — | ✓ | ✓ | — | `go/internal/parser/json/composer_lock.go` | `packages` and `packages-dev` arrays emit exact-version rows with a `lockfile` flag so the reducer can join manifest ranges to installed PHP versions without dropping the dev/runtime split; transitive chain is not yet derived. |
| go | go.mod | manifest | gap | — | — | — | — | — | — | `go/internal/parser/go_package_interface_prescan.go` reads go.mod for package interface only | go.mod is read for package-interface prescan but does not emit content_entity dependency facts. |
| go | go.sum | lockfile | gap | — | — | — | — | — | — | no parser registered in `go/internal/parser/registry.go` | Module checksum file is not parsed; exact-version evidence for Go repos is missing. |
| gradle | build.gradle | build | gap | — | — | — | — | — | — | `go/internal/parser/registry.go` registers groovy by extension but does not extract dependency blocks | Groovy build scripts are parsed for syntax only. |
| gradle | build.gradle.kts | build | gap | — | — | — | — | — | — | `go/internal/parser/registry.go` registers kotlin by extension but does not extract dependency blocks | Kotlin DSL build scripts are parsed for syntax only. |
| maven | pom.xml | manifest | gap | — | — | — | — | — | — | no XML parser registered in `go/internal/parser/registry.go` | Maven POMs are not yet parsed; Maven impact relies on registry-side evidence only. |
| npm | package.json | manifest | covered | ✓ | — | ✓ | ✓ | ✓ | — | `go/internal/parser/json/language.go` (`dependencyVariables`, npm) | `dependencies` and `devDependencies` emit content_entity rows; optional and peer scopes are not yet split. |
| npm | package-lock.json | lockfile | covered | ✓ | ✓ | — | ✓ | — | ✓ | `go/internal/parser/json/package_lock.go` | Lockfile v3 and v1 emit exact-version rows with `dependency_path`/`dependency_depth` and `direct_dependency`. |
| npm | pnpm-lock.yaml | lockfile | gap | — | — | — | — | — | — | `go/internal/parser/yaml/language.go` does not branch on pnpm-lock.yaml | pnpm lockfiles are not yet parsed. |
| npm | yarn.lock | lockfile | gap | — | — | — | — | — | — | no parser registered in `go/internal/parser/registry.go` | Yarn classic and Berry lockfiles are not yet parsed. |
| nuget | *.csproj | manifest | covered | ✓ | — | ✓ | ✓ | ✓ | — | `go/internal/parser/nuget_project_language.go` | PackageReference rows preserve requested versions, resolved MSBuild properties, unresolved-property partial evidence, and PrivateAssets dev/test signals. |
| nuget | packages.lock.json | lockfile | covered | ✓ | ✓ | — | ✓ | — | ✓ | `go/internal/parser/json/nuget_lock.go` | NuGet lockfiles emit exact resolved versions and direct/transitive dependency paths when the lockfile proves them. |
| pypi | Pipfile | manifest | gap | — | — | — | — | — | — | no TOML parser registered in `go/internal/parser/registry.go` | Pipenv manifest is not yet parsed. |
| pypi | Pipfile.lock | lockfile | gap | — | — | — | — | — | — | `go/internal/parser/json/language.go` does not branch on Pipfile.lock | Pipenv lockfile is JSON but not yet branched into dependency rows. |
| pypi | poetry.lock | lockfile | gap | — | — | — | — | — | — | no TOML parser registered in `go/internal/parser/registry.go` | Poetry lockfile is TOML and not yet parsed. |
| pypi | pyproject.toml | manifest | gap | — | — | — | — | — | — | no TOML parser registered in `go/internal/parser/registry.go` | PEP 621 / Poetry / Hatch manifests are not yet parsed. |
| pypi | requirements.txt | manifest | gap | — | — | — | — | — | — | no raw-text dependency parser branches on requirements\*.txt | pip-style requirement files are not yet parsed. |
| rubygems | Gemfile | manifest | covered | ✓ | — | ✓ | ✓ | ✓ | — | `go/internal/parser/ruby/bundler_gemfile.go` | Literal `gem` declarations emit RubyGems rows with group scope and git/path source metadata; dynamic Ruby is skipped. |
| rubygems | Gemfile.lock | lockfile | covered | ✓ | ✓ | — | ✓ | — | ✓ | `go/internal/parser/ruby/bundler_lockfile.go` | Bundler lockfiles emit exact versions and dependency chains only when `DEPENDENCIES` and `specs:` indentation prove them. |

## Implications For The Reducer And Readiness Envelope

- Covered files give the reducer a positive consumption decision when a
  package-registry identity matches. PR
  [#565](https://github.com/eshu-hq/eshu/pull/565) used npm/package.json
  evidence plus Maven registry identity to validate the log4j-core impact
  story end-to-end. Maven impact still relies on registry-side evidence
  alone until `pom.xml` is parsed.
- Gap files turn into evidence-incomplete readiness states. The MCP and
  HTTP supply-chain reads must keep distinguishing
  `evidence_incomplete` from `ready_zero_findings` so callers cannot
  mistake a Go-only or Maven-only repo for "no vulnerabilities."
- When a new parser graduates a gap into a covered entry, the PR MUST
  update the matrix, add a covered-fixture entry to
  `TestDependencyCoverageCoveredFilesEmitDependencyRows`, and add (or
  extend) a reducer test proving the new evidence path produces a
  consumption decision.

## Performance Evidence

No-Regression Evidence: baseline coverage before the NuGet slice was the
existing in-memory npm, Composer, and RubyGems parser/reducer path; after the
rebased change, `go test ./internal/parser/json ./internal/parser
./internal/reducer -count=1` and `go test ./...` pass on Go 1.26.3
darwin/arm64. Input shape is fixture-only manifests and lockfiles
(`package.json`, `package-lock.json`, `composer.json`, `composer.lock`,
`Gemfile`, `Gemfile.lock`, `.csproj`, and `packages.lock.json`). Terminal
runtime counts stay bounded to parser dependency rows and reducer decisions
asserted in tests: no queue rows, graph rows, or Postgres rows are written by
these paths.

`go test ./internal/parser -run 'TestParseNuGetProject' -count=1` proves
PackageReference extraction, MSBuild property handling, and malformed XML
rejection. `go test ./internal/parser/json -run
'TestDependencyCoverage|TestParseComposerLock|TestParseComposerManifestAndLockfile|TestParseNuGet'
-count=1` runs the matrix invariants plus the covered/gap parser fixtures,
including Composer, RubyGems, and NuGet lockfile fixtures; it is a pure
in-memory fixture path and adds no Cypher, queue, or storage work. `go test
./internal/reducer -run
'TestBuildPackageConsumptionDecisions(Rejects|Matches|Normalizes|Preserves|Admits|Keeps|.*Ruby|MatchesNuGet|PreservesNuGet)'
-count=1` exercises positive consumption admission, Composer lockfile
evidence, RubyGems Bundler lockfile admission, NuGet lockfile and project
signals, git/path ambiguity rejection, and the safety-rule negatives without
touching Postgres or the graph backend.

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
decision surfaces `direct_dependency: null` rather than guessing.
