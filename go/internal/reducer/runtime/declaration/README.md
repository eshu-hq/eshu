# internal/reducer/runtime/declaration

## Purpose

Projects `runtime_declaration.version` facts (contract and emission from
#8815) into the canonical graph as
`Repository-[:DECLARES_RUNTIME]->RuntimeDeclaration` (#8816), so a question such
as "which repositories declare Node below 20" reads typed graph data.

## Flow

```mermaid
flowchart LR
  collector[git collector] -->|runtime_declaration.version + repository + shared_followup| facts[(facts)]
  facts --> projector[projector reducer_intent]
  projector -->|runtime_declaration_projection intent| handler[Handler]
  handler -->|retract then write| writer[storage/cypher RuntimeDeclarationWriter]
  writer --> graph[(Neo4j)]
```

`Handler.Handle` loads the generation's `repository` and
`runtime_declaration.version` facts, decodes each declaration through
`schemadecode.DecodeRuntimeDeclarationVersion`, and:

1. retracts what the generation supersedes: every declaration of a repository
   on a full generation, or only the declarations of the changed and deleted
   files on a delta generation;
2. writes one row per declaration key.

The first attempt of a scope's first generation skips the retract (nothing can
exist yet). A retry always retracts.

## Ownership boundary

**Owns:** declaration row extraction (`ExtractRows`), the retract scope, and
the domain registration (`Definition`).

**Does not own:** fact emission (`internal/collector/repo/runtime/declaration`),
the Cypher (`internal/storage/cypher/edge/writer/runtime_declaration.go`), the
schema (`internal/graph`), or the read surface (#8817).

## Exported surface

| Symbol | What it is |
| --- | --- |
| `Handler` | the `runtime_declaration_projection` domain handler |
| `Writer` | the graph port the handler writes through |
| `Definition` | the domain registration the reducer root attaches `Handler` to |
| `ExtractRows` | pure fact-to-row extraction with per-fact quarantine |
| `EvidenceSource` | the `evidence_source` every node and edge carries |

## Telemetry

- `eshu_dp_reducer_input_invalid_facts_total{domain="runtime_declaration_projection"}`
  counts a declaration fact quarantined for a missing required field.
- INFO log `runtime declaration projection completed` carries
  `declaration_count`, `whole_repo_retract_count`, `delta_retract_repo_count`,
  `retract_skipped`, and `input_invalid_count`.
- The reducer's per-domain execution metrics and the canonical-phase statement
  metadata (`phase=runtime_declaration`) cover duration and failure.

## Gotchas / invariants

- A whole-repository retract on a delta generation erases every unchanged pin
  file's declarations. The delta path must stay path-scoped.
- The repository fact, not the declaration facts, names the repositories a
  full generation retracts; otherwise deleting the last pin file would leave
  its nodes behind.
- Never zero-fill a nullable version part: `0` reads as "below every
  threshold".
