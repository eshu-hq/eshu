# AGENTS.md - internal/searchrerank guidance for LLM assistants

## Read first

1. `go/internal/searchrerank/README.md` - package purpose, contract, signals.
2. `go/internal/searchrerank/rerank.go` - `Rerank`, fusion, states, basis.
3. `go/internal/searchrerank/signals.go` - handle-to-signal mapping and weights.
4. `go/internal/searchretrieval/README.md` - bounded retrieval result shape.
5. `go/internal/searchhybrid/AGENTS.md` - the fusion stage this reranks after.
6. `docs/public/reference/semantic-hybrid-search-admission.md` - search gate.

## Invariants this package enforces

- **Permutation only** - reranking reorders the input result set; it never adds,
  drops, or relabels a result. Upstream scope/authorization filtering is final.
- **Baseline preserved** - `RankingBasis` always carries the baseline rank and
  lexical/vector score; the original order is recoverable.
- **No graph call, no truth write** - signals come only from handles already on
  each curated document. No Cypher, no hosted call, no canonical graph write.
- **Deterministic** - equal inputs yield an equal outcome; ties break by baseline
  rank then document id.
- **Fail closed** - disabled, stale, or no-signal cases return the baseline order
  with a `State` that says why.
- **No content leak** - a `Contribution` exposes only `kind:id` and a weight.

## Common changes and how to scope them

- **Add a signal kind** - add the `SignalKind`, map its handle kinds in
  `handleSignal`, give it a default weight in `defaultWeights`, and add a focused
  test in `rerank_test.go` first. Ground new signals in handle kinds that curated
  documents actually carry (`searchdocs`).
- **Tune weights** - change `defaultWeights`; re-run the rerank benchmark
  evaluation and record the accept/reject decision before merge. Do not tune
  weights without measured evidence.
- **Change fusion** - keep the id tie-break and the "graph-anchored results only
  move up" property; update the README fusion section in lockstep.

## Anti-patterns specific to this package

- Calling NornicDB, Cypher, HTTP, MCP, or a hosted embedding API.
- Adding or removing results during reranking, or changing a truth label.
- Rewarding an anchored handle whose id does not match the request scope id.
- Returning `applied` when no signal fired, or hiding a stale-context fallback.
- Tuning weights to pass a benchmark without recording the measured evidence.
