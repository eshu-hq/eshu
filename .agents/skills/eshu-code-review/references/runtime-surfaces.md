# Runtime Review Surfaces

Read the sections matching the diff; missing applicable proof blocks review.

## Pass 1: Correctness And Truth

Review for wrong graph, query, API, MCP, or CLI truth before considering
performance. Check:

- missing tests or tests that do not exercise the production subject;
- raw evidence -> fact -> queue -> reducer/projector -> graph/content ->
  API/MCP agreement;
- fixture intent, cassettes, B-12 golden snapshot, and replay coverage;
- tenant/repo scope boundaries, stale generations, unknown/ambiguous ownership,
  cycles, duplicates, empty state, invalid input, no-provider-key behavior, and
  deterministic evidence preservation;
- cross-repo/live-if-used-by-consumer semantics and evidence citations;
- OpenAPI, HTTP, MCP, CLI, docs, and capability inventory lockstep.

Capability, replay, and product-claim reviews must explicitly attack
false-green shapes:

- blank or whitespace-only proof refs or proof kinds;
- unknown capability ids, stale maturity, stale source-line anchors, or stale
  generated surface counts;
- proof signals that no longer match catalog rows;
- product claims whose deterministic docs path passes while the live issue or
  tokened API path fails;
- replay coverage entries that count an authored scenario but do not name the
  sibling gate that proves the scenario green.
- replay coverage manifest refs whose artifact paths are not watched by the
  coverage workflow and `specs/ci-gates.v1.yaml` trigger list.

## Pass 2: Performance And Storage/Query Shape

Review the same diff for cost and backend shape after correctness is understood.
Check:

- hot-path Cypher, graph writes/retracts, Postgres queries, indexes, and
  constraints;
- unbounded all-graph/all-table scans, late LIMIT, broad OR, function-wrapped
  indexed predicates, optional branch multiplication, missing deterministic
  ordering, and payload size;
- reducer/shared-projection queue pressure, graph write budgets, batching,
  worker knobs, and full-corpus or no-regression evidence;
- missing instrumentation or missing `Performance Evidence:`,
  `Benchmark Evidence:`, `No-Regression Evidence:`, `Observability Evidence:`,
  or `No-Observability-Change:` markers when required;
- for a claim/lock/lease/queue rewrite: a concurrency proof (contention /
  EvalPlanQual recheck / lease-safety), not only a row-set equivalence
  differential — the differential drops `FOR UPDATE` and cannot catch lease theft;
- a wall-clock proof on the BUILT BINARY against the real worst-case backlog, not
  only a small-N `EXPLAIN` (which can hide a missing `AS MATERIALIZED` re-inline or
  an O(N^2) residual subquery);
- a differential whose "expected" query is hand-frozen (drift → false-green)
  rather than derived from the shipped constant with a hermetic prefix guard, and
  any DSN-gated proof that SKIPS in CI without a hermetic in-CI structural guard.

### NornicDB/Cypher Review

When Cypher, graph reads/writes, query-shape generation, reducer projection, or
API/MCP graph-backed responses change:

- Compare Eshu's pinned NornicDB image/tag/digest against current NornicDB
  docs/source before relying on optimizer behavior.
- Read Eshu `docs/public/reference/cypher-performance.md`,
  `docs/public/reference/nornicdb-pitfalls.md`,
  `docs/public/reference/nornicdb-tuning.md`, and the relevant current
  NornicDB source/docs such as `docs/performance/hot-path-query-cookbook.md`,
  `docs/skills/cypher-queries.skill.md`, `pkg/cypher/*hotpath*_test.go`, and
  `pkg/cypher/executor_hotpath_trace.go`.
- Identify the expected named fast path or deliberate fallback:
  `UnwindMergeChainBatch`, `UnwindMultiMatchCreateBatch`,
  `MergeSchemaLookupUsed`, `CompoundQueryFastPath`,
  `CallTailTraversalFastPath`, indexed traversal seed paths, or another traced
  flag from current source.
- Prove `MergeScanFallbackUsed=false` and `OuterScanFallbackUsed=false` for
  intended indexed paths unless fallback is deliberate, bounded, and measured.
- Require exact emitted query-shape tests or live profile/trace evidence for
  generated Cypher; simplified hand-written query tests are not enough.
- Verify every multi-label MATCH/MERGE alternative label has the required
  uniqueness constraint or property index. One unindexed alternative can flip
  `MergeScanFallbackUsed=true`.
- Treat runtime-selected labels and identity properties as alternatives too.
  Proof for one label/property pair does not cover any other branch.
- A query-plan fixture that claims `NodeIndexSeek` MUST declare its load-bearing
  index or constraint under `required_schema`; a caveat naming it is not a gate.
- Prefer stable parameterized query templates. Whitespace/query-text churn can
  defeat plan-cache reuse.
- Review DDL/bootstrap separately: schema DDL must be startup-first,
  idempotent, and not reissued against populated stores in a way that blocks
  restarts behind corpus reads.
