# AGENTS.md - scannerworker/v1 contracts

This package owns schema-version-1 payload structs for the two
`scanner_worker` source facts: `scanner_worker.analysis` (emitted by the image
analyzer) and `scanner_worker.warning` (emitted by both the image analyzer and
the `WarningAnalyzer` fallback in `go/internal/collector/scannerworker`). The
`Warning` struct keeps the image-analysis fields optional so the fallback, which
has no image evidence, stays in lockstep with the contract.

Rules:

- Keep this module standalone: do not import `go/internal/...`.
- Payload structs are flat, exported, and typed. Required fields are
  non-pointer fields without `omitempty`; optional fields are pointers with
  `omitempty`.
- Regenerate `../../schema/*.json`, refresh `../../fixturepack/schema/`, and
  update fixture-pack payloads whenever these structs change.
- Keep registry refs in `specs/fact-kind-registry.v1.yaml` aligned with the
  generated schema files.
