# SBOM/Attestation Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the `sbom_attestation` fact family:
`Document`, `Component`, `DependencyRelationship`, `ExternalReference`,
`Warning`, `Statement`, `SignatureVerification`, and `SLSAProvenance`. It must
remain independent from Eshu internals.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing any payload struct's fields, run `go generate ./...` from
  the module root and commit the regenerated schema under `../../schema/`.
- Run `go test ./... -count=1` from the module root (`sdk/go/factschema`),
  `gofmt` on changed Go files, and `git diff --check` from the repo root.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by
  the flat-struct convention required fields are also non-pointer, and
  optional fields are pointers or slices carrying `omitempty`. Both the
  schema generator (`../../internal/schemagen`) and the decode seam's
  required-field check (`../../decode.go`) derive that set reflectively from
  the struct's own tags via `../../fields.go`, so there is no hand-maintained
  key list to keep in sync.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). A reducer handler receiving it must dead-letter the
  fact rather than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `../../decode.go`), not a silent edit here.
- **No struct here carries an Attributes pass-through.** Unlike
  `awsv1.Resource`/`gcpv1.Resource`, no sbom/attestation kind is a
  polymorphic multi-shape envelope — each kind has one fixed field set on
  both collector paths (`go/internal/collector/sbomdocument`,
  `go/internal/collector/sbomruntime`). Do not add one without discussing
  scope first.
- **`Warning` has ZERO required fields on purpose.** Two distinct collector
  paths emit `sbom.warning` with two distinct, mutually-exclusive identity
  keys: the SBOM document collector always sets `document_id` and never
  `statement_id`; the attestation runtime collector always sets
  `statement_id` and never `document_id`. Neither key can be required
  without dead-lettering half of this kind's real traffic. Do not make either
  field required without re-verifying both collector paths still hold this
  invariant.
- **Required identity fields per kind** (all others optional):
  - `Document.DocumentID`, `Component.DocumentID`,
    `DependencyRelationship.DocumentID`, `ExternalReference.DocumentID` — the
    reducer's join key back to (or within) an SBOM document.
  - `Statement.StatementID`, `SignatureVerification.StatementID`,
    `SLSAProvenance.StatementID` — the reducer's join key back to an
    attestation statement.
- **Deferred kinds — typed but not consumed**: `DependencyRelationship`,
  `ExternalReference`, and `SLSAProvenance` have no reducer or storage read
  path today (`SLSAProvenance` additionally has no collector emitter). They
  are typed anyway for join-key consistency across the family; do not remove
  them, and do not add speculative optional fields beyond what an emitter
  already produces (`SLSAProvenance` in particular has no real payload shape
  to derive from yet — keep it minimal).
- This package defines eight fact kinds. Adding a ninth kind or a `v2` major
  is follow-on work, not a casual edit.
