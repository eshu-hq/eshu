# internal/facts Agent Instructions

These rules are mandatory for this package. Root `AGENTS.md` still owns the
repo-wide proof, performance, concurrency, and skill-routing rules.

## Read First

1. `README.md` and `doc.go`.
2. `models.go`, `stableid.go`, and `source_confidence.go`.
3. The fact-family file you touch, such as `tfstate.go`,
   `package_registry.go`, `oci_registry.go`, `aws.go`, `ci_cd_run.go`,
   `sbom_attestation.go`, `vulnerability_intelligence.go`,
   `service_catalog.go`, or `documentation.go`.
4. `go/internal/storage/postgres/README.md` before changing persisted shapes.

## Local Rules

- Keep this package standard-library-only and storage-neutral.
- Treat `Envelope`, `Ref`, fact kind strings, schema versions, stable IDs, and
  source-confidence values as on-disk contracts.
- New fields must be additive and backward-compatible. Convenience fields for a
  single caller belong outside this package.
- Clone envelopes before replaying, branching, or passing payloads to code that
  may mutate nested maps or slices.
- Stable ID inputs must be deterministic, JSON-serializable identity maps. Do
  not include credentials, raw source content, timestamps that are not identity,
  or mutable high-cardinality values.
- `FencingToken` must travel with envelopes so stale workers cannot overwrite
  newer generation data.
- Terraform state, registry, cloud, SBOM, vulnerability, documentation,
  service-catalog, and CI/CD facts are evidence. Reducers decide graph truth.

## Change Gates

- New fact families require kind constants, schema-version helpers, stable ID
  tests, source-confidence validation, and storage/parser/collector consumers in
  the same PR.
- Renames or removals require a storage compatibility and migration plan.
- Any fact payload shape used by API/MCP or reducer truth needs fixture proof
  from source fact through reducer/query behavior.

## Do Not Change Without Owner Review

- Existing fact kind strings or schema versions.
- `Envelope` persisted field semantics.
- `StableID` identity behavior.
- Source-confidence vocabulary.
