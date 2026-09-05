# Performance Proof Plan

## Proof Ladder

Select the stopping rung before implementation from the changed contract and
acceptance criteria. Preserve input semantics and record each required result
before escalation. Local optimizations need representative exactness, applicable
concurrency proof, and a built-binary replay; a full-corpus run is required only
when the acceptance criteria or a repo-scale claim requires it. This selection
does not waive repository gates, explicit issue requirements, or real scale risks.

1. **Theory shim.** Use `EXPLAIN (ANALYZE, BUFFERS)`, Cypher `PROFILE`, a
   microbenchmark, or a throwaway query against representative worst-case data.
2. **Exactness.** For output-preserving work, prove bidirectional set difference
   0/0, identical ordered output, or equivalent counts. For a behavior fix,
   prove the explicit expected delta.
3. **Concurrency.** For claims, locks, leases, queues, DDL, or shared writers,
   prove contention, retry, idempotency, ordering, and failure recovery. Set
   equivalence alone is insufficient. Index and constraint candidates must
   prove first application, identical reapplication, restart/bootstrap
   behavior, and rollback on an isolated populated store. A fast first build
   does not authorize production DDL when repeated application mutates backend
   index state.
4. **Built-binary bounded replay.** Rebuild the production binary and run the
   worst-case repository, partition, scope, or backlog. Query-shape proof does
   not establish wall time.
5. **Small corpus, when scale or integration coverage is needed.** Progress
   through a representative large repository and small/medium proof before the
   credential-free 20-25 repository or equivalent bounded corpus. Verify
   graph/content/API truth.
6. **Full corpus, when required by acceptance.** Run once only after the
   previous rungs pass and the [scaled-run preflight](scaled-runs.md) matches a
   named baseline profile.

If a rung disproves the hypothesis, record it in the hypothesis ledger and do
not implement or retain an optimization justified by that hypothesis. A rejected
hypothesis is a valid result.

## Caller And Route Inventory

Before changing an index, readiness gate, cache, fallback, queue fence, or
shared state, inventory every caller and user-visible route. Search interfaces,
direct calls, indirect enrichment paths, pagination helpers, CLI, API, MCP,
background jobs, and recovery paths.

For each path state whether it:

- remains available;
- fails closed with a documented bounded error;
- uses a different exact index or scope;
- retries safely; or
- is intentionally outside the change.

Add tests for every distinct path class. Do not rely on final hostile review to
discover bypasses.
