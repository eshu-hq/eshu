# package_registry fact payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the
`package_registry` fact family, part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` contracts module (Contract System
v1, `docs/internal/design/contract-system-v1.md`). It turns the
`package_registry.*` fact payloads from unvalidated `map[string]any` into
typed, schema-generated, decode-validated contracts.

## Why

The package-registry collector emits nine fact kinds; the projector's
source-local canonical extractor
(`go/internal/projector/package_registry_canonical.go`) reads five of them to
materialize canonical `PackageRegistryPackage`, `PackageRegistryVersion`,
`PackageRegistryDependency`, `PackageRegistryArtifact`, and
`PackageRegistryEvent` graph rows. Before typing, it read each payload key
with a raw lookup that returned `""` for an absent key, so a collector that
dropped or renamed an identity key produced an empty-identity row — or
silently dropped the fact — with no operator signal. Typing the payload makes
an absent identity key a classified `input_invalid` dead-letter instead of
silent wrong graph truth (Contract System v1 §3.2).

## Kinds

| Struct | Fact kind | Status | Required (identity/join) |
| --- | --- | --- | --- |
| `Package` | `package_registry.package` | consumed | `package_id` |
| `PackageVersion` | `package_registry.package_version` | consumed | `package_id`, `version_id`, `version` |
| `PackageDependency` | `package_registry.package_dependency` | consumed | `package_id`, `version_id`, `dependency_package_id` |
| `PackageArtifact` | `package_registry.package_artifact` | consumed (#5458) | `package_id`, `version_id`, `artifact_key` |
| `RegistryEvent` | `package_registry.registry_event` | consumed (#5458) | `event_key`, `event_type` |
| `SourceHint` | `package_registry.source_hint` | typed, not yet consumed here | `package_id`, `hint_kind` |
| `VulnerabilityHint` | `package_registry.vulnerability_hint` | typed, not yet consumed | `package_id`, `advisory_id`, `advisory_source` |
| `RepositoryHosting` | `package_registry.repository_hosting` | typed, not yet consumed | `provider`, `registry`, `repository` |
| `Warning` | `package_registry.warning` | typed, not yet consumed | `warning_key`, `warning_code` |

### The required set is the identity gate, nothing more

Each consumed kind's required set is exactly the identity/join key whose
ABSENCE breaks the graph identity in the projector today:

- `Package.PackageID` anchors the package node uid and is dropped when empty.
- `PackageVersion.PackageID`/`.VersionID`/`.Version` are the version node's uid
  and its join key back to the owning package.
- `PackageDependency.PackageID`/`.VersionID`/`.DependencyPackageID` are the
  dependency edge's three join keys (the edge's own uid additionally requires
  a non-blank `StableFactKey`, enforced on the envelope, not the payload).
- `PackageArtifact.PackageID`/`.VersionID`/`.ArtifactKey` are the artifact
  node's owning package/version join keys and its own identity within that
  version (the node's own uid additionally requires a non-blank
  `StableFactKey`, enforced on the envelope, not the payload). Its `Hashes`
  map carries the per-artifact algorithm-to-digest binding that
  `PackageVersion.Checksums`/`checksum_algorithms` drops (algorithm names
  only). `Hashes` accepts any string key, including one containing `:`,
  matching the v1 JSON Schema's unconstrained `hashes.additionalProperties`
  exactly — `DecodePackageRegistryPackageArtifact` does not reject any
  `Hashes` key (an earlier version rejected a colon-bearing algorithm name,
  which silently narrowed the public v1 contract; #5820 P2 review finding).
  The canonical graph writer flattens `Hashes` into a sorted
  `"algorithm:digest"` string list (Cypher node properties cannot hold a
  nested map) and escapes both the algorithm and digest so the split stays
  unambiguous for any input, instead of narrowing what decode accepts.
- `RegistryEvent.EventKey`/`.EventType` are the event's own required identity
  (the emitter fails closed on either being blank). `.PackageID`/`.VersionID`
  are schema-OPTIONAL (a registry-wide event may report neither), so an
  absent value there is a VALID decode — but the projector's row builder
  (`packageRegistryEventRow`, NOT this decode seam) additionally requires
  BOTH present before a `PackageRegistryEventRow` materializes: this row
  exists to project a per-VERSION yank/deprecate/publish timeline, and an
  event with no version has nothing for the deferred `HAS_REGISTRY_EVENT`
  edge to attach to. That materialization gate is a projector decision, not a
  payload-validity decision, so it stays out of this decode seam's required
  set.

A present-but-empty required value is a VALID decode (an empty observed value
the projector already treats as non-materializable), not a dead-letter. Only an
ABSENT key (or explicit null) dead-letters.

`PackageVersion.IsYanked`/`.IsUnlisted`/`.IsDeprecated`/`.IsRetracted` and
`PackageDependency.Optional`/`.Excluded` are optional `*bool` with `omitempty`,
not required keys. They are descriptive status flags rather than identity keys,
and the projector re-decodes stored facts on every re-projection, so a
persisted or older fact that omits one still projects its node (nil derefs to
false) instead of dead-lettering on a missing descriptive flag — the same
`*bool` convention ociregistry's `Mutated` and terraformstate's `Sensitive`
follow.

### Typed but not yet consumed

`SourceHint`, `VulnerabilityHint`, `RepositoryHosting`, and `Warning` have no
decode-seam read consumer in the current codebase:

- `SourceHint`'s payload IS read today, but only by the reducer's
  `package_source_correlation` domain
  (`go/internal/reducer/package_source_correlation.go`, raw `payloadStr`
  calls) — a separate reducer family this wave does not convert. The
  projector's own `package_source_correlation_intents.go` reads only
  `envelope.FactKind` to route a reducer intent, never a payload field.
- `VulnerabilityHint.PackageID` and `Warning.Ecosystem`/`.WarningCode` are read
  by raw-SQL-JSONB loaders in `go/internal/storage/postgres`
  (`facts_active_supply_chain_impact.go`, `status_registry.go`). Those fields
  stay declared here and are locked by
  `go/internal/storage/postgres/package_registry_sql_schema_lockstep_test.go`
  so a dropped field fails the build instead of silently breaking the SQL read.
- `RepositoryHosting` is referenced only on the collector emit side today (no
  reducer, projector, or SQL reader).

They are typed here so the contract, schema, and fixture pack are ready the
moment a consumer is added, matching how the terraform_state family typed
`Candidate`/`ProviderBinding`/`Warning` ahead of their consumer. Their
decode-site conversion, `input_invalid` regression test, and No-Regression
benchmark land in the change that first reads each kind through the typed seam
— there is no read path to convert or benchmark in this wave.

## Contract shape

- Required fields are non-pointer with no `omitempty`; the decode seam rejects a
  payload that omits one (or supplies null) with a classified
  `ClassificationInputInvalid` error naming the field.
- Optional fields are pointers/slices/maps with `omitempty`, so an absent value
  decodes to nil and stays distinct from an observed zero.
- These structs are fully typed. No `Attributes map[string]any` pass-through
  exists in this family; every emitted payload key is a named field.

## Regeneration

After changing any struct's fields, from the module root
(`sdk/go/factschema`):

```bash
go generate ./...          # or: go run ./internal/schemagen/cmd
```

then copy each regenerated `schema/package_registry.*.v1.schema.json` to
`fixturepack/schema/` and run `go test ./... -count=1`. The drift-lock tests
(`TestSchemasHaveNoDrift`, `TestFixturePackSchemasMatchCanonical`) fail until
the checked-in artifacts match the structs.
