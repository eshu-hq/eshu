# Fact Schema Versioning

This page defines compatibility rules for fact payload schemas. For envelope
fields, current fact families, and promotion rules, use
[Fact Envelope Reference](fact-envelope-reference.md).

## Identity

A fact schema is identified by:

- `fact_kind`
- `schema_version`
- `collector_kind`
- `source_confidence`

`schema_version` is required for every fact family that crosses collector,
storage, projector, reducer, or query boundaries. It uses semantic versioning.

Core fact kinds are owned by Eshu. Optional components must use a namespaced
fact kind such as reverse-DNS or another collision-resistant prefix. Two
components must not claim the same unowned fact kind.

The Go source of truth for core ownership is `go/internal/facts`: callers use
`CoreFactKinds()` and `IsCoreFactKind(kind)` rather than copying fact-kind
lists. Component manifest validation rejects core-owned claims and
non-namespaced component fact kinds. Component install and activation planning
also compare installed manifests so a different component ID cannot claim an
already-installed fact kind. The only local shared-ownership case is another
version of the same component ID with the same schema-version major set.

## Compatibility Rules

| Bump | Meaning | Runtime behavior |
| --- | --- | --- |
| Major | Breaking semantic change, removed field, or redefined field meaning. | Unsupported majors are rejected. No silent fallback. |
| Minor | Backward-compatible additive change. | The runtime must declare support before treating new fields as authoritative. |
| Patch | Documentation, bug fix, or non-semantic correction. | Must remain backward-compatible. |

When in doubt, bump higher. A conservative bump is cheaper than a silent
semantic change that corrupts downstream truth.

## Go Source Of Truth

The supported schema version for every core fact kind lives in `go/internal/facts`.
Callers classify a collector's fact version against it rather than copying
version strings:

- `facts.SchemaVersion(factKind)` returns the version a core consumer currently
  supports for a core fact kind.
- `facts.SupportedSchemaVersions()` returns the full core fact-kind to
  supported-version registry.
- `facts.ClassifySchemaVersion(factKind, candidate)` returns one of
  `supported`, `unsupported_major`, `unsupported_minor`, or `unknown_kind`.
- `facts.ValidateSchemaVersion(factKind, candidate)` returns an error for a
  core-owned kind carrying an unsupported version and nil for supported versions
  or fact kinds core does not own.

The classifier implements the table above: a different major (or a blank or
non-canonical version) is `unsupported_major` and rejected; a minor or patch
ahead of the supported version is `unsupported_minor` and not yet authoritative;
the supported version and older same-major versions are `supported`. An
out-of-tree component fact kind is `unknown_kind`, so core compatibility does not
falsely reject it.

Operators read the registry and classify a collector version with the read-only
CLI surface:

```bash
eshu component schema-versions                                  # every core fact kind
eshu component schema-versions --json
eshu component schema-versions --check terraform_state_resource=2.0.0  # classify one version
```

The `--check` form exits non-zero when the candidate version is not supported,
so it gates a collector version before it is accepted.

## Runtime Behavior

The runtime must fail clearly when a collector or component emits a fact schema
it does not support. It must not silently drop unknown meaning, invent missing
values, or downgrade facts to an older semantic shape.

For optional component packages, local manifest validation checks declared fact
kinds, schema versions, collector kinds, source-confidence values, compatible
core range, and digest-pinned artifacts. Current local component trust policy is
configuration-driven: disabled mode rejects all optional components, allowlist
mode accepts allowed identities and publishers, and strict mode additionally
requires configured Sigstore/Cosign signature and SLSA provenance verification.

## In-Store Migration

Facts already in durable storage are part of the data-plane contract.

- Backward-compatible readers may dual-read old and new schema versions during
  an operator-visible migration window.
- Incompatible schema changes require an explicit migration or reindex path.
- Silent in-place reinterpretation of stored facts is not allowed.
- Rows with `source_confidence=unknown` are compatibility debt. Re-emit them
  from the owning collector with an explicit confidence value, or run an
  operator-visible migration that proves the source family before updating the
  value.

The migration window should name the acceptance signal: successful reindex,
explicit admin migration, or zero old-version rows.

## DDL Ownership

Core runtime owns durable-store DDL. Components and collectors do not apply
arbitrary database schema migrations directly to Eshu's core stores.

If a new fact family needs new durable tables, indexes, or queue contracts, land
that schema through an explicit core-owned migration path before the collector
is considered active.

## Consumer Compatibility

A fact kind is not useful until a downstream consumer understands it.

New component fact families must declare their expected reducer or query
consumer contract in structured metadata. Unknown fact kinds must not be
presented as active platform truth.

## Idempotency

Fact emission and ingestion must be idempotent under at-least-once delivery.
Emitting the same source fact twice must converge on the same stable fact key
and must not create divergent graph or query truth.

## Test Requirements

When changing schema compatibility, cover:

- compatible schema accepted
- unsupported major rejected
- newer minor without declared support rejected or held non-authoritative
- manifest-declared schema mismatch rejected
- emitted-but-not-declared component fact kind rejected
- namespace collision rejected
- migration or rollback path where stored facts already exist

## Related

- [Fact Envelope Reference](fact-envelope-reference.md)
- [Component Package Manager](component-package-manager.md)
- [Plugin Trust Model](plugin-trust-model.md)
- [Collector Authoring](../guides/collector-authoring.md)
