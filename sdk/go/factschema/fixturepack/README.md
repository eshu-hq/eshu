# Fact-schema fixture pack

The fixture pack is the versioned, importable payload-conformance artifact for
the Eshu fact-schema contracts (Contract System v1 §3.5). It bundles the
checked-in JSON Schema for every typed fact kind together with a curated valid
and invalid example payload per kind, so an out-of-tree collector can pin one
fixture-pack version and prove in its own CI that it emits exactly the payload
shapes the target reducer release consumes.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no third-party
  dependencies reachable from this package — it uses only `embed`/`encoding/json`)

## What it ships

- `schema/*.json` — the JSON Schema for each typed fact kind, embedded with
  `go:embed`. Byte-identical to the canonical generated artifacts under
  `sdk/go/factschema/schema/*.json`; the drift-lock test
  `TestFixturePackSchemasMatchCanonical` fails the build if the two diverge, so
  the pack can never ship a stale schema.
- `payloads/<kind>.valid.json` — one schema-valid example payload per kind.
- `payloads/<kind>.invalid.json` — one payload per kind that omits a
  schema-required field, to exercise the fail-closed path.

## Accessors

```go
schema, ok := fixturepack.SchemaFor("aws_resource") // json.RawMessage
valid, ok := fixturepack.ValidPayload("aws_resource")   // map[string]any
invalid, ok := fixturepack.InvalidPayload("aws_resource")
all := fixturepack.Schemas()  // map[string]json.RawMessage, keyed by fact kind
kinds := fixturepack.Kinds()  // sorted fact-kind list
```

## How a collector uses it

Conformance's payload validation is keyed by the fact kind the collector
actually emits. Because the bare core kinds (`aws_resource`, ...) are host-owned
and reserved, an out-of-tree collector emits the same payload **shape** under
its own namespaced kind (see `collector-extraction-policy.md`) and maps that
namespaced kind to the shipped schema shape:

```go
schema, _ := fixturepack.SchemaFor("aws_resource")
report := conformance.Run(conformance.Request{
    Manifest: manifest, // declares dev.acme.collector.aws_resource
    Fixtures: []collector.Result{result}, // emits dev.acme.collector.aws_resource
    Mode:     conformance.ModeFixture,
    PayloadSchemas: map[string]json.RawMessage{
        "dev.acme.collector.aws_resource": schema, // namespaced kind -> aws_resource shape
    },
})
```

The wire kind is namespaced while the schema is the `aws_resource` shape on
purpose: the collector observes and re-emits the shape, but the core kind stays
host-owned. This is not a bug — it is the extension contract.

`examples/collector-extensions/scorecard/fixturepack_pin_test.go` is the
copy-paste reference: it pins the pack, maps a namespaced kind to the
`aws_resource` shape, and proves the valid payload passes and the invalid one
fails closed, all in a separate module with its own `go.mod`.

## Versioning and lockstep

The fixture-pack version **is** the `sdk/go/factschema` module version. The pack
ships inside that module, so pinning
`github.com/eshu-hq/eshu/sdk/go/factschema` at a git tag pins the schemas and
example payloads that were valid at that tag together. There is no separate
fixture-pack version number to keep in sync.

"Lockstep with the contracts module" means, operationally:

- **Cutting a pack** is cutting a `sdk/go/factschema` release: tag the module
  (the standard Go submodule tag `sdk/go/factschema/vX.Y.Z`). The schemas and
  payloads embedded at that commit are that pack version.
- **A schema change bumps the pack** through the same semver rules the contracts
  module follows: a breaking payload change is a major bump, an additive
  optional field is a minor bump. The drift-lock test guarantees the embedded
  schema copy tracks the regenerated canonical artifact within the same commit,
  so a schema change and its pack update land together or the build fails.
- **A collector pins a pack** by requiring that module version. It re-runs
  conformance against the pinned schemas whenever it bumps the pin, so a payload
  shape it can no longer satisfy surfaces as a failed conformance run in its own
  CI, before the mismatch reaches a reducer.

A fixture pack that outlived the schema version it was cut from is stale
evidence, not a fixture, which is why the pack and the contracts module share
one tag rather than versioning independently.
