# scannerworker

## Purpose

`scannerworker` defines the contract and claim loop for isolated security
analyzer workers. Concrete analyzers plug in behind the `Analyzer` port; this
package stays responsible for the shared claim, output, failure, and telemetry
boundary.

## Ownership boundary

This package owns analyzer lane routing, scanner-worker claim input, bounded
target scope, resource limits, source-fact output validation, retry or
dead-letter payload shape, and the workflow claim loop used by
`cmd/scanner-worker`.

Reducers remain truth owners. Scanner workers may emit source fact families such
as `scanner_worker.*`, `sbom.*`, `attestation.*`, `vulnerability.os_package`,
and `vulnerability.warning`, but they must not emit reducer finding facts or
graph projection phases.

## Exported surface

- `AnalyzerKind`, `ExecutionLane`, `AnalyzerLane` — route heavy analyzer
  profiles to `scanner_worker` and reducer-owned analysis to `reducer`.
- `TargetScope`, `TargetKind` — bounded repository, image, or artifact target
  identity copied from workflow work items. `LocatorHash` must use
  `sha256:<64 hex>`.
- `ResourceLimits` — CPU, memory, timeout, input-size, file-count, and
  fact-count limits a runtime must enforce.
- `ClaimInput`, `NewClaimInput`, `NewClaimInputAt` — immutable scanner-worker
  input copied from an active workflow claim and fencing token.
- `FactOutput`, `ValidateFactOutput` — validates fenced source facts and
  rejects silent clean output or reducer-owned finding facts.
- `FailureDisposition`, `FailureClass`, `ResourceUsage`, `FailurePayload`,
  `FailurePayloadFor` — bounded retry and dead-letter payloads that avoid raw
  target locators.
- `Analyzer`, `AnalyzerResult`, `AnalyzerFailure`,
  `NewRetryableAnalyzerFailure`, `NewTerminalAnalyzerFailure` — analyzer port
  and failure vocabulary used by hosted workers.
- `DefaultResourceLimits`, `TargetScopeFromWorkItem` — runtime defaults and
  safe target-scope derivation for scanner work items.
- `Service` — claim loop that copies workflow claims into `ClaimInput`, runs an
  analyzer, validates source-fact output, commits under the claim fence, and
  records retry or dead-letter state.
- `WarningAnalyzer` — safe built-in analyzer used when no concrete scanner is
  configured; it emits `scanner_worker.warning`.

Concrete scanner-worker analyzers live in sub-packages of this package:
`imageanalyzer` reads configured image rootfs metadata or ordered local OCI
layer tar streams and emits `scanner_worker.analysis` coverage facts,
installed OS package facts, or unsupported warning facts, and `sbomgenerator`
emits bounded CycloneDX-compatible `sbom.document`,
`sbom.component`, and `sbom.warning` source facts for repository, image, or
artifact targets when the runtime source has enough subject evidence. The
hosted `eshu-scanner-worker` binary wires configured image, OS package
extraction, and repository-manifest SBOM sources for `package-lock.json`,
`npm-shrinkwrap.json`, `go.mod`, `Cargo.lock`, `composer.lock`,
`packages.lock.json`, `Pipfile.lock`, `poetry.lock`, `Gemfile.lock`, and
`gradle.lockfile` targets, and keeps `WarningAnalyzer` as the fallback when no
runtime-owned analyzer source is configured, so claims still commit explicit
warning facts instead of pretending the target was scanned clean.

## Dependencies

- `internal/facts` — source fact envelope and scanner/SBOM fact registries.
- `internal/storage/postgres/ingest` — claim-fenced source fact commits.
- `internal/scope` — scanner-worker collector identity.
- `internal/telemetry` — scanner-worker metrics, spans, and structured logs.
- `internal/workflow` — work item and claim contracts.

## Telemetry

`Service` records the scanner-worker metric set registered in
`internal/telemetry`: claim starts/completions, retryable failures, terminal
dead letters, source facts emitted, queue wait, scan duration, target count,
result count, CPU seconds, and memory bytes. It starts
`scanner_worker.claim.process`, `scanner_worker.analyze`, and
`scanner_worker.fact.emit_batch` spans and logs bounded failure fields.

## Gotchas / invariants

- Scanner workers emit source facts only. Reducer finding facts are rejected.
- Concrete analyzers may preserve their parser-owned `collector_kind` and
  `source_ref.source_system` as long as both are nonblank, match each other,
  and the fact kind is on the scanner-worker source allowlist.
- Completed scanner claims must emit at least one source fact or warning; silent
  clean output is not accepted.
- Retry and dead-letter payloads carry safe locator hashes and bounded failure
  classes, not raw repository paths, image names, registry URLs, package
  coordinates, or bucket keys.
- Claim input rejects mismatched claim IDs, fencing tokens, owners, lease
  expirations, expired claims, and non-scanner workflow items.
- The built-in warning analyzer is not a clean finding. It preserves proof that
  the hosted worker claimed and committed source evidence without pretending a
  real scanner ran. Warning payloads carry `analysis_status=not_scanned` and
  `coverage_status=unsupported` so readback can expose missing analyzer
  capability instead of classifying the target as safe, affected, or scanned.

## Evidence

Collector Performance Evidence: `go test ./internal/collector/scannerworker ./internal/collector/scannerworker/imageanalyzer ./internal/collector/scannerworker/sbomgenerator ./internal/collector/ospackagevulnerability/osruntime ./cmd/scanner-worker ./internal/runtime ./internal/telemetry ./internal/workflow ./internal/scope -count=1`
covers claim processing, repository/image/artifact target kind derivation,
source-fact output, configured image rootfs/layer package extraction,
configured repository SBOM generation, OS package analyzer apk/dpkg parsing,
retry/dead-letter handling, and deployment render contracts.
Analyzer rollout still needs target count, fact count, runtime, CPU, memory,
queue state, retry count, dead-letter count, and pprof evidence from the target
environment before it becomes a default.

No-Regression Evidence: `go test ./cmd/scanner-worker -run 'TestRepositorySBOMSource(ParsesCargoAndComposerLockfiles|ParsesPythonRubyAndGradleLockfiles|EmitsMalformedLockfileWarning)' -count=1`
proved the hosted scanner-worker repository SBOM source extracts Cargo,
Composer, PyPI, RubyGems, Gradle/Maven, and NuGet exact lockfile components with
bounded ecosystem, relative path, dependency scope/type, PURL, and extraction
reason evidence, and turns malformed Composer lockfiles into bounded
`sbom.warning` evidence instead of clean output or path-leaking terminal
failure.

Collector Observability Evidence: `Service` records
`eshu_dp_scanner_worker_claims_total`,
`eshu_dp_scanner_worker_retries_total`,
`eshu_dp_scanner_worker_dead_letters_total`,
`eshu_dp_scanner_worker_facts_emitted_total`,
`eshu_dp_scanner_worker_queue_wait_seconds`,
`eshu_dp_scanner_worker_scan_duration_seconds`,
`eshu_dp_scanner_worker_target_count`,
`eshu_dp_scanner_worker_result_count`,
`eshu_dp_scanner_worker_cpu_seconds`, and
`eshu_dp_scanner_worker_memory_bytes`, plus
`scanner_worker.claim.process`, `scanner_worker.analyze`, and
`scanner_worker.fact.emit_batch` spans.

Collector Deployment Evidence: Dockerfile, local binary install, remote Compose,
pprof overlay, and Helm render tests include `eshu-scanner-worker`. Helm keeps
the scanner-worker Deployment disabled by default and rejects it unless the
workflow coordinator runs in active claims mode. Remote Compose runtime-state
proof is still blocked locally because no `eshu-remote-e2e` containers are
running.

## Related docs

- `docs/public/reference/security-intelligence.md`
- `docs/public/reference/collector-reducer-readiness.md`
- `docs/public/reference/telemetry/metrics-ingestion-collectors.md`
