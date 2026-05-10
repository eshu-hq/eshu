---
name: cypher-query-rigor
description: >
  Use when writing, reviewing, debugging, or optimizing Cypher for Neo4j,
  NornicDB, or graph-backed application work, including graph schema design,
  graph indexes and constraints, MATCH, MERGE, UNWIND, query plans, graph write
  performance, graph read performance, graph-backed API/query handlers,
  materialization jobs, projection code, migrations, and backend portability.
---

# Cypher Query Rigor

## Operating Rule

MUST NOT write or change Cypher until you understand the graph model, data
distribution, and effect on the surrounding system. Optimize in this order:
correctness, then query selectivity and write safety, then backend-specific
performance.

## Workflow

1. Understand the model first.
   Identify labels, relationship types, uniqueness rules, optional data, fan-out, skew, and which nodes or edges can be large. Trace who calls the query, how often it runs, expected row counts, timeout budget, transaction scope, retries, and downstream consumers.

2. Choose the entrypoint deliberately.
   Start from the most selective, indexed anchor available. Estimate cardinality at each pattern expansion before adding more hops. Treat every unanchored pattern as suspicious until proven small.

3. Map anchors to indexes and constraints.
   Confirm that each lookup predicate can use a label plus property index or a uniqueness constraint. Add or request indexes when a hot query depends on them. Do not assume an index exists because a property looks unique.

4. Shape reads for bounded work.
   Prefer label-property anchored `MATCH` patterns, short directed traversals, early filtering on indexed anchors, and early `WITH` projections that shrink rows. Avoid hidden broad scans, unlabelled node matches, unbounded variable-length traversals, late `LIMIT`, and predicates such as broad `OR` or `coalesce()` that can block index use.

5. Shape writes for idempotency and contention.
   Define conflict domains before using `MERGE`. Use stable keys, batch with `UNWIND`, keep transactions bounded, avoid huge cross-products, and separate independent write phases when a single statement would create lock contention or retry amplification.

6. Compare backend behavior.
   For Neo4j, inspect planner output with `EXPLAIN` or `PROFILE` when possible. For NornicDB, check whether the statement matches supported hot-path templates and verify uncertain behavior against NornicDB docs or source before adding a workaround.

7. Add verification and observability.
   Capture plans or statement summaries where possible, timings, row counts, db hits or equivalent counters, batch sizes, errors, and retry behavior. Measure phase-by-phase timing and duration slope across chunks before blaming the largest label or raising timeouts. Add tests for positive, negative, empty, high-cardinality, duplicate, and ambiguous inputs when query behavior affects correctness.

## Query Checklist

- MUST state the expected input cardinality, output cardinality, and largest fan-out.
- MUST name the selective anchor label and property.
- MUST confirm the supporting index or constraint.
- Prove `LIMIT` happens after the intended narrowing, not after a large traversal.
- Check for accidental cartesian products between independent `MATCH` clauses.
- Check that `OPTIONAL MATCH` does not multiply rows unexpectedly.
- Ensure every variable-length traversal has a bounded range and a selective anchor.
- Keep returned payloads narrow for API surfaces; do not return full paths or nodes unless required.

## Write Checklist

- MUST use `MERGE` only on the true identity key, not on a wide mutable map.
- Split `MERGE` identity from `SET` mutable properties.
- Batch rows with `UNWIND $rows AS row`; keep batch size tied to transaction and lock behavior.
- Watch chunk duration slope as the graph grows. Stable batch size with rising duration often means lookup or relationship-existence checks are scanning despite an indexed-looking Cypher shape.
- Make retries safe through idempotent keys and deterministic relationship identity.
- Avoid writing from a broad read result unless the read side is bounded and measured.
- Verify duplicate input rows do not create duplicate relationships or excess writes.
- Track rows attempted, rows written, batches committed, duration, and failure reason.

## Good And Bad Patterns

Bad: unlabelled scan plus late filter.

```cypher
MATCH (n)
WHERE n.id = $id
RETURN n
```

Good: indexed label-property anchor.

```cypher
MATCH (s:Service {id: $id})
RETURN s
```

Bad: broad expansion before limiting.

```cypher
MATCH (r:Repository)-[:CONTAINS*]->(n)
RETURN n
LIMIT 25
```

Good: anchor, bound, filter, then limit.

```cypher
MATCH (r:Repository {id: $repo_id})-[:CONTAINS*1..3]->(n:File)
WHERE n.language = $language
RETURN n.path
ORDER BY n.path
LIMIT 25
```

Bad: wide mutable `MERGE` identity.

```cypher
UNWIND $rows AS row
MERGE (s:Service {id: row.id, name: row.name, owner: row.owner})
```

Good: stable identity plus mutable updates.

```cypher
UNWIND $rows AS row
MERGE (s:Service {id: row.id})
SET s.name = row.name,
    s.owner = row.owner,
    s.updated_at = row.updated_at
```

Bad: independent matches that can create a cartesian write multiplier.

```cypher
MATCH (s:Service {id: $service_id})
MATCH (e:Environment)
MERGE (s)-[:RUNS_IN]->(e)
```

Good: constrain both sides.

```cypher
MATCH (s:Service {id: $service_id})
MATCH (e:Environment {name: $environment})
MERGE (s)-[:RUNS_IN]->(e)
```

## Neo4j Notes

- Use `EXPLAIN` before changing production query shape; use `PROFILE` on safe datasets or test environments to verify actual rows and db hits.
- Constraints create backing indexes in Neo4j, but still write portable, selective query shapes rather than relying on planner magic.
- Watch for planner regressions from `OR`, `coalesce()`, function-wrapped properties, mixed label patterns, and optional expansions before filters.
- Prefer explicit uniqueness constraints for `MERGE` identities that must be globally unique.

## Eshu Graph Backend Lessons

- Eshu graph performance proofs are invalid unless the graph schema is applied
  before indexing. For production-profile Neo4j or NornicDB runs, execute
  `eshu-bootstrap-data-plane` before `eshu-bootstrap-index`; otherwise missing
  indexes or constraints can make shared Cypher look falsely slow.
- Treat NornicDB tuning wins as shared-Cypher wins first. Prefer improving the
  backend-neutral writer/query shape in Eshu before adding backend-specific
  branches.
- Neo4j and NornicDB must use Eshu's shared raw Cypher/Bolt contract wherever
  possible. Backend-specific code belongs only in narrow seams such as schema
  DDL, connection/runtime settings, retry classification, query builders, or
  measured dialect adapters.
- When a backend appears much slower, prove the setup first: schema applied,
  expected indexes present, same corpus, rebuilt binaries, same queue terminal
  state, and API/MCP truth checks against the completed graph.

## NornicDB Notes

- Hot path eligibility matters. A logically equivalent Cypher shape may be much slower if it misses a supported template.
- Indexed-looking anchors are not proof. Verify the exact statement shape uses schema/index lookup and does not fall back to label scans, all-node scans, or relationship fanout scans.
- `UNWIND $rows AS row MERGE ...` or staged `WITH $rows AS rows UNWIND rows AS row` can be better than an `UNWIND ... MATCH` fallback when writing batches.
- For high-cardinality writes, compare earlier phases before blaming entity labels. File or directory upsert chunks can degrade first and make later entity containment appear guilty.
- Uniqueness constraints can still be a write-time cost center. On NornicDB, verify that constraint validation uses direct unique-value lookup rather than scanning the label population on every create.
- Relationship `MERGE` can be dominated by existence checks on the start node's outgoing fanout. If `(start)-[:TYPE]->(end)` is hot, look for a direct `(startID,type,endID)` lookup path or backend support before only shrinking batches.
- Explicit property indexes may be needed for hot graph-backed APIs and materialization jobs.
- Multi-label and unlabelled node matches can be risky; prefer one clear label plus indexed property anchors.
- Verify uncertain behavior against NornicDB docs or source before assuming Neo4j planner behavior applies.

## Response Discipline

When proposing or implementing Cypher, include the intended anchor, expected cardinality, required index or constraint, backend-specific concern, and verification plan. If any of those are unknown and materially affect correctness or performance, stop and ask instead of guessing.
