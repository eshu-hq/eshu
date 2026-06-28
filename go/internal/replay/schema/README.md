# schema

The discoverable contract for the v1 cassette envelope format and the fast,
offline validator that runs every committed cassette through it.

## Why this exists

Cassettes are credential-free fixtures that any contributor can hand-author or
record. The format is otherwise implicit knowledge that only fails at load
time — often eight minutes into a CI gate, after Docker is up. A published JSON
Schema plus a millisecond validator moves that failure left: a typo or a missing
field is caught at author time, before the change is even pushed.

## What it provides

- `CassetteFormatV1() ([]byte, error)` — the JSON Schema (draft 2020-12) for the
  cassette format, built in Go from the cassette structs. The committed
  `cassette-format.v1.schema.json` is generated from this function, never
  hand-edited.
- `ValidateCassetteBytes(name, data)` — validates one cassette document offline:
  structural validation via the canonical loader
  (`cassette.ParseAndValidate`) **and** `additionalProperties:false` enforcement
  that rejects unknown (typo) fields with a field-level path.

## Generated, not hand-maintained

The schema is the JSON serialization of `CassetteFormatV1()`. Two committed
copies are kept byte-identical to the builder:

- `go/internal/replay/schema/cassette-format.v1.schema.json` — canonical copy.
- `sdk/go/collector/schema/cassette-format.v1.schema.json` — mirror shipped to
  external collector authors alongside the SDK result schema.

`TestCassetteSchemaMatchesGolden` is the drift gate. After changing the cassette
format or the builder, regenerate both copies:

```bash
cd go && go test ./internal/replay/schema -run TestCassetteSchemaMatchesGolden -update
```

`TestSchemaPropertiesMatchCassetteStructs` binds the schema to `format.go`: the
schema's declared properties must equal exactly the JSON keys the cassette
structs serialize, so a new struct field cannot land without the schema
following.

## Validating cassettes

Run the offline author-time gate over every committed cassette — no Docker, no
graph, milliseconds:

```bash
scripts/verify-cassette-author.sh
```

It wraps the focused `go test` that drives `ValidateCassetteBytes` across
`testdata/cassettes/*/*.json`, plus the structural and unknown-field negative
cases.

## Relationship to the loader

The schema is the **published contract**; `cassette.ParseAndValidate` is its
**runtime enforcement**. They are kept in lockstep by the cross-link test, so
the two never declare different required fields. The schema additionally
declares `additionalProperties:false`, which the validator enforces on top of
the loader to catch the field-name-typo class.
