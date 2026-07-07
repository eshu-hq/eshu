# Service Catalog Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the
`service_catalog` fact family. A reducer handler never reads
`Envelope.Payload["some_key"]` for these kinds directly; it decodes through
the parent `factschema` package's kind-keyed seam (for example
`factschema.DecodeServiceCatalogEntity`) and receives one of these structs,
validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/servicecatalog/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

Four `service_catalog` fact kinds decode through this package:

| Fact kind | Struct | Decode function | Read by |
| --- | --- | --- | --- |
| `service_catalog.entity` | `Entity` | `factschema.DecodeServiceCatalogEntity` | reducer correlation index |
| `service_catalog.ownership` | `Ownership` | `factschema.DecodeServiceCatalogOwnership` | reducer correlation index |
| `service_catalog.repository_link` | `RepositoryLink` | `factschema.DecodeServiceCatalogRepositoryLink` | reducer correlation index |
| `service_catalog.operational_link` | `OperationalLink` | `factschema.DecodeServiceCatalogOperationalLink` | query-layer incident-context read model (`go/internal/query`, #4794 W2a) |

The `service_catalog` registry family is ALREADY registered and
schema-version-admitted (`SchemaVersion: "1.0.0"`,
`AdmissionHook: facts.ValidateSchemaVersion`, see
`specs/fact-kind-registry.v1.yaml`) — unlike the `codegraph` family (Wave 4f
S1). This package's migration only fills the registry's
`payload_schema_overrides` for the kinds a real consumer decodes; it makes no
admission-behavior change.

The registry family has nine fact kinds. This package types only the four
above. The remaining five — `service_catalog.dependency`,
`service_catalog.api_link`, `service_catalog.scorecard_definition`,
`service_catalog.scorecard_result`, and `service_catalog.warning` — are
loaded into the correlation handler's fact batch
(`serviceCatalogCorrelationFactKinds`) but no reducer index builder or query
loader reads their payload fields today. Typing them here would create a
`Decode*` the real read path never calls. They migrate WITH the surface that
reads them, per Contract System v1 §7.

## `OperationalLink` is a query-layer-only kind

`OperationalLink` is not read by any reducer decode call. It is read by the
query-layer incident-context read model: `go/internal/query/incident_context_runtime_sql.go`'s
`listIncidentServiceCatalogOperationalLinksQuery` fetches the fact, and
`go/internal/query/incident_context_runtime_store.go`'s
`decodeIncidentServiceCatalogOperationalLink` decodes it through
`go/internal/query/factschema_decode_incident.go`'s
`decodeServiceCatalogOperationalLink` seam (#4794 W2a). That query-layer seam
is gated by the merged reducer+query payload-usage manifest
(`go/internal/payloadusage`'s `resolveQueryDecodeFiles` glob), which is why
`FactKindServiceCatalogOperationalLink` is mapped in
`go/internal/payloadusage/schema.go`'s `factKindSchemaFile`. No field on this
struct is required — the decode seam never dead-letters this kind on a
missing field, only on an unsupported schema major.

## Ownership boundary

This package owns the Go type definitions for these four fact kinds'
payloads. It does not own decode dispatch, schema-version routing, or
required-field validation — that lives in the parent `factschema` package
(`decode.go`, `decode_servicecatalog.go`). It does not own graph/correlation
projection; the reducer's `service_catalog_correlation*.go` handlers consume
the decoded structs but live outside this module. It does not own the
collector emitters that build these payloads
(`go/internal/collector/servicecatalog` or equivalent provider adapters),
which also live outside this module.

## Exported surface

`Entity`, `Ownership`, `RepositoryLink`, `OperationalLink`. See each struct's
godoc comment for its full field list; the required/optional split below is
the contract most callers need first.

## Dependencies

Standalone: this package has no imports beyond the standard library implied
by its struct tags (no custom JSON codec — every kind here is a flat,
fully-typed, closed struct). It carries no dependency on `go/internal/...` —
see the module `AGENTS.md` for the rule.

## Required vs. optional fields, per struct

Field mutability encodes the contract, per Contract System v1 §3.1
(`docs/internal/design/contract-system-v1.md`):

- **Required**: a non-pointer field with no `omitempty` tag. The decode seam
  rejects a payload that omits a required field, or supplies an explicit JSON
  null for one, with a classified `input_invalid` error naming the field,
  never a zero-value struct. A present-but-empty value (for example the empty
  string) is a valid observed value and decodes normally.
- **Optional**: a pointer field carrying `omitempty`. An absent optional
  field decodes to nil, not a defaulted zero value.

| Struct | Required identity fields | Why |
| --- | --- | --- |
| `Entity` | `EntityRef` | The correlation index's join key (`serviceCatalogEntityFromFact`); the index drops any entity whose `entity_ref` is blank, so a fact missing it carries no usable catalog identity at all. `Provider` participates in the same join key but stays optional: a blank provider is a legitimate single-provider deployment's observation. |
| `Ownership` | `EntityRef` | Same join-key rationale as `Entity`. `OwnerRef`/`OwnerLegacy` (wire keys `owner_ref`/`owner`) both stay optional: the reducer's `firstNonBlank` already tolerates either being absent. |
| `RepositoryLink` | `EntityRef` | Same join-key rationale. Every repository-identifying field (`RepositoryID`, four URL spellings, `RepositoryName`) stays optional: a link carrying none of them is a legitimate "name-only" catalog claim the reducer classifies as `ServiceCatalogCorrelationRejected` — a correlation OUTCOME, not a decode failure. |
| `OperationalLink` | none | The query-layer decode wrapper derefs every optional pointer field to `""`/nil on absence (`workItemDerefString`), matching the pre-typing raw-map lookup's tolerance for an absent key; nothing here gates admission. |

## No Attributes pass-through

None of these four structs carry a polymorphic `Attributes map[string]any`
pass-through: every kind here is a flat, fully-typed, closed schema.

## Changing a struct

Any field change here is a payload-schema change.

- **Additive optional field** (new pointer/`omitempty` field): a minor schema
  bump. Add the field, regenerate, and commit the schema in the same change.
- **Remove, rename, or narrow a field**: a major schema bump. It needs a
  conversion shim in the parent package's decode seam (`decode.go`,
  `decodeLatestMajor`) — see the module `README.md` — never a silent edit
  here.

Regenerate after any struct change:

```bash
cd sdk/go/factschema
go generate ./...
```

`schema_gen_test.go`'s `TestSchemasHaveNoDrift` fails the build on drift. The
decode seam derives its required-field set reflectively from each struct's
tags (`../../fields.go`), so there is no separate map to update;
`TestDerivedKeySetsMatchGeneratedSchemas` fails if that reflective set ever
diverges from the generated schema, and `TestPayloadStructShapeConvention`
rejects a field shape that would make "required" ambiguous. Any fixture pack
copy under `../../fixturepack/schema/` must be refreshed in the same change
(`TestFixturePackSchemasMatchCanonical` locks the two).

## Telemetry

None. This package has no runtime, network, queue, graph, or telemetry
emission path — see the module `README.md`'s no-observability-change note.

## Gotchas / invariants

- `RepositoryLink`'s four URL fields (`NormalizedURL`, `RepositoryURL`,
  `RawURL`, `URL`) are matched by `firstNonBlank` in that preference order —
  never assume only one is populated.
- A `RepositoryLink` carrying only `RepositoryName` (no id, no URL) is valid
  and decodes; the reducer rejects it at the correlation-decision level, not
  at decode time.
- `OperationalLink` has no reducer decode call; it exists only to keep the
  checked-in schema honest against the SQL loader's field reads (see the
  module `AGENTS.md`'s SQL-loader-only-fields note).
- The reducer decodes only the latest struct per fact kind. Older-schema-major
  shims live in the parent package's `decodeLatestMajor`, never here.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §5 versioning, §7 migration plan.
- `docs/internal/contract-system-contributor-summary.md`
- Parent module `README.md` (`sdk/go/factschema/README.md`) — decode seam,
  classified errors, schema generation.
- `go/internal/reducer/service_catalog_correlation_index.go` — the
  correlation index this package's Entity/Ownership/RepositoryLink structs
  feed.
