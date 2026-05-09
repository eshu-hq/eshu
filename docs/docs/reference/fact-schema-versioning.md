# Fact Schema Versioning Policy

This document defines the compatibility rules for facts emitted by core
collectors and future OCI-packaged collector plugins.

## Why This Exists

Collector plugins only stay safe if the core runtime can answer two questions
before activation:

1. do I understand this fact schema version?
2. if not, do I fail clearly rather than silently dropping meaning?

## Fact Identity

A fact must be identified by:

- `fact_kind`
- `schema_version`
- `collector_kind`
- `source_confidence`

`schema_version` is required on every fact family that crosses collector
boundaries.

`schema_version` uses semantic versioning.

`collector_kind` identifies the collector family that produced the fact, such
as `git`, `terraform_state`, `aws`, `webhook`, or `documentation`.

`source_confidence` identifies how Eshu learned the fact:

- `observed` — read directly from the source artifact
- `reported` — returned by an external system or API
- `inferred` — concluded by correlating other evidence
- `derived` — materialized from existing Eshu facts
- `unknown` — legacy or system fallback only

New collectors must set `source_confidence` deliberately. `unknown` is allowed
as a storage compatibility default, not as a normal authoring choice.

Documentation collectors should use `source_confidence=observed` for page,
document, section, link, mention, and claim-candidate facts read directly from
the documentation source. A documentation claim candidate remains
non-authoritative even when it is observed. It proves that the document says
something, not that the claim is operationally true.

## Fact Kind Namespace

- core fact kinds are owned by the Eshu core runtime
- plugin fact kinds must use a namespaced form, such as reverse-DNS or another
  collision-resistant prefix

Two plugins must not emit the same unowned fact kind.

Core documentation facts use these fact kinds at schema version `1.0.0`:

- `documentation_source`
- `documentation_document`
- `documentation_section`
- `documentation_link`
- `documentation_entity_mention`
- `documentation_claim_candidate`

## Compatibility Rules

### Major incompatibility

If a plugin emits a fact with an unsupported major version, the runtime must
reject the plugin or the emitted fact family with a hard error.

### Minor compatibility

If the runtime supports the fact major version but not the plugin's newer minor
version, the runtime must fail clearly rather than silently accepting unknown
fields as authoritative.

### Patch-level compatibility

Patch-level changes must be backward-compatible and must not change semantic
meaning.

## Required Plugin Metadata

A plugin manifest must declare:

- emitted fact kinds
- supported schema versions
- collector kind
- source confidence values used by each fact family
- minimum compatible Eshu core version

## Runtime Behavior

At plugin load time, Eshu must:

1. validate plugin provenance and trust policy
2. validate declared fact-schema compatibility
3. reject incompatible plugins before runtime ingestion starts

Plugin trust policy is defined in `plugin-trust-model.md`.

Silent downgrade is not allowed for incompatible fact schemas.

If one plugin is rejected, other compatible plugins may continue to load unless
the operator requested fail-closed startup.

## Bump Rules

- major bump
  - breaking semantic change
  - requires explicit core support update
- minor bump
  - backward-compatible additive change
  - still requires declared core compatibility
- patch bump
  - documentation, bug fix, or non-semantic correction

## In-Store Migration Policy

When facts already exist in durable storage:

- backward-compatible readers may dual-read old and new schema versions during a
  migration window
- incompatible schema changes require an explicit migration or reindex path
- silent in-place reinterpretation of old facts is not allowed

The migration window should be explicit and operator-visible. Completion may be
driven by:

- a successful reindex
- an explicit admin migration command
- a zero-old-version-row verification gate

## DDL Ownership

Core runtime owns durable store DDL. Plugins do not apply arbitrary database
schema migrations directly to the core runtime.

If a new plugin fact family requires new durable tables or indexes, that schema
must be introduced through an explicit core-owned migration path.

## Consumer Compatibility

Fact kinds are not useful unless a downstream consumer understands them.

A plugin introducing a new fact kind must also declare the reducer or query
consumer contract expected to process it. Unknown fact kinds must not be
presented as active platform truth.

That declaration should live in structured plugin metadata, not just prose.

## Idempotency

Fact emission and ingestion must be idempotent. Emitting the same fact twice
must not create divergent truth under at-least-once delivery conditions.

## Deprecation

Unsupported major versions should not be removed abruptly. The compatibility
window must be documented at release time.

For additive minor versions, older stored facts may remain valid with null or
missing fields until re-emitted or backfilled through an explicit migration
path. The runtime must not silently invent values for absent fields.

## Test Requirements

- compatible plugin accepted
- unsupported major rejected
- newer minor without declared compatibility rejected
- mismatched manifest and emitted fact version rejected
- emitted-but-not-declared fact kind rejected
- concurrent plugin namespace conflict rejected
- downgrade and rollback paths exercised where migrations exist
