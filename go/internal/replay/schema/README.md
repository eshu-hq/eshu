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
- `ValidateCassetteBytes(name, data)` — validates one cassette document offline
  against the **generated schema itself** (types, required, `const`/`enum`,
  `minimum`/`minLength`/`minItems`, and `additionalProperties:false`), plus the
  canonical loader's few semantic checks the schema cannot express (a non-zero
  `observed_at`). Errors are field-level. Validating against the published schema
  — not a permissive re-implementation — is what keeps the author-time gate from
  drifting from the contract.

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

The validator enforces the **generated schema** directly (via a small
interpreter over exactly the JSON Schema vocabulary the builder emits), so the
author-time gate and the published schema cannot drift: a constraint the schema
declares — a negative `fencing_token`, a null `metadata`, an unknown field — is
rejected by the gate, not silently accepted. The canonical loader
(`cassette.ParseAndValidate`) runs as a second pass only for the rules the schema
cannot express (a non-zero `observed_at`). The cross-link test additionally
proves the schema's properties stay equal to the cassette structs' JSON keys.
