# Dockerfile install graph proof (#9303)

Dockerfile `RUN` and URL `ADD` declarations become `DockerfileInstall`
content entities and canonical graph nodes. They remain source declarations;
they do not assert that a final image contains the software. The read route
uses persisted Postgres entities, while the graph projection gives each
install a file containment edge and generation-scoped retraction.

## Identity and write contract

The parser assigns `install_ordinal` in source order. The content writer and
canonical projector use the same metadata-aware identity helper when an
incoming entity ID is absent. A supplied ID is preserved. The graph writer
uses the existing `UNWIND` / `MERGE (n:DockerfileInstall {uid: row.entity_id})`
/ `SET n += row.props` template. The new `uid` uniqueness constraint is the
index and concurrent MERGE conflict domain. The `version` property is included
with `nil` when absent, so a later unpinned declaration clears the old value
without replacing unrelated properties. The standard entity retract family
removes nodes with an older generation in the same repository and
`projector/canonical` evidence source.

## Pinned Neo4j scratch proof

On 2026-10-28, an isolated local `neo4j:2026-community` image at digest
`sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
reported runtime 2026.08.1. A `DockerfileInstall(uid)` uniqueness constraint
was applied. Two same-line surrogate IDs yielded one node each. Re-MERGE with
`version:null` removed an old `8.2` property, while another changed from
`8.3` to `9.0`. Two concurrent MERGEs of one uid both exited zero and left
one node with the same `2.0` version. A generation-scoped retract removed a
seeded stale install while retaining a current one. A uid lookup `EXPLAIN`
used `NodeUniqueIndexSeek` on the constraint.

Performance Evidence: In a quiet-host window from 19:35:30 to 19:37:00 UTC,
we PROFILEd the same 1,000-row constrained `MERGE` + `SET +=` shape on
`Function` (existing baseline) and `DockerfileInstall` (new label), alternating
arms on the same isolated backend. Create pass server times were 110 ms and
97 ms. Three warm passes were 24/12/56 ms and 17/13/46 ms respectively.
Both plans used `MergeUniqueNode`, 1,000 unique-index lookups, and 8,000 DB
hits. The medians were 24 ms and 17 ms. This proves no observed added cost
from the new label on an equal-shape local workload. The samples are small
and noisy; they are not a production p95 or corpus-scale latency claim.
The isolated stack and volume were removed at 19:37:00 UTC.

Observability Evidence: The canonical writer already records per-phase
`canonical atomic write completed` logs and `postgres.query` spans. The new
read marks `db.operation=search_dockerfile_install` on its Postgres span.
The standard retraction statements carry `OperationCanonicalRetract`, so
existing phase timing and retraction counters include this label. No new
metric cardinality was added.
