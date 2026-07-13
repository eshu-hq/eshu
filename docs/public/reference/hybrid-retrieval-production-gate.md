# Hybrid Retrieval Production Gate

This is the admission gate for treating Eshu `semantic` and `hybrid` search as
**production-grade** retrieval rather than a working-but-degraded capability. It
defines the benchmark inputs, the acceptance thresholds, the degradation
classes, and the no-regression expectations for the API and MCP surfaces.

It complements [Semantic Hybrid Search Admission](semantic-hybrid-search-admission.md),
which defines *when the route may serve* a real embedder-backed path. This gate
defines *when that path is good enough to call production-grade*, with measured
thresholds.

The gate is executable, not just prose: the thresholds and the decision logic
live in `go/internal/searchbench/production_gate.go`
(`ProductionGateThresholdsFor`, `EvaluateProductionGate`) and are pinned by
`production_gate_test.go`, so the doc and the code cannot drift.

## Truth boundary

Search rank, BM25 score, vector similarity, Reciprocal Rank Fusion score, and
graph-reranking boost are **derived retrieval evidence**. They must never create
or overwrite canonical graph truth. A run that emits any false canonical claim
is rejected regardless of its accuracy or latency.

## Benchmark inputs

The gate is measured by the searchbench harness (`go/internal/searchbench`,
`go/cmd/search-bench`). The benchmark must record **public-safe** evidence: no
credentials, no private source text, no provider keys, no machine-specific
hosts or paths.

| Input | Requirement |
| --- | --- |
| Corpus | A stated repository corpus with recorded indexed-document count, fact count, and commit id. Corpus size band must be named (single repo, 20–25 repo, or full corpus). |
| Query suite | A versioned `QuerySuite` (`semantic-retrieval-query-suite/v1`) of at least `MinimumQuerySuiteSize` (15) queries, each with bounded scope, mode, and `limit`. |
| Relevance labels | Each query declares its expected graph handles / documents. Labels are derived deterministically from the corpus, never hand-tuned per run. |
| Latency | Per-mode `p50` and `p95` over a stated query count and round count. |
| Vector coverage | The fraction of the in-scope corpus that carries a compatible, current vector (matching the deterministic hash or the active provider schema). |
| Degradation state | The `retrieval_state` and any failure classes observed during the run. |

## Acceptance thresholds

Thresholds are split by profile because the deterministic local hash embedder
and a governed production provider are different quality regimes. A passing
**local** run is a proof, not production readiness; only the **production
provider** profile can reach `production_ready`.

| Threshold | Local deterministic | Production provider |
| --- | --- | --- |
| Min recall | 0.60 | 0.80 |
| Min precision | 0.50 | 0.70 |
| Min nDCG | 0.60 | 0.80 |
| Max p95 latency | 50 ms | 150 ms |
| Min vector coverage | 0.95 | 0.98 |
| False canonical claims | 0 | 0 |

These numbers are the source of truth in
`ProductionGateThresholdsFor`. Recall, precision, and nDCG are measured at the
suite's bounded `limit`; p95 is the per-query latency budget; vector coverage is
the fraction of in-scope documents with a compatible current vector.

## Decision classes

`EvaluateProductionGate(profile, run, vectorCoverage)` returns one decision and
the unmet thresholds:

| Decision | Meaning |
| --- | --- |
| `production_ready` | A production-provider run met every threshold with zero false canonical claims. |
| `local_proof_passed` | A local deterministic run met the local bar. It is explicitly **not** production-ready on its own. |
| `degraded` | The vector path did not participate, or vector coverage was below the minimum. The run is keyword-degraded and not evaluable as semantic retrieval. |
| `rejected` | The run failed a measured recall/precision/nDCG/p95 threshold, emitted a false canonical claim, omitted the false-canonical measurement, or named an unknown gate profile. |

The order is deliberate. A run is rejected up front when its gate profile is
unknown (so a typo or unset config can never be admitted on the lenient local
thresholds) and when its false-canonical-claim count is missing (the truth-safety
measurement is required, never assumed zero). A run is then classified `degraded`
before any accuracy or latency threshold is judged, because semantic quality is
not evaluable without a vector path. This makes the answer to "is semantic search
production-ready or degraded?" explicit rather than implied.

## Degradation and freshness handling

Degradation reuses the admission contract's retrieval states and failure
classes. The gate treats these as `degraded`, never `rejected`, because they are
readiness conditions, not quality failures:

- `semantic_unavailable` / `index_unready` — no governed embedder or vector
  index answered.
- `hybrid_degraded` — hybrid requested but only keyword candidates participated.
- `vector_index_stale` / `vector_index_partial` / `vector_index_building` — the
  vector index is behind, partial, or still building.

Stale, partial, and unavailable states must surface in `retrieval_state` and the
failure classes; they must not be hidden behind a claim of semantic or hybrid
participation.

`EvaluateProductionGate` itself sees only the run's measured metrics, its search
flags, and the supplied vector coverage — not `retrieval_state` or the failure
classes. The caller is therefore responsible for translating a stale, partial, or
building vector index into a reduced `vectorCoverage` (or clearing the vector
flags) before calling the gate, so those readiness states resolve to `degraded`.
A run whose flags claim a vector path but that served only keyword candidates in
practice is a `hybrid_degraded` case the caller must reflect the same way; the
gate cannot infer it from the flags alone.

## Graph-reranking gate

Graph-neighborhood reranking (`go/internal/searchrerank`) is gated separately and
is **accepted as opt-in** with measured evidence: mean nDCG@3 improved 0.7232 →
1.0000 over a labeled fixture suite with no regression and the no-signal case
held neutral. The accept decision, thresholds, and reproduction are recorded in
[issue-2678 graph-rerank evidence](https://github.com/eshu-hq/eshu/blob/main/docs/internal/evidence/searchbench-evidence/2678-graph-rerank.md).
Reranking never changes the result set, scores, or truth labels, so it does not
affect the retrieval thresholds above; it only reorders the in-scope results.

## No-regression expectations (API and MCP)

The production gate also bounds the wire surfaces so adopting it cannot regress
existing behavior:

- The `POST /api/v0/search/semantic` route and the `search_semantic_context`
  MCP tool keep their bounded contract: required `repo_id`, `query`, `mode`,
  `limit`, `timeout_ms`, and explicit `retrieval_state`, `truncated`, and
  per-result `search_method`.
- Keyword behavior is unchanged when no embedder is configured: the route serves
  deterministic BM25 and reports `keyword_only` or `hybrid_degraded`.
- API and MCP report the same retrieval state and method for the same request
  (parity), and scoped-token no-grant and out-of-grant behavior is unchanged.
- Reranking is opt-in and absent from the response unless requested, so existing
  callers see no new fields.

## Public-safe evidence

Checked-in evidence lives under
[`docs/internal/evidence/searchbench-evidence/`](https://github.com/eshu-hq/eshu/tree/main/docs/internal/evidence/searchbench-evidence)
and must be reproducible without credentials or private source. A gate evaluation records
the profile, the corpus band and commit, the measured metrics and p95, the
vector coverage, and the resulting decision.

## Verification

```bash
cd go && go test ./internal/searchbench -run ProductionGate -count=1
cd go && go test ./internal/searchbench ./internal/searchrerank ./internal/searchretrieval \
  ./internal/searchdocs ./internal/searchhybrid -count=1
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

## Related docs

- [Semantic Hybrid Search Admission](semantic-hybrid-search-admission.md)
- [Semantic Search Route](http-api/semantic-search.md)
- [Search Retrieval Contract](search-retrieval-contract.md)
- [issue-2678 graph-rerank evidence](https://github.com/eshu-hq/eshu/blob/main/docs/internal/evidence/searchbench-evidence/2678-graph-rerank.md)
