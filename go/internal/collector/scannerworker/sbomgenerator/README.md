# sbomgenerator

## Purpose

`sbomgenerator` is the bounded scanner-worker analyzer that generates
CycloneDX-compatible SBOM source facts from a repository, image, or artifact
target when the runtime source has enough subject evidence. It runs inside the
scanner-worker boundary defined by
`internal/collector/scannerworker` and never publishes user-facing security
findings: every emitted fact is a source fact or warning that the existing
`sbom_attestation_attachment` reducer is responsible for admitting.

This package does not replace the hosted `sbom-attestation` collector
(`internal/collector/sbomruntime`). That collector fetches and parses
already-published CycloneDX, SPDX, and in-toto documents from configured URLs
and OCI referrers. `sbomgenerator` builds new SBOM evidence when no such
document exists, and it stays on the scanner-worker lane so repository
indexing, image or artifact analysis, and reducer drains keep their CPU and
memory headroom.

## Ownership boundary

This package owns:

- The `Source` port the runtime implements to return a bounded
  `Inventory` for one scanner-worker claim.
- The mapping from `Inventory` to `sbom.document`, `sbom.component`, and
  `sbom.warning` source facts (collector kind `scanner_worker`, schema
  version `1.0.0`).
- The contract-level enforcement of `MaxFiles`, `MaxInputBytes`, and
  `MaxFacts` from `scannerworker.ResourceLimits`.
- The analyzer treats `Inventory.FileCount` as analyzer input files. The
  repository-manifest runtime source counts supported manifest and lockfile
  files, not unrelated regular files skipped by the repository walk.
- The privacy-safe failure vocabulary used when the runtime cannot satisfy
  the claim (`unsupported_target`, `source_unavailable`,
  `file_limit_exceeded`, `input_limit_exceeded`, `fact_limit_exceeded`,
  `analyzer_failed`). Raw target locators stay out of the failure payload.

The runtime owns filesystem walking, archive extraction, SDK invocation,
and CPU/memory/timeout enforcement (cgroups, ulimits, or process supervision
configured per analyzer profile in `cmd/scanner-worker`). Reducers own the
attachment truth table that decides whether an emitted `sbom.document`
becomes attached, attached_parse_only, ambiguous_subject, unknown_subject,
unparseable, or subject-mismatched.

## Exported surface

- `Analyzer` — implements `scannerworker.Analyzer`. Construct with a
  runtime-owned `Source` and an optional `Now` clock.
- `Source` — port the runtime implements to return bounded inventory for
  one claim.
- `Inventory`, `Component`, `Warning` — bounded inputs the source returns,
  including measured CPU and peak-memory usage for scanner-worker telemetry.
  Repository lockfile sources use component and warning fields to preserve
  ecosystem, relative lockfile path, evidence source, dependency scope/type
  when present, and the extraction reason.
- `ErrUnsupportedTarget`, `ErrSourceUnavailable` — sentinels the runtime
  returns from `Collect`; the analyzer maps them to terminal
  `unsupported_target` or retryable `source_unavailable` workflow
  dispositions.
- `Format`, `ParseStatusGenerated`, `SpecVersionDefault`, `ToolDefault` —
  constants emitted on document facts so reducer attachment can distinguish
  generated documents from parsed third-party documents.
- `WarningReason*` constants — bounded vocabulary used on `sbom.warning`
  facts (`missing_subject`, `malformed_subject_digest`,
  `component_missing_identity`, `no_components_found`).

## Gotchas / invariants

- Scanner workers must emit at least one source or warning fact per
  completed claim. The analyzer always emits a document fact; when zero
  components survive identity validation it also emits a
  `no_components_found` warning so the output is never a silent clean
  finding.
- Subject digests must use `sha256:<64 hex>`. Malformed digests are
  recorded as `malformed_subject_digest` warnings and the document fact's
  `subject_digest` is cleared; the reducer then classifies the document as
  `unknown_subject`. The analyzer never invents subject identity.
- Component facts deduplicate on `purl`-or-`name@version` canonical
  identity (lowercased, whitespace-trimmed). Components that differ only in
  PURL casing, `bom_ref`, or extra metadata collapse to one emitted fact
  and the duplicates surface as aggregated `component_missing_identity`
  warnings with the canonical identity hint. Components missing both PURL and
  `name@version` identity also aggregate by stable warning key. Aggregate
  warning payloads preserve `occurrence_count` and bounded
  `sample_component_indexes` so operators can diagnose affected component
  volume without one warning row per occurrence. The component fact ID is
  derived from the same canonical identity, so two equivalent inputs always
  produce identical fact IDs across runs.
- Repository lockfile inputs must not invent components without exact
  lockfile proof. When the runtime source can name the ecosystem, relative
  lockfile path, package name, installed version, dependency scope/type, or
  extraction reason, the analyzer preserves those fields on the component
  fact. Malformed supported lockfiles become bounded `sbom.warning` facts
  such as `lockfile_malformed`; they are not terminal analyzer failures and
  they are not clean results.
- Resource-limit breaches dead-letter; they do not silently truncate the
  output. The analyzer pre-checks non-component warnings, then checks
  component and aggregate warning facts while building the bundle. Repeated
  missing-identity components can collapse into one warning fact, while
  genuinely high-cardinality unique component output still fails before
  emitting any partial fact bundle.
- Runtime sources carry `ResourceUsage` on `Inventory`; the analyzer passes it
  through to `scannerworker.Service` so `sbom_generation` records CPU and
  memory signals with the rest of the scanner-worker metric set.
- Generic `Source` errors map to terminal `analyzer_failed` and the wrapped
  cause is discarded. This keeps repository paths, image names, registry
  URLs, and package coordinates out of retry/dead-letter payloads.

## Dependencies

- `internal/collector/scannerworker` — claim input, resource limits,
  analyzer failure vocabulary, source-fact validator.
- `internal/facts` — `sbom.document`, `sbom.component`, and `sbom.warning`
  envelope contracts plus the SBOM/attestation schema-version registry.
- `internal/scope` — collector kind constant (`scanner_worker`).

## Evidence

Coverage Evidence: `go test ./internal/collector/scannerworker/sbomgenerator -count=1`
exercises successful generation, repository/image/artifact target support,
malformed subject digest, missing subject warning, component identity skipping,
silent clean rejection, file/input/fact limit enforcement, unsupported target
classification, retryable source unavailability, terminal analyzer failure
with privacy-safe error strings, and resource-usage propagation.

No-Regression Evidence:
`go test ./internal/collector/scannerworker/sbomgenerator -run TestAnalyzerPreservesLockfileComponentAndWarningEvidence -count=1`
proves `sbom.component` facts preserve lockfile ecosystem, evidence source,
relative lockfile path, dependency scope/type, and extraction reason, and
`sbom.warning` facts preserve malformed lockfile evidence without leaking a raw
repository root.

No-Regression Evidence:
`go test ./internal/collector/scannerworker/sbomgenerator -run TestAnalyzerAggregatesRepeatedComponentMissingIdentityWarnings -count=1`
failed before repeated `component_missing_identity` occurrences collapsed into
one aggregate warning fact, then passed. The emitted warning preserves
`occurrence_count`, bounded `sample_component_indexes`, and a stable warning
key while the claim still validates as scanner-worker source facts only.

Benchmark Evidence: Apple M4 Pro, `go test ./internal/collector/scannerworker/sbomgenerator -run '^$' -bench BenchmarkAnalyzerAggregatedMissingIdentityWarnings -benchmem -benchtime=2s -count=1`.
Input shape is one valid component plus 10,000 components missing PURL and
`name@version` identity. Terminal emitted facts per operation: one
`sbom.document`, one `sbom.component`, and one aggregate `sbom.warning`.
Result: `BenchmarkAnalyzerAggregatedMissingIdentityWarnings-12` ran 2602
iterations at `907289 ns/op`, `3187948 B/op`, and `294 allocs/op`.

Reducer Path Evidence:
`go test ./internal/reducer -run 'TestScannerWorkerGeneratedSBOMFactsAdmittedByReducerAttachment' -count=1`
proves the analyzer-emitted document and component facts feed
`reducer.BuildSBOMAttestationAttachmentDecisions` and produce
`attached_parse_only` when a subject digest is present and
`unknown_subject` when it is not. It also proves lockfile component evidence
and malformed lockfile warning summaries flow through reducer attachment
without letting scanner workers short-circuit attachment truth.

Reducer Path Evidence:
`go test ./internal/reducer -run TestScannerWorkerGeneratedSBOMAggregatedWarningCountFlowsThroughReducerAttachment -count=1`
proves aggregate `component_missing_identity` warning counts flow through
`reducer.BuildSBOMAttestationAttachmentDecisions` into
`WarningSummaryCount` without expanding raw warning summaries.

Observability Evidence: this package reuses `scanner_worker.*` claim metrics
and spans from `internal/telemetry`. The `Inventory.ResourceUsage` field feeds
`eshu_dp_scanner_worker_cpu_seconds` and
`eshu_dp_scanner_worker_memory_bytes`; the analyzer adds no new metric
instrument, span, log key, queue, reducer lane, graph write, or runtime
configuration knob.

Resource Contract Evidence: the analyzer enforces the same
`max_files`, `max_input_bytes`, and `max_facts` from
`scannerworker.DefaultResourceLimits(AnalyzerSBOMGeneration)` as the rest
of the scanner-worker contract:
`cpu_millis=4000`, `memory_bytes=8 GiB`, `timeout=10m`,
`max_input_bytes=2 GiB`, `max_files=250000`, `max_facts=50000`. CPU,
memory, and timeout enforcement remain runtime concerns; pprof on the
hosted `eshu-scanner-worker` binary continues to back operator
investigation of those budgets.

## Related docs

- `docs/public/reference/security-intelligence.md`
- `docs/public/reference/collector-reducer-readiness.md`
- `docs/public/reference/telemetry/metrics-ingestion-collectors.md`
- `internal/collector/scannerworker/README.md`
- `internal/collector/sbomruntime/README.md`
