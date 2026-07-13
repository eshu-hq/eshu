# Issue #2678 — graph-neighborhood reranking accept decision

Graph-neighborhood reranking is an opt-in stage (`internal/searchrerank`) that
reorders the already-retrieved, in-scope semantic-search results around
code-to-cloud graph anchors (service story, deployment unit, environment,
incident, package, owner). It preserves each result's baseline lexical/vector
score, exposes a per-result ranking basis, and fails closed to the baseline
order when it is disabled, when no graph signal fires, or when graph context is
stale.

This record captures the measured accept decision the epic gate (#2677) and the
parent epic (#2676) require: *graph reranking improves or is explicitly rejected
with measured evidence.*

## Decision: ACCEPTED (opt-in)

Reranking is accepted as an **opt-in** retrieval stage. It strictly improves
ranking quality on the graph-anchored cases and is held neutral when no graph
signal is present, so it cannot regress non-anchored queries. It remains off by
default until adopted per surface.

## Benchmark inputs

- Harness: `go test ./internal/searchrerank -run Benchmark -v -count=1`
  (`TestRerankBenchmarkAcceptanceEvidence`).
- Deterministic and public-safe: a labeled in-memory fixture suite, no
  credentials, no private source, no network. Reproducible byte-for-byte.
- Metric: binary-relevance nDCG@3, baseline (lexical/vector order) vs reranked.
- Suite: four cases — a service-story anchor, an incident-context anchor, a
  supply-chain package anchor, and a no-graph-signal control.

The fixtures are minimal and adversarial by construction: each anchored case
places the relevant document one rank below a lexical near-miss so the benchmark
measures whether graph reranking recovers it, and the control has no graph
signal so reranking must hold it neutral. This suite is a deterministic CI gate
that proves the fusion, signal extraction, and fail-closed behavior are correct;
it is not sampled from production and does not substitute for production
retrieval telemetry when tuning weights.

## Measured result

| Case | Baseline nDCG@3 | Reranked nDCG@3 | Rerank state |
| --- | --- | --- | --- |
| `service_story_anchor` | 0.6309 | **1.0000** | `applied` |
| `incident_context_anchor` | 0.6309 | **1.0000** | `applied` |
| `supply_chain_package_anchor` | 0.6309 | **1.0000** | `applied` |
| `no_graph_signal_neutral` | 1.0000 | 1.0000 | `inactive` |
| **Suite mean** | **0.7232** | **1.0000** | 3/4 improved |

The graph-anchored cases recover the relevant document that the lexical/vector
order ranked below a near-miss; the control case has no graph signal, so
reranking returns `inactive` and the order is unchanged. No case regressed.

## Acceptance thresholds (gate)

The benchmark fails (REJECT) if either holds:

1. Suite mean reranked nDCG@3 is below the baseline mean (any regression), or
2. fewer than three graph-anchored cases strictly improve.

These thresholds are enforced in code by `TestRerankBenchmarkAcceptanceEvidence`,
so a future change that degrades reranking fails CI rather than silently
shipping.

## Truth and safety properties (tested separately)

- Permutation only — the result set, scores, and truth labels are unchanged
  (`TestRerankPreservesResultSetAndIsDeterministic`).
- Fail closed — disabled, stale, and no-signal paths return the baseline order
  with an explicit state (`TestRerankDisabledReturnsBaseline`,
  `TestRerankStaleFailsClosed`, `TestRerankInactiveWhenNoSignalsFire`).
- No content leak — a contribution exposes only the `kind:id` handle and a
  weight (`TestRerankAppliedPromotesAnchoredResult`).
- API/MCP exposure — the route and tool surface `rerank`, per-result
  `ranking_basis`, and `recommended_next_calls`
  (`TestSemanticSearchHandlerRerankPromotesServiceAnchoredResult`,
  `TestSemanticSearchToolPassesRerankFlag`).

## No-Observability-Change

Reranking adds opt-in response fields (`rerank`, `ranking_basis`,
`recommended_next_calls`) and no new runtime flag, graph write, or egress. It
issues no extra graph read: signals come only from handles already on each
retrieved document, so the bounded retrieval observation contract is unchanged.

## Reproduce

```bash
cd go && go test ./internal/searchrerank -run Benchmark -v -count=1
```
