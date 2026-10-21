# Serverless Extractors

## Purpose

The serverless extractor group owns typed-depth extraction for GCP serverless
and application-platform CAI asset types: App Engine, Cloud Functions (gen1
and gen2), Cloud Scheduler, Cloud Tasks, Composer, Eventarc, Cloud Run, and
Workflows. It is the first group moved out of the flat `gcpcloud` root under
issue #6696; the compute, data, security, and platform groups follow the same
shape.

## Ownership boundary

Owns one extractor file per serverless asset type plus its unit and offline
fixture tests. Does not own parsing, normalization, redaction, envelope
construction, generation fencing, metrics, or transport — those stay in
`gcpcloud` and `gcpruntime`. Does not call Google Cloud APIs.

## Exported surface

None. Registration is init-driven: each file calls
`extractor.RegisterAssetExtractor` from its init function, and the collector
binary imports this package for its side effects.

See `doc.go` for the full contract.

## Dependencies

- `../` (the extractor leaf) — registry, shared shapes, and shared helpers
- `internal/collector/access/posture` — keyed fingerprinting for IAM members
  and container names

## Telemetry

None. Extraction outcomes remain covered by the collector-local
`eshu_dp_gcp_cloud_attribute_extractions_total` and
`eshu_dp_gcp_cloud_facts_emitted_total` counters, recorded by the
`gcpcloud` dispatch path.

## Gotchas / invariants

- A file name never repeats its directory name. File names keep the resource
  word (`cloud_function.go`, `run_service.go`) because the directory carries
  no prefix to drop; `app_engine_service.go` and `run_service.go` both exist
  and must stay distinct.
- This package must never import the parent `gcpcloud` package. Internal
  (same-package) tests cover extractor behavior; the external
  `serverless_test` package covers the offline end-to-end fixtures through
  the parent pipeline. The external tests pin relationship types and full
  resource names as literals so they assert values, not constants.
- If you add an asset type here, register it from init, add unit and fixture
  coverage, and update the extractor catalog in
  `../../AGENTS-extractors-2.md`. Then run
  `scripts/verify-package-docs.sh`.

## Related docs

- `../README.md` — the shared extractor foundation
- `../../AGENTS.md` — the gcpcloud source-fact contract and invariants
- `docs/public/reference/gcp-cloud-collector-contract.md`
