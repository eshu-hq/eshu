# AWS Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the `aws` fact family. It must
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

- A field is required exactly when its json tag carries no `omitempty`; by the
  flat-struct convention required fields are also non-pointer, and optional
  fields are pointers or slices/maps, carrying `omitempty`. Both the schema
  generator (`../../internal/schemagen`) and the decode seam's required-field
  check (`../../decode.go`) derive that set reflectively from the struct's own
  tags via `../../fields.go`, so there is no hand-maintained key list to keep
  in sync. `TestDerivedKeySetsMatchGeneratedSchemas` locks the two derivations
  to the generated schema, `TestPayloadStructShapeConvention` enforces the
  flat-struct convention, and `TestSchemasHaveNoDrift` keeps every checked-in
  schema in lockstep with its struct.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). A reducer handler receiving it must dead-letter the
  fact rather than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `../../decode.go`), not a silent edit here.
- `Resource` and `Relationship` are polymorphic envelopes: type and validate
  only the shared identity and common fields; every remaining
  service-/verb-specific key stays in `Attributes map[string]any`, untyped,
  on purpose. The pass-through is nested: the collector nests service-specific
  fields one level deep under a single `"attributes"` payload key, so an
  UNTYPED field is reached at `Attributes["attributes"][key]`, via the
  reducer's own `payloadAttributes(...)` helper — never flat
  `Attributes[key]`.
- `attribute_shapes.go` types the BOUNDED SUBSET of that pass-through a
  consumer actually reads (issue #4631): a small resource_type/
  relationship_type-keyed catalog plus two resource-type-agnostic anchor
  shapes, each with a validating `Decode<Resource|Relationship><Shape>`
  accessor. A reducer consumer that reads one of the already-typed shapes
  MUST call its accessor, never a raw map lookup, and MUST route a non-nil
  `*AttributeShapeError` through the same input_invalid dead-letter path a
  missing required identity field already uses. Do not add a new
  per-resource-type shape here casually — only when a real consumer needs the
  field. Typing the remaining, not-yet-consumed `Attributes` contents ahead of
  a consumer is tracked as separate follow-up work (design §7); see the
  package `README.md`.
- If you add a named field to `Resource` or `Relationship`, add its JSON tag
  to `resourceKnownKeys` / `relationshipKnownKeys` in the same change (in
  `resource.go` / `relationship.go`). Forgetting this leaks the new field
  into `Attributes` as well as the named struct field, which is silently
  wrong, not a compile error.
- Non-polymorphic structs such as `DNSRecord`, `ImageReference`,
  `SecurityGroupRule`, `Warning`, `EC2InstancePosture`,
  `RDSInstancePosture`, `S3BucketPosture`, and `S3ExternalPrincipalGrant`
  have no `Attributes` pass-through; every payload key they care about is a
  named field. Do not add one without discussing scope — it changes this
  package's polymorphic-vs-fully-typed shape for that kind.
- This package defines the AWS fact kinds wired through `decode_aws.go`.
  Adding a new kind or a `v2` major is follow-on epic work, not a casual edit.
