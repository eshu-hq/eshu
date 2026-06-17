# searchrerank

`searchrerank` is the opt-in graph-neighborhood reranking stage for Eshu
hybrid retrieval. It reorders the in-scope results a retrieval backend already
returned around code-to-cloud graph anchors, while preserving the baseline
lexical/vector ranking and never touching canonical graph truth.

## Why this package exists

Lexical and vector retrieval rank by text and embedding similarity alone. For a
service-story, incident, or supply-chain question the most relevant hit is often
the one tied to the *anchor* the caller scoped to — the service, its workload,
its deployment, an incident, a package, or an owner — even when its text score
is lower. This package promotes those anchored results in a measured, bounded,
explainable way.

It lives outside `searchhybrid` so that fusion package keeps its strict "no
graph call, no truth write" boundary. `searchrerank` honours the same boundary:
it reads only the graph handles already carried on each curated document, so it
issues no Cypher and makes no hosted call.

## Contract

- **Reorders, never mutates the set.** Reranking is a permutation of the input
  results. It never adds, drops, or relabels a result, so upstream scope and
  authorization filtering is preserved end to end.
- **Preserves the baseline.** Every result keeps its baseline rank and
  lexical/vector score in its `RankingBasis`; the original order is always
  recoverable.
- **Deterministic.** Equal inputs yield an equal outcome. Ties break by baseline
  rank then document id.
- **Opt-in and fail-closed.** When reranking is disabled, the graph context is
  stale, or no signal fires, the baseline order is returned and `State` records
  which fallback applied.
- **No content leak.** A `Contribution` exposes only the `kind:id` of a matched
  graph handle and a numeric weight, never document text.

## Signals

Signals are derived from the graph handle kinds curated documents already carry:

| Signal | Handle kinds | Anchored to scope |
| --- | --- | --- |
| `service_anchor` | `service` | service id |
| `workload_anchor` | `workload` | workload id |
| `deployment` | `runtime_summary`, `deployment` | — |
| `environment_anchor` | `environment` | environment |
| `incident` | `incident` | — |
| `package` | `container_image`, `package`, `image` | — |
| `owner` | `owner`, `team`, `ownership` | — |

A handle whose id matches the request scope anchor (e.g. a `service` handle equal
to the requested `service_id`) earns an anchor-match bonus so an in-scope anchor
outranks a merely present one. A handle of an anchored kind that points at a
*different* id is skipped rather than rewarded.

## Fusion

The baseline ranked list and the graph-signal ranked list are blended with
Reciprocal Rank Fusion (`k = 60`, matching `searchhybrid`). Each baseline result
contributes its baseline term; results whose graph signal fired contribute a
second term, so a graph-anchored result can only move up, never drop out.

## States

| State | Meaning |
| --- | --- |
| `disabled` | Reranking not requested; baseline returned, no signals computed. |
| `applied` | At least one graph signal fired; results fused into graph-aware order. |
| `inactive` | Requested but no signal fired for any result; baseline returned. |
| `stale_skipped` | Caller marked the graph context stale; failed closed to baseline. |

## Usage

```go
out := searchrerank.Rerank(results, searchrerank.Options{
    Enabled: true,
    Anchor:  req.Scope.Anchor(),
    Scope:   req.Scope,
})
// out.State, out.Results[i].Result, out.Results[i].Basis
```

## Boundaries

- Does **not** call NornicDB, Cypher, HTTP, MCP, or a hosted embedder.
- Does **not** write the canonical graph or promote a rerank score to truth.
- Does **not** read backend state; it operates only on the results passed in.
