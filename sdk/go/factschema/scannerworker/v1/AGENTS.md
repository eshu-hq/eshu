# AGENTS.md - scannerworker/v1 contracts

This package owns schema-version-1 payload structs for the two
`scanner_worker` source facts: `scanner_worker.analysis` (emitted by the
satellite image analyzer) and `scanner_worker.warning` (emitted by both the
image analyzer and the satellite warning fallback). The `Warning` struct keeps
the image-analysis fields optional so the fallback, which has no image
evidence, stays in lockstep with the contract.

`Warning.RepositoryID` and `Warning.RelativePath` are optional scan-target
fields (core eshu-hq/eshu#10043). Keep them optional: image and SBOM warnings
carry neither. `RelativePath` is the go.mod FILE path in slash form, byte-equal
to the `relative_path` on that file's `go_module_evidence` rows; do not turn it
back into a module path, which cannot join a finding keyed by the dependency.
The failure class lives in `reason`; `warning_class` stays the constant
`scanner_worker_warning`. `EncodeScannerWorkerWarning` in
`../../decode_scannerworker.go` is hand-written, so a new optional field needs
an `addOptionalString` line there as well as the struct field;
`TestEncodeScannerWorkerWarningWritesEveryField` fails when one is missing.

Rules:

- Keep this module standalone: do not import `go/internal/...`.
- Payload structs are flat, exported, and typed. Required fields are
  non-pointer fields without `omitempty`; optional fields are pointers with
  `omitempty`.
- Regenerate `../../schema/*.json`, refresh `../../fixturepack/schema/`, and
  update fixture-pack payloads whenever these structs change.
- Keep registry refs in `specs/fact-kind-registry.v1.yaml` aligned with the
  generated schema files.
