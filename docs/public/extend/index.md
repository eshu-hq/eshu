# Extend Eshu

Use this section when adding a source family, parser behavior, language
support, relationship extractor, or collector plugin.

The short rule: collectors observe source truth and emit versioned facts.
Reducers and graph writers own canonical graph truth.

## Extension Paths

| Need | Start here |
| --- | --- |
| Understand package ownership | [Source Layout](../reference/source-layout.md) |
| Author a collector | [Collector Authoring](../guides/collector-authoring.md) |
| Emit facts safely | [Fact Envelope Reference](../reference/fact-envelope-reference.md) |
| Version fact schemas | [Fact Schema Versioning](../reference/fact-schema-versioning.md) |
| Package and trust plugins | [Plugin Trust Model](../reference/plugin-trust-model.md) |
| Add language parsing or query support | [Language Support](../contributing-language-support.md) |
| Query language-specific structure | [Language Query DSL](../reference/language-query-dsl.md) |
| Add relationship extraction | [Relationship Mapping](../reference/relationship-mapping.md) |

## Boundary Rules

- New collectors write facts, not canonical graph rows.
- New fact kinds need schema versions and a consumer contract.
- Unknown or incompatible plugin facts fail closed.
- Parser and relationship changes need fixtures for the behavior they claim.
- Runtime behavior changes need telemetry and a verification gate.
