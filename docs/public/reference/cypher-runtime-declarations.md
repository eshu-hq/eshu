# Runtime Declaration Graph Writes

The `runtime_declaration_projection` reducer domain (#8816) writes one
`RuntimeDeclaration` node per declared runtime version and links it from its
repository: `Repository-[:DECLARES_RUNTIME]->RuntimeDeclaration`. This page
records the schema those writes depend on and the measured plans. The proof
behind the retract scope is in the repository evidence note
`docs/internal/evidence/8816-runtime-declaration-graph.md`.

## Schema

| Statement | Backend | Purpose |
| --- | --- | --- |
| `runtime_declaration_identity`: unique `(repo_id, relative_path, line_number, runtime, order_index)` | Neo4j | The MERGE key. One node per token on a pin-file line. |
| `runtime_declaration_runtime_major`: range index on `(runtime, version_major)` | both | The version-threshold read, such as Node below 20 (#8817). |
| `nornicdb_runtime_declaration_repo_id_lookup` on `repo_id` | NornicDB | NornicDB drops composite constraints, so the MERGE needs a single-property lookup. |

`RuntimeDeclaration` and `DECLARES_RUNTIME` are new names: no other writer
MERGEs the label or the relationship type, so the retract may `DETACH DELETE`
the nodes it owns.

## Write and retract shapes

- Upsert: `UNWIND $rows`, `MERGE` the `Repository` by `id`, `MERGE` the node on
  the five-part key, `SET` every other property, `MERGE` the edge.
- Full-generation retract: seek each `Repository.id`, expand
  `DECLARES_RUNTIME`, `DETACH DELETE` the nodes with this evidence source.
- Delta retract: `UNWIND $scopes`, one row per repository with its own changed
  and deleted paths, then the same seek and expand filtered on
  `relative_path`. Binding paths per repository keeps a `.nvmrc` change in one
  repository from deleting another repository's `.nvmrc` declarations.

## Measured plans

Performance Evidence: pinned Neo4j image `neo4j:2026-community` (server
2026.08.1), one throwaway container, 50,000 `RuntimeDeclaration` nodes across
5,000 repositories (10 per repository) with the production DDL applied.
`node` is 4 of every 12 runtimes and 1 in 10 declarations has no
`version_major`; 10,002 declarations match `node` below 20.

| Statement | Operators | DB hits | Time |
| --- | --- | ---: | --- |
| `node` below 20, ordered by the key, `LIMIT 50` | `NodeIndexSeek` on `runtime_declaration_runtime_major`, `Top` | 50,061 | 1 + 29 ms warm (570 + 220 ms cold) |
| Production upsert, 500 rows (half new keys) | `MergeUniqueNode`, `LockingMerge`, `Expand(Into)` | 19,408 first run, 14,416 replay | 106 ms, then 48 ms |
| Whole-repository retract, 1 repository | `NodeUniqueIndexSeek(Locking)` on `Repository.id`, `Expand(All)` | 52 | under 1 ms |
| Whole-repository retract, 50 repositories | same | 2,620 | under 1 ms |
| Delta retract, 50 repositories with 2 paths each | `Unwind`, `NodeUniqueIndexSeek(Locking)`, `Expand(All)` | 2,224 | under 1 ms |

No plan contains `NodeByLabelScan` or `AllNodesScan`. The read cost grows with
the number of matching declarations, not with the label size. The upsert
replay created no node; the delta retract removed only the listed paths and
kept each repository's other files.

No-Observability-Change: the statements run through the reducer's
instrumented Cypher executor; the signals are listed in
[Telemetry Coverage](../observability/telemetry-coverage.md).
