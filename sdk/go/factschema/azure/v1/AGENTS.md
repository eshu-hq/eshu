# Azure Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the four wired/consumer-less `azure`
fact kinds: `CloudResource`, `CloudRelationship`, `DNSRecord`, and
`CollectionWarning`, plus `TagObservation`, `IdentityObservation`,
`ResourceChange`, and `ImageReference`. It must remain independent from Eshu
internals.

The Azure family has eight fact kinds, and this directory now types all eight.
Shared cross-provider reducers still own tag and image semantics, but the
collector emit path and decode seam must use the typed structs so missing
required fields surface as classified `input_invalid` errors.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing any payload struct's fields, run `go generate ./...` from
  the module root and commit the regenerated schema under `../../schema/`
  AND its copy under `../../fixturepack/schema/`
  (`TestFixturePackSchemasMatchCanonical` locks the two).
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
  flat-struct convention (every slice/map field MUST carry `omitempty`, even
  one the collector always populates in practice), and
  `TestSchemasHaveNoDrift` keeps every checked-in schema in lockstep with its
  struct.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). A reducer handler receiving it must dead-letter the
  fact rather than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `../../decode.go`), not a silent edit here.
- `CloudResource` and `CloudRelationship` are polymorphic envelopes: type and
  validate only the shared identity and common fields; every remaining key
  stays in `Attributes map[string]any`, untyped, on purpose. UNLIKE the aws
  family, the Azure collector emitter writes its remaining fields FLAT at the
  top level (no nested `"attributes"` object), so a reducer consumer reaches
  one directly at `Attributes[key]` — there is no `payloadAttributes(...)`
  unwrap helper for this family. Do not add a per-resource-type field here
  casually; typing `Attributes` contents per `resource_type`/
  `relationship_type` is a distinct, larger increment (design §7), not
  required by this migration's identity-accuracy goal.
- If you add a named field to `CloudResource` or `CloudRelationship`, add its
  JSON tag to `cloudResourceKnownKeys` / `cloudRelationshipKnownKeys` in the
  same change (in `resource.go` / `relationship.go`). Forgetting this leaks
  the new field into `Attributes` as well as the named struct field, which is
  silently wrong, not a compile error.
- `DNSRecord`, `CollectionWarning`, `TagObservation`, `IdentityObservation`,
  `ResourceChange`, and `ImageReference` have no `Attributes` pass-through;
  every payload key they care about is a named field. Do not add one without
  discussing scope — it changes this package's polymorphic-vs-fully-typed shape
  for that kind.
- This package defines eight fact kinds (`azure_cloud_resource`,
  `azure_cloud_relationship`, `azure_dns_record`, `azure_collection_warning`,
  `azure_tag_observation`, `azure_identity_observation`,
  `azure_resource_change`, and `azure_image_reference`). Adding a ninth kind or
  a `v2` major is follow-on epic work, not a casual edit.
