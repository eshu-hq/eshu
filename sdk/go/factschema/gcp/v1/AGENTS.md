# GCP Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the `gcp` fact family: `Resource`,
`Relationship`, `CollectionWarning`, `DNSRecord`, `IAMPolicyObservation`,
`TagObservation`, and `ImageReference`. It must remain independent from Eshu
internals.

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
  only the shared identity and common fields; every remaining unmodeled key
  stays in `Attributes map[string]any`, untyped, on purpose. `Resource`'s
  nested `"attributes"` bounded typed-depth map lands at
  `Attributes["attributes"][key]`, reached via the reducer's
  `payloadAttributes(...)` helper — never flat `Attributes[key]`. Do not add
  a per-asset-type field here casually; typing `Attributes` contents is
  deferred follow-up work (design §7), mirroring the AWS deferral (issue
  #4631); see the package `README.md`.
- If you add a named field to `Resource` or `Relationship`, add its JSON tag
  to `resourceKnownKeys` / `relationshipKnownKeys` in the same change (in
  `resource.go` / `relationship.go`). Forgetting this leaks the new field
  into `Attributes` as well as the named struct field, which is silently
  wrong, not a compile error.
- `CollectionWarning`, `DNSRecord`, `IAMPolicyObservation`, `TagObservation`,
  and `ImageReference` have no `Attributes` pass-through; every payload key
  they care about is a named field. Do not add one without discussing scope —
  it changes this package's polymorphic-vs-fully-typed shape for that kind.
- **Family boundary**: `gcp_iam_principal`, `gcp_iam_trust_policy`, and
  `gcp_iam_permission_policy` belong to the `secrets_iam` fact family
  (`go/internal/facts/secrets_iam.go`), NOT this package. Do not add structs
  for those kinds here; they are a different family's scope.
- This package defines seven fact kinds (`gcp_cloud_resource`,
  `gcp_cloud_relationship`, `gcp_collection_warning`, `gcp_dns_record`,
  `gcp_iam_policy_observation`, `gcp_tag_observation`, and
  `gcp_image_reference`). Adding an eighth kind or a `v2` major is follow-on
  epic work, not a casual edit.
- `Resource`'s schema version is pinned at 1.1.0
  (`facts.GCPCloudResourceSchemaVersion` in `go/internal/facts/gcp.go`), one
  minor ahead of the rest of this family. Generate its schema artifact at
  1.1.0; the decode seam still dispatches on schema-version MAJOR only, so no
  separate decode path is needed for the minor difference.
