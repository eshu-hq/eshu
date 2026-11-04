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
- `payloads/nvd_paging_golden.json` — two synthetic NVD response pages and the
  exact source-snapshot/CVE payloads expected from complete collection. The
  published SDK fixture must remain byte-identical to this canonical source.
- `payloads/osv_severity_golden.json` — twelve synthetic OSV records and the
  `cvss_vector` and `severity_label` a conforming OSV collector writes for each
  (#9692). `cvss_vector` is the first prefixed `severity[]` entry of the
  highest CVSS type, CVSS_V4 then CVSS_V3 (a prefix check, not a validation);
  CVSS_V2 and unknown types never fill it. `severity_label` is
  `database_specific.severity` lower-cased, with `MODERATE` as `medium`; an
  unrecognized or non-string value leaves it empty.
  The collector writes no `cvss_score`. Both the in-tree collector and the
  extracted collector replay these records against their own envelope code.
- `payloads/osv_identity_edges_golden.json` — seven synthetic OSV records and the
  identity-edge fields a conforming OSV collector writes for each (#9954).
  `aliases`, `related` and `upstream` are trimmed, de-duplicated and sorted
  ascending; `aliases` and `related` are always written, an empty list as JSON
  `null`; `upstream` is written only when it has an id and is otherwise absent.
  `upstream` is a directed edge (a distro record naming the CVEs it
  repackages): it is never an alias, never joins `aliases` or
  `correlation_anchors`, and never selects `cve_id`, so a DSA bundle of CVEs is
  `upstream`, not `aliases`. `status` is `withdrawn` only for a record with a
  non-blank `withdrawn` timestamp; an active record has no `status` key. Each
  case carries its own `expected_absent_payload_keys`. The pack test derives
  every expected field from the case record and pins the file digest.
- `payloads/source_terms_golden.json` — the `license` and `attribution` each
  existing vulnerability source's `source_snapshot` carries (#9958): `osv`,
  `nvd`, `cisa_kev`, and `first_epss`. `license` is an SPDX identifier, a
  `LicenseRef-` identifier where the source publishes terms with no SPDX
  identifier, or `NOASSERTION` where no stated license was found. It describes
  the source, not each record (`osv` mixes licenses per record, so it says
  `LicenseRef-OSV-per-record`), and it discloses terms without deciding access.
  A source absent from the file carries neither key; `glad` is absent on
  purpose because GitLab's published terms forbid storing the content. The
  `first_epss` attribution is provisional Eshu wording until the owner gives
  FIRST's, and the `nvd` values rest on a reproduction of the NVD API terms, not
  the primary page. Core and the extracted
  collector each replay it against their own per-source constants. The pack
  test validates the rules, replays seeded violations, and pins the digest.

The NVD golden records the **full provider fact payload maps**, including
producer metadata outside the narrower typed factschema CVE and source-snapshot
structs. It is separate from the per-kind schema-valid/invalid examples. The
pack test decodes the SDK-known fields through the typed seam; each provider
must separately compare its complete emitted payload and stable key.

## Accessors

```go
schema, ok := fixturepack.SchemaFor("aws_resource") // json.RawMessage
valid, ok := fixturepack.ValidPayload("aws_resource")   // map[string]any
invalid, ok := fixturepack.InvalidPayload("aws_resource")
all := fixturepack.Schemas()  // map[string]json.RawMessage, keyed by fact kind
kinds := fixturepack.Kinds()  // sorted fact-kind list
golden := fixturepack.NVDPagingGolden() // copied JSON bytes for the NVD paging fixture
osv := fixturepack.OSVSeverityGolden()  // copied JSON bytes for the OSV severity fixture
edges := fixturepack.OSVIdentityEdgesGolden() // copied JSON bytes for the OSV identity-edge fixture
terms := fixturepack.SourceTermsGolden() // copied JSON bytes for the per-source license and attribution fixture
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
