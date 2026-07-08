# Semantic Fact Payloads

This package defines schema-version-1 typed payloads for semantic evidence
facts:

| Fact kind | Struct | Decode seam |
| --- | --- | --- |
| `semantic.documentation_observation` | `DocumentationObservation` | `factschema.DecodeSemanticDocumentationObservation` |
| `semantic.code_hint` | `CodeHint` | `factschema.DecodeSemanticCodeHint` |

Both kinds are optional semantic evidence. They carry provenance, redaction,
freshness, and admission state so downstream reducers can decide whether a
model-produced observation is eligible for deterministic corroboration.
