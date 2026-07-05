# Work Item Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the work-item fact family:
`WorkItemRecord`, `WorkItemTransition`, `WorkItemExternalLink`,
`WorkItemProjectMetadata`, `WorkItemIssueTypeMetadata`,
`WorkItemStatusMetadata`, `WorkItemWorkflowMetadata`, `WorkItemFieldMetadata`,
and `WorkItemMetadataWarning`. It must remain independent from Eshu internals.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing any payload struct's fields, run `go generate ./...` from the
  module root and commit the regenerated schema under `../../schema/` AND the
  mirrored copy under `../../fixturepack/schema/` (the fixture-pack drift test
  fails on divergence).
- Run `go test ./... -count=1` from the module root (`sdk/go/factschema`),
  `gofmt` on changed Go files, and `git diff --check` from the repo root.

## Decode site is the query layer, not the reducer

No reducer or projector domain decodes `work_item.*` payloads. The decode
sites are `go/internal/query/factschema_decode_workitem.go` (the typed seam
wrapper the #4573 payload-usage manifest gate's `QueryDir` input scans),
`work_item_evidence_store.go`/`work_item_evidence.go`, and
`incident_context_review_store.go`. When you change a struct's shape here,
check those query-layer files for a required-field regression before
assuming the reducer-side tests are the only signal.

## Dotted-kind rule

These fact kinds are DOTTED, matching the incident/kubernetes_live/
oci_registry/package_registry/sbom_attestation convention. The `FactKind*`
constants (parent `decode.go`) and the schema filenames match the wire kind
byte-for-byte, dots included (`work_item.record` ->
`work_item.record.v1.schema.json`). Never rename a dotted work-item kind to
underscores, and never add dotting to a kind whose wire string does not
already carry it. The reducer-side `TestFactSchemaKindsMatchWireFactKinds`
asserts each constant equals its `go/internal/facts.*FactKind` counterpart.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by
  the flat-struct convention required fields are also non-pointer, and
  optional fields are pointers or slices carrying `omitempty`. Both the schema
  generator (`../../internal/schemagen`) and the decode seam's required-field
  check (`../../decode.go`) derive that set reflectively from the struct's own
  tags via `../../fields.go`, so there is no hand-maintained key list.
  `TestDerivedKeySetsMatchGeneratedSchemas`, `TestPayloadStructShapeConvention`,
  and `TestSchemasHaveNoDrift` lock it.
- Ground the required set in the ACTUAL collector emitter, not intuition. A
  field is required only where the emitter's identity guard makes it
  unconditional. The emitter is
  `go/internal/collector/jira/envelope.go`/`envelope_metadata.go`. Making a
  conditionally-emitted or alternate-anchor field required would dead-letter
  valid facts — see each struct's godoc for the specific guard it reflects.
- `WorkItemFieldMetadata` is the one kind in this family where the Go-level
  emitter guard (a non-blank field id) does NOT promote to a required payload
  key: the payload's own `field_id` is always redacted to `""`, so only
  `provider` is required. Do not "fix" this by requiring `field_id` — that
  dead-letters every valid field-metadata fact.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). A decode-site caller receiving it must dead-letter
  the fact rather than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `../../decode.go`), not a silent edit here.
- Redacted fields (summary, title, url, project_name, workflow/transition
  names, field name/description, self_url, and their user/id counterparts)
  MUST stay declared in the structs even though the collector always emits
  them as `""` — dropping them narrows the schema relative to what the
  collector emits, which is itself a contract violation. Only their sibling
  `_present`/`_fingerprint` fields carry real signal.
- `WorkItemWorkflowMetadata.Statuses`/`Transitions` are nested typed lists
  (`WorkItemWorkflowStatus`, `WorkItemWorkflowTransition`). Every field on
  those nested types is optional because they are list elements, not the
  top-level fact identity.
- This package defines nine fact kinds, all from a single collector (Jira).
  Adding a tenth kind or a `v2` major is follow-on work, not a casual edit.
