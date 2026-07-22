# Terraform State Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the `terraform_state` fact family:
`Snapshot`, `Resource`, `Module`, `Output`, `TagObservation`, `Candidate`,
`ProviderBinding`, and `Warning`. It must remain independent from Eshu
internals.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing any payload struct's fields, run `go generate ./...` from the
  module root and commit the regenerated schema under `../../schema/` AND the
  embedded fixture-pack copy under `../../fixturepack/schema/` (the drift-lock
  test `TestFixturePackSchemasMatchCanonical` compares the two).
- Run `go test ./... -count=1` from the module root (`sdk/go/factschema`),
  `gofumpt` on changed Go files, and `git diff --check` from the repo root.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by the
  flat-struct convention required fields are also non-pointer, and optional
  fields are pointers or slices/maps carrying `omitempty`. Both the schema
  generator (`../../internal/schemagen`) and the decode seam's required-field
  check (`../../decode.go`) derive that set reflectively from the struct's own
  tags via `../../fields.go`, so there is no hand-maintained key list to keep
  in sync. `TestDerivedKeySetsMatchGeneratedSchemas`,
  `TestPayloadStructShapeConvention`, and `TestSchemasHaveNoDrift` lock the
  derivations together.
- **Required set = today's identity/join gate only.** Mark a field required
  ONLY when its ABSENCE produces a broken or empty graph identity in the
  projector's current read path (`go/internal/projector/tfstate_canonical.go`).
  A field the projector tolerates empty must stay OPTIONAL. Flipping a
  present-but-empty value into a dead-letter is an ACCURACY REGRESSION the
  contract forbids: only an ABSENT key (or explicit null) dead-letters; a
  present-but-empty value is a valid decode. See `doc.go` for the per-kind
  required set and its justification.
- `Snapshot` deliberately has NO required field: the projector reads every
  snapshot field best-effort and tolerates any being empty, so no snapshot
  field's absence breaks an identity. Do not add a required field to `Snapshot`
  to satisfy a test — that would dead-letter a today-valid incomplete snapshot.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). A consumer receiving it must dead-letter the fact
  rather than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `../../decode.go`), not a silent edit here.
- These structs are fully typed (no `Attributes map[string]any` pass-through).
  A polymorphic payload key that no consumer reads (for example an output's
  raw `value`, a tag's `tag_key`/`tag_value` classification envelopes) is
  intentionally NOT modeled: the open schema (`additionalProperties: true`)
  permits the key and the decode seam preserves it in the envelope, so a future
  consumer models it when it needs to. Do not add a bare optional `any` field —
  `TestPayloadStructShapeConvention` bans it.

## Consumed vs deferred

- **Consumed today** (decode through the seam on the projector read path):
  `Snapshot`, `Resource`, `Module`, `Output`, `TagObservation`, and
  `ProviderBinding` (gained its projector consumer,
  `terraformStateProviderBindingsByResource`, in #5446).
- **Typed-but-not-yet-consumed**: `Candidate` and `Warning` have no read-side
  decode consumer in the current codebase. They ship a struct, schema, and
  fixture pack so the contract is ready, but their decode-site conversion,
  regression test, and benchmark land in the change that first reads each kind,
  matching how the GCP family typed `gcp_image_reference` /
  `gcp_tag_observation` ahead of their shared consumer. Do not add a decode
  site for them here.

## Family boundary

- This package defines eight fact kinds. Adding a ninth kind or a `v2` major is
  follow-on epic work, not a casual edit. The kinds and their wire strings are
  the `facts.TerraformState*FactKind` constants in
  `go/internal/facts/tfstate.go`; the parent package's `FactKind*` constants
  MUST stay byte-equal to them (the reducer-side drift lock
  `TestFactSchemaKindsMatchWireFactKinds` asserts it).
