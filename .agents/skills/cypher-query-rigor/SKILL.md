---
name: cypher-query-rigor
description: Use when writing or reviewing Eshu Cypher reads, graph writes, indexes, or NornicDB/Neo4j dialect behavior. Covers anchor and index choice, write idempotency, and when to patch NornicDB. Postgres SQL belongs to eshu-postgres-rigor.
---

# Cypher Query Rigor

## Operating Rule

MUST NOT write or change Cypher until you understand the graph model, data
distribution, and effect on the surrounding system. Optimize in this order:
correctness, then query selectivity and write safety, then backend-specific
performance.

Add `eshu-performance-rigor` for any measured Cypher/index optimization,
scaled replay, or before/after latency or throughput claim.

## Mandatory Pre-Implementation Discipline

Before designing or merging a Cypher statement that lives in a hot path
(canonical writer, reducer projection, query handler, materialization job),
answer both of these explicitly:

1. **Research first.** Read the relevant backend behavior in source before
   writing the query — see
   [Cypher Performance: Research The Pinned Backend](../../../docs/public/reference/cypher-performance.md#1-research-the-pinned-backend)
   for Neo4j and NornicDB source locations, and always check
   [NornicDB Pitfalls](../../../docs/public/reference/nornicdb-pitfalls.md) for
   known traps. If your query uses a pattern you haven't validated against the
   pinned binary, that's research debt — close it with a focused test or
   `curl`-against-Bolt-HTTP probe in an isolated, uniquely-named Compose stack.

2. **Benchmark first.** Capture a baseline before and an after measurement
   against the pinned backend binary on the same inputs, per
   [Cypher Performance: Measure The Same Shape](../../../docs/public/reference/cypher-performance.md#2-measure-the-same-shape-before-and-after).
   Unmeasured Cypher in a hot path is a regression-shaped surprise. Pure
   correctness fixes can trade a full bench for a "no measurable regression"
   check on the same input shape, but must state that decision explicitly in
   the PR.

3. **Prove setup before query blame.** For remote or Compose NornicDB proof,
   record the Eshu commit, NornicDB commit or image tag, effective container
   environment, schema/bootstrap state, embeddings state, pprof state, worker
   knobs, clean-volume state, and terminal queue counts before deciding a
   Cypher shape is the bottleneck.

CI enforces this for the repo. Any PR that changes hot-path Cypher, graph
writer code, schema, reducer/projector graph work, or a new collector package
that contains Cypher-like query text must pass:

```bash
scripts/test-verify-performance-evidence.sh
scripts/verify-performance-evidence.sh
```

The gate is **content-based, not only path-based** (a file that *contains*
`MATCH`/`MERGE`/`UNWIND` is flagged even for a comment-only diff), diffs
`HEAD~1` locally but `origin/$GITHUB_BASE_REF` in CI, and requires a tracked
docs/ADR/package note (not PR text) with one benchmark marker
(`Performance Evidence:`, `Benchmark Evidence:`, or `No-Regression Evidence:`)
plus one observability marker (`Observability Evidence:` or
`No-Observability-Change:`) naming the exact query shape, backend, input
cardinality, index/constraint state, and before/after timing. Reproduce the CI
diff window locally before pushing:

```bash
ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main scripts/verify-performance-evidence.sh
```

Full detail on the gate, the query-plan-regression fixture contract, and
worked good/bad evidence text is in
[Cypher Performance: CI Evidence Gate](../../../docs/public/reference/cypher-performance.md#ci-evidence-gate).

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
   For Neo4j, inspect planner output with `EXPLAIN` or `PROFILE` when possible. For NornicDB, check whether the statement matches supported hot-path templates and verify uncertain behavior against NornicDB docs or source before adding a workaround. See [backend-notes.md](references/backend-notes.md).

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
- Make retries safe through idempotent keys and deterministic relationship identity — see root
  [Serialization Is Not A Fix](../../../AGENTS.md#serialization-is-not-a-fix)
  before reaching for fewer workers or smaller batches.
- Avoid writing from a broad read result unless the read side is bounded and measured.
- Verify duplicate input rows do not create duplicate relationships or excess writes.
- Track rows attempted, rows written, batches committed, duration, and failure reason.

Worked good/bad examples for both checklists: [patterns.md](references/patterns.md).
Backend-specific dialect notes, Eshu graph-backend lessons, and the NornicDB
patching contract: [backend-notes.md](references/backend-notes.md).

## Response Discipline

When proposing or implementing Cypher, include the intended anchor, expected cardinality, required index or constraint, backend-specific concern, and verification plan. If any of those are unknown and materially affect correctness or performance, measure them (`PROFILE`/`EXPLAIN`, a scratch query) or escalate to an arbiter model instead of guessing.
