# Incident Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the incident fact family:
`IncidentRecord`, `LifecycleEvent`, `ChangeRecord`, `AppliedPagerDutyResource`,
`AppliedAlertRoute`, `ObservedPagerDutyService`, `ObservedPagerDutyIntegration`,
and `CoverageWarning`. It must remain independent from Eshu internals.

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

## Dotted-kind rule

These are the first DOTTED fact kinds in the module. The `FactKind*` constants
(parent `decode.go`) and the schema filenames match the wire kind byte-for-byte,
dots included (`incident.record` -> `incident.record.v1.schema.json`). Never
rename a dotted incident kind to underscores, and never add dotting to a kind
whose wire string does not already carry it. The reducer-side
`TestFactSchemaKindsMatchWireFactKinds` asserts each constant equals its
`go/internal/facts.*FactKind` counterpart.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by the
  flat-struct convention required fields are also non-pointer, and optional
  fields are pointers or slices carrying `omitempty`. Both the schema generator
  (`../../internal/schemagen`) and the decode seam's required-field check
  (`../../decode.go`) derive that set reflectively from the struct's own tags via
  `../../fields.go`, so there is no hand-maintained key list.
  `TestDerivedKeySetsMatchGeneratedSchemas`, `TestPayloadStructShapeConvention`,
  and `TestSchemasHaveNoDrift` lock it.
- Ground the required set in the ACTUAL collector emitter, not intuition. A
  field is required only where the emitter emits it unconditionally. The
  emitters are `go/internal/collector/pagerduty/envelope.go` and
  `config_envelope.go` and `go/internal/collector/terraformstate/pagerduty_applied.go`.
  Making a conditionally-emitted field required would dead-letter valid facts.
- `CoverageWarning` is emitted by TWO emitters (Terraform-state and live
  PagerDuty) with different base payloads. Its required set is the INTERSECTION
  of what both emit; do not promote a Terraform-only-or-live-only field to
  required.
- `ClassificationInputInvalid` is the parent `factschema` package's own constant
  (`decode.go`). A reducer handler receiving it must dead-letter the fact rather
  than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `../../decode.go`), not a silent edit here.
- No struct here carries an `Attributes` pass-through — every incident kind is
  fully typed with a closed set of named fields (nested `ServiceReference` and
  `ChangeLink` sub-objects are also fully typed). If a new incident payload shape
  genuinely needs a pass-through, that is a design discussion, not a local edit.
- Some fields (`resource_class`, `backend_kind`, `locator_hash`,
  `state_generation_id`, `provider_incident_id`, `service_id`, `provider`,
  `declared_match_state`, `redaction_state`, `provider_object_id`,
  `name_fingerprint`) are read by raw-SQL-JSONB loaders in
  `go/internal/storage/postgres` that the #4573 payload-usage manifest gate
  cannot see. Keep them declared here; the reducer-side
  `TestIncidentRoutingSQLProjectedFieldsAreSchemaDeclared` fails the build if one
  is dropped.
- This package defines eight fact kinds. Adding a ninth kind or a `v2` major is
  follow-on epic work, not a casual edit.
