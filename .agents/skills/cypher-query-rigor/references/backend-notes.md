# Backend Dialect Notes

Eshu-specific lessons beyond the canonical docs. Read the canonical docs first
for anything not repeated here:

- [Cypher Performance](../../../../docs/public/reference/cypher-performance.md) —
  research locations, the CI evidence gate, backend-specific-behavior order,
  anti-patterns, and the Neo4j/NornicDB quick reference.
- [NornicDB Pitfalls](../../../../docs/public/reference/nornicdb-pitfalls.md) —
  known traps and "When To Patch NornicDB".
- [NornicDB Tuning](../../../../docs/public/reference/nornicdb-tuning.md) —
  runtime knobs.

## Neo4j

- Use `EXPLAIN` before changing production query shape; use `PROFILE` on safe
  datasets or test environments to verify actual rows and db hits.
- Constraints create backing indexes in Neo4j, but still write portable,
  selective query shapes rather than relying on planner magic.
- Prefer explicit uniqueness constraints for `MERGE` identities that must be
  globally unique.

## Eshu Graph Backend Lessons

- Eshu graph performance proofs are invalid unless the graph schema is applied
  before indexing. For production-profile Neo4j or NornicDB runs, execute
  `eshu-bootstrap-data-plane` before `eshu-bootstrap-index`; otherwise missing
  indexes or constraints can make shared Cypher look falsely slow.
- Treat NornicDB tuning wins as shared-Cypher wins first. Prefer improving the
  backend-neutral writer/query shape in Eshu before adding backend-specific
  branches.
- When a backend appears much slower, prove the setup first: schema applied,
  expected indexes present, same corpus, rebuilt binaries, same queue terminal
  state, effective runtime knobs, embeddings state, pprof state, and API/MCP
  truth checks against the completed graph.

## NornicDB

- Hot path eligibility matters. A logically equivalent Cypher shape may be much
  slower if it misses a supported template.
- Indexed-looking anchors are not proof. Verify the exact statement shape uses
  schema/index lookup and does not fall back to label scans, all-node scans, or
  relationship fanout scans.
- `UNWIND $rows AS row MERGE ...` or staged `WITH $rows AS rows UNWIND rows AS
  row` can be better than an `UNWIND ... MATCH` fallback when writing batches.
- For high-cardinality writes, compare earlier phases before blaming entity
  labels. File or directory upsert chunks can degrade first and make later
  entity containment appear guilty.
- Uniqueness constraints can still be a write-time cost center. Verify that
  constraint validation uses direct unique-value lookup rather than scanning
  the label population on every create.
- Relationship `MERGE` can be dominated by existence checks on the start node's
  outgoing fanout. If `(start)-[:TYPE]->(end)` is hot, look for a direct
  `(startID,type,endID)` lookup path or backend support before only shrinking
  batches.
- Treat `IF NOT EXISTS` as syntax, not idempotency evidence. For NornicDB index
  or constraint DDL, inspect the pinned executor's already-exists path and
  prove identical reapplication on an isolated populated store. Compare
  index-backed query set/order and index-entry cardinality where observable;
  graph node/edge counts alone are insufficient.
- Multi-label and unlabelled node matches can be risky; prefer one clear label
  plus indexed property anchors.

## Patching NornicDB: Support The Shape, Never Fail Loud

NornicDB's goal is drop-in Neo4j compatibility, and the maintainer has rejected
fail-loud patches twice for the same reason. When fixing a NornicDB Cypher
executor bug, the contract is:

- **Support every valid shape by mirroring Neo4j's actual semantics.** Do not
  fail loud, reject, or error on a shape Neo4j executes. Rejecting a valid
  query is not better than corrupting it — both break compatibility.
- **Reference the Neo4j source.** The Cypher runtime lives in the Neo4j
  monorepo under `community/cypher/`. Look for a checkout at
  `~/os-repos/neo4j`; if it is absent, clone it (`git clone
  --filter=blob:none --sparse https://github.com/neo4j/neo4j
  ~/os-repos/neo4j` then `git sparse-checkout set community/cypher`). Map the
  logical operator that governs the shape — for `OPTIONAL MATCH` that is
  `OptionalPipe` / `ApplyPipe` / `OptionalExpandAllPipe` (per left row, run
  the inner pattern; no match means keep the row and null the new
  variables); for aggregation it is `EagerAggregationPipe` plus the
  front-end `isolateAggregation` rewrite. Cite the Neo4j file mirrored.
- **Keep only genuine parse errors** — the ones Neo4j itself raises at parse
  time (for example `count()` with no argument). If unsure whether Neo4j
  accepts a shape, assume it does and support it.
- **Route to the real evaluator, not a string-slicing fallback.** A guard
  that fires before the pipeline handoff freezes valid queries out of the
  good executor. Teach the executor the shape rather than gating it.

See [NornicDB Pitfalls: When To Patch NornicDB](../../../../docs/public/reference/nornicdb-pitfalls.md#when-to-patch-nornicdb)
for the same rule and the patch-and-repin process.
