# Fact Schema Contracts

This module defines the versioned collector-to-reducer **payload** contracts
described in Contract System v1 §3.1, "The contracts module"
(`docs/internal/design/contract-system-v1.md`), and its contributor summary
(`docs/internal/contract-system-contributor-summary.md`). It intentionally
does not import `github.com/eshu-hq/eshu/go/internal` packages — the same
constraint `sdk/go/collector` already satisfies. Both collector repositories
and the core reducer depend on this module; they never depend on each other.

This is a **scaffold**: it demonstrates the pattern end to end with one
sample fact kind, `aws.resource` (schema version 1). It intentionally does
not migrate any existing fact family. See Contract System v1 §7
(`docs/internal/design/contract-system-v1.md`) for the family-by-family
migration plan this scaffold is step 1 of.

## Compatibility

- Go module path: `github.com/eshu-hq/eshu/sdk/go/factschema`
- Sample fact kind: `aws.resource`, schema version `1.0.0`
- JSON Schema artifact: `schema/aws_resource.v1.schema.json`, generated with
  [`invopop/jsonschema`](https://github.com/invopop/jsonschema)

## Contracts

- `Envelope` — the canonical fact envelope: `fact_kind`, `schema_version`,
  `stable_fact_key`, `scope_id`, `generation_id`, `collector_kind`,
  `source_confidence`, `observed_at`, `is_tombstone`, `source_ref`, and the
  raw `payload` map.
- `aws/v1.Resource` — the sample typed payload struct for fact kind
  `aws.resource`.
- `DecodeAWSResource(env Envelope) (awsv1.Resource, error)` — the kind-keyed
  decode seam. Reducer handlers call this instead of reading
  `env.Payload["some_key"]` directly.
- `EncodeAWSResource(resource awsv1.Resource) (map[string]any, error)` — the
  emit-side counterpart collectors use to build an `Envelope.Payload` from a
  typed struct.
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
fields present), unsupported schema major, and a reflection-based assertion
that the struct's pointer/omitempty shape matches the required/optional
contract documented on `aws/v1.Resource`.

`schema_gen_test.go` is the drift gate described above.

No-Observability-Change: this module has no runtime, network, queue, graph,
or telemetry emission path, the same as `sdk/go/collector`. Runtime
telemetry for the decode seam, once reducer handlers call it, is owned by
the migration PRs in Contract System v1 §7, not by this scaffold.
