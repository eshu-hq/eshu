# Proof Tier Selection

## Proof Tier Decision

Select exactly one tier and explain why it is enough. If cassette proof is
sufficient, name the exact cassette/golden assertions that would fail on the
bug. If it is not sufficient for behavior changed by the PR, name the missing
runtime condition and block merge until the stronger gate runs. Link or create a
follow-up only when the stronger proof is genuinely outside the PR scope and the
current PR does not claim that condition is proven.

| Tier | Use when |
| --- | --- |
| Unit/static proof enough | Documentation and agent guidance, pure helper logic, parser-local behavior, generated string construction, or small contract code with no projected truth or runtime coupling. Guidance changes need semantic review of the instructions as well as docs/static checks. |
| Cassette/golden replay required and sufficient | Deterministic fact emission, reducer/projector truth, API/MCP response shape, capability truth, dead-code classification, cross-repo liveness, stale generations, tenant/repo scope boundaries, or no-provider-key evidence is covered by committed replay inputs and golden assertions. |
| Backend-required cassette/replay required | Correctness depends on real NornicDB/Neo4j behavior, Cypher dialect support, schema/index behavior, planner/hot-path eligibility, or exact emitted query shape against a live graph backend. |
| Scaled/performance replay required | Small replay may be correct but cardinality, fanout, queue depth, batching, graph write budgets, Postgres indexes, or p95/p99 latency can fail. |
| Full remote corpus required | Live collector behavior, clone/discover/parse cost, provider credentials, cross-service startup/restart behavior, image/runtime version drift, pprof/resource attribution, or queue-terminal guarantees are load-bearing. |

Wrong proof tier is a P1 unless it could ship wrong graph/query/deployment truth
or private data, in which case it is P0.

Pressure scenarios reviewers must distinguish:

- Dead-code semantics: cassette/golden replay is sufficient only when the
  library asserts live-by-consumer, unknown ownership, stale generations,
  cycles, tenant boundaries, API/MCP parity, evidence citations, confidence
  labels, and candidate bucket items.
- Graph write/retract timeout fixes: normal cassette truth is not enough;
  backend-required or scaled proof must expose graph-write timeout budgets.
- Reducer, materialization, or search-index long poles: replay can expose queue
  truth, but scaled or full-corpus proof is needed for latency and pprof.
- Parser regressions: collector cassettes are insufficient when they replay
  after collection or parse instead of exercising the broken parser path.
- Bootstrap or DDL restart waits: require fault-injection or live runtime
  restart proof rather than ordinary replay.
- Backend image or optimizer upgrades: cassette/golden replay proves functional
  truth, but backend-version, hot-path, startup, and performance proof need
  stronger validation.
