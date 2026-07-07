# Service Catalog Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for four `service_catalog` fact
kinds: `Entity`, `Ownership`, `RepositoryLink`, and `OperationalLink`. It must
remain independent from Eshu internals.

The `service_catalog` registry family has nine fact kinds and is ALREADY
registered and schema-version-admitted (`SchemaVersion: "1.0.0"`,
`AdmissionHook: facts.ValidateSchemaVersion`, see
`specs/fact-kind-registry.v1.yaml`). Only the four kinds above are typed
here. The other five (`service_catalog.dependency`, `service_catalog.api_link`,
`service_catalog.scorecard_definition`, `service_catalog.scorecard_result`,
`service_catalog.warning`) are intentionally NOT typed this wave — no reducer
index builder or query loader reads their payload fields today, so typing
them would create a `Decode*` no read path calls (a hollow
"typed-kind-read-raw" contract). Do NOT add structs, schemas, or Decode
functions for those five until the change that converts their read-side
consumer; they migrate WITH that surface (Contract System v1 §7).

`OperationalLink` is atypical: no reducer decode call reads it. It is decoded
by the query-layer incident-context read model
(`go/internal/query/incident_context_runtime_sql.go` fetches the fact,
`incident_context_runtime_store.go` `decodeIncidentServiceCatalogOperationalLink`
shapes it, through the `go/internal/query/factschema_decode_incident.go`
`decodeServiceCatalogOperationalLink` seam, #4794 W2a). That query-layer seam
is covered by the merged reducer+query payload-usage manifest gate. It carries
NO required field, so the decode never dead-letters this kind on a missing
field, only on an unsupported schema major.

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
- This family is ALREADY registered and schema-version-admitted. A struct
  change here is additive-only (filling `payload_schema_overrides`); it MUST
  NOT change `admission_hook`, `schema_version`, or `truth_profile` in
  `specs/fact-kind-registry.v1.yaml` for the `service_catalog` family.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by the
  flat-struct convention required fields are also non-pointer, and optional
  fields are pointers carrying `omitempty`. Both the schema generator
  (`../../internal/schemagen`) and the decode seam's required-field check
  (`../../decode.go`) derive that set reflectively from the struct's own tags
  via `../../fields.go`, so there is no hand-maintained key list to keep in
  sync. `TestDerivedKeySetsMatchGeneratedSchemas` locks the two derivations to
  the generated schema, `TestPayloadStructShapeConvention` enforces the
  flat-struct convention, and `TestSchemasHaveNoDrift` keeps every checked-in
  schema in lockstep with its struct.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). A reducer handler receiving it must dead-letter the
  fact rather than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `../../decode.go`), not a silent edit here.
- `EntityRef` is the ONLY required field on `Entity`, `Ownership`, and
  `RepositoryLink` — it is the reducer correlation index's join key
  (`go/internal/reducer/service_catalog_correlation_index.go`). Do NOT make
  `Provider` required: a blank provider is a legitimate single-provider
  catalog deployment's observation, matching the pre-migration
  `payloadString` read it replaces.
- Do NOT make any of `RepositoryLink`'s repository-identifying fields
  (`RepositoryID`, `RepoID`, `NormalizedURL`, `RepositoryURL`, `RawURL`,
  `URL`, `RepositoryName`) required. A link carrying none of them is a valid
  "name-only" catalog claim the reducer classifies as
  `ServiceCatalogCorrelationRejected` — a correlation OUTCOME, not a decode
  failure. Requiring any of them would turn that intentional business
  decision into an incorrect input_invalid dead-letter.
- `OperationalLink` carries NO required field. Its query-layer read site
  (`go/internal/query/incident_context_runtime_store.go`
  `decodeIncidentServiceCatalogOperationalLink`, via the
  `decodeServiceCatalogOperationalLink` seam) derefs every optional pointer
  field to `""`/nil on absence, so an absent key is a valid empty value.
  Adding a required field here would make that read path dead-letter a fact
  the collector legitimately emits without the key.
- None of the four structs here carry a polymorphic `Attributes
  map[string]any` pass-through — every kind is flat and fully closed. Do not
  add one without discussing scope; it would be a first for this family.
- This package defines four fact kinds. Typing one of the five deferred
  service_catalog kinds (see the top of this file), a tenth kind, or a `v2`
  major is follow-on work gated on converting the read path, not a casual
  edit.
