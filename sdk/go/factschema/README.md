# Fact Schema Contracts

This module defines the versioned collector-to-reducer **payload** contracts
described in Contract System v1 §3.1, "The contracts module"
(`docs/internal/design/contract-system-v1.md`), and its contributor summary
(`docs/internal/contract-system-contributor-summary.md`). It intentionally
does not import `github.com/eshu-hq/eshu/go/internal` packages — the same
constraint `sdk/go/collector` already satisfies. Both collector repositories
and the core reducer depend on this module; they never depend on each other.

The migration is incremental, family by family (Contract System v1 §7,
`docs/internal/design/contract-system-v1.md`). Two families are typed today:

- **aws / iam / security-group** — `aws/v1` and `iam/v1` (underscore kinds).
- **incident** — `incident/v1`: the incident-context and incident-routing kinds
  (`incident.record`, `incident.lifecycle_event`, `change.record`,
  `incident_routing.applied_pagerduty_resource`,
  `incident_routing.applied_alert_route`,
  `incident_routing.observed_pagerduty_service`,
  `incident_routing.observed_pagerduty_integration`,
  `incident_routing.coverage_warning`). This is the first family with **dotted**
  wire kinds; the `FactKind*` constants and schema filenames match the dots the
  collector already emits (for example `incident.record.v1.schema.json`).

## Compatibility

- Go module path: `github.com/eshu-hq/eshu/sdk/go/factschema`
- JSON Schema artifacts: `schema/<kind>.v1.schema.json`, generated with
  [`invopop/jsonschema`](https://github.com/invopop/jsonschema). One artifact
  per typed fact kind; the file name is the wire kind plus `.v1.schema.json`,
  including the dot for a dotted kind.

## Contracts

- `Envelope` — the canonical fact envelope: `fact_kind`, `schema_version`,
  `stable_fact_key`, `scope_id`, `generation_id`, `collector_kind`,
  `source_confidence`, `observed_at`, `is_tombstone`, `source_ref`, and the
  raw `payload` map.
- `aws/v1.Resource`, `iam/v1.Permission`, `incident/v1.IncidentRecord`, and
  their siblings — the typed payload structs, one per fact kind, under each
  family's `<family>/v1` package.
- `Decode<Kind>(env Envelope) (<Struct>, error)` — the kind-keyed decode seam
  (for example `DecodeAWSResource`, `DecodeIncidentRecord`). Reducer handlers
  call this instead of reading `env.Payload["some_key"]` directly.
- `Encode<Kind>(<Struct>) (map[string]any, error)` — the emit-side counterpart
  collectors use to build an `Envelope.Payload` from a typed struct.
- `DecodeError` — the classified error type `decodeAndValidate` returns for
  a malformed or incomplete payload, carrying `Classification` (currently
  always `ClassificationInputInvalid`, `"input_invalid"`) and the missing
  `Field` name.

## Required vs. optional fields

Contract System v1 §3.1 fixes the rule this module follows everywhere:

> Required payload fields are non-pointer struct fields validated on decode.
> Optional fields are pointers or `omitempty`.

`aws/v1.Resource` demonstrates both: `AccountID`, `ResourceID`, `Region`, and
`ResourceType` are non-pointer fields with no `omitempty` tag (required);
`Name` (`*string`) and `Tags` (`*map[string]string`, `omitempty`) are pointers
(optional). The generated schema's `"required"` array lists exactly the four
required fields — `schema_gen_test.go` fails if the struct and the checked-in
schema ever disagree.

`Tags` is a pointer to a map, not a plain map, so the two "empty" states stay
distinct across a round trip: a nil pointer means the collector did not observe
tags (omitted from the payload), while a non-nil pointer to an empty map means
the collector observed zero tags (marshals as `"tags":{}` and round-trips back
to a non-nil empty map). A plain map with `omitempty` could not express
"observed empty" — an empty map would be omitted and decode back as nil.

A required field that is **absent** from `Envelope.Payload`, or present with an
explicit JSON null, decodes to a `*DecodeError` with
`Classification == ClassificationInputInvalid` naming the field. A
present, non-null but empty value (for example the empty string) decodes
successfully, since an empty string is a valid (if unusual) observed value.
Only an absent key or an explicit null is rejected — this is what stops a
missing required identity from silently becoming an empty-string graph node.

## Decode seam and classified errors

```go
resource, err := factschema.DecodeAWSResource(env)
if err != nil {
    var decodeErr *factschema.DecodeError
    if errors.As(err, &decodeErr) {
        // decodeErr.Classification == factschema.ClassificationInputInvalid
        // decodeErr.Field names the missing payload key.
    }
    // dead-letter, never proceed with a zero-value resource.
}
```

`ClassificationInputInvalid` is this module's **own** string constant
(`"input_invalid"`), not an import of `go/internal/projector`'s dead-letter
triage classes — that would violate the no-internal-imports rule below. The
reducer maps this classification to its own triage class by string value.

### Decode options

Kind-specific `Decode*` functions accept variadic `DecodeOption` values.
Passing none preserves the historical decode exactly, so an existing caller is
never affected. The options are opt-in performance knobs, not behavior changes.

`WithoutAttributesRemainder()` is for a **named-field-only** hot caller — one
that reads only named struct fields and never reads a polymorphic struct's
`Attributes` pass-through map. The default decode rebuilds `Attributes` (a fresh
map of every non-named payload key) on every call, and on a wide-`Attributes`
payload that rebuild dominates the decode cost. The option skips it, leaving
`Attributes` nil while every named field decodes identically (issue #4865):

```go
// go/internal/relationships/gcp_evidence.go reads only source/target/type and
// never Relationship.Attributes, so it opts out of the discarded remainder.
rel, err := factschema.DecodeGCPCloudRelationship(env, factschema.WithoutAttributesRemainder())
```

A caller that reads `.Attributes` (for example the reducer's own decode site)
MUST NOT pass this option; it stays on the default full-remainder decode.

## Schema generation

`internal/schemagen` reflects each fact kind's typed struct into a JSON
Schema using `invopop/jsonschema`, with `DoNotReference: true` so a flat
struct inlines directly instead of producing `$defs`/`$ref` indirection.
Regenerate the checked-in artifacts with:

```bash
cd sdk/go/factschema
go generate ./...
```

This must be run, and the result committed, whenever a payload struct's
fields change. `schema_gen_test.go` regenerates the schema in memory on
every `go test` run and fails the build if it no longer matches the
committed artifact — schema drift is a test failure, not something only a
separate CI diff gate would catch.

## Envelope unification (out of scope, documented follow-up)

Eshu currently has **three** separate envelope definitions for the same wire
concept:

1. `factschema.Envelope` (this module),
2. `go/internal/facts.Envelope` (the durable reducer-side representation),
3. `sdk/go/collector.Fact` (the wire-protocol collector-side representation).

Contract System v1 §3.1 calls for eventually generating or aliasing all
three from one definition. This scaffold does **not** do that: `Envelope`
here is a standalone struct with its own field set and JSON tags, matching
the contributor-summary field list, and does not import or alias
`facts.Envelope` or `collector.Fact`. Unifying the three is tracked as
follow-up work under the epic this scaffold is step 1 of; see design §3.1
and §7 step 1 ("envelope unification").

## Fixtures and tests

`decode_test.go` is the TDD-first suite for the decode seam: missing
required field (classified error, zero-value struct never returned),
present-but-empty required field (decodes successfully), round trip (typed
struct → payload map → decoded struct, both with and without optional
fields present), and unsupported schema major. It also holds the
single-source-of-truth locks: `TestDerivedKeySetsMatchGeneratedSchemas`
asserts the reflectively derived required/known key sets equal each generated
schema's `required` and `properties`, `TestPayloadStructShapeConvention` bans
the field shapes that would make "required" ambiguous, and
`TestPayloadContractsCoverAllSchemas` fails if a schema file has no registered
payload contract — so adding a fact kind cannot skip these checks.

`fields_test.go` covers the reflective derivation itself (`payloadKeySetOf`,
`parseJSONTag`) that both the decode seam and those locks depend on.

`schema_gen_test.go` is the drift gate described above.

No-Observability-Change: this module has no runtime, network, queue, graph,
or telemetry emission path, the same as `sdk/go/collector`. Runtime
telemetry for the decode seam, once reducer handlers call it, is owned by
the migration PRs in Contract System v1 §7, not by this scaffold.
