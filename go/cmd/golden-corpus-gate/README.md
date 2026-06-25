# golden-corpus-gate

The typed assertion step of the **B-7 golden end-to-end corpus gate**
([#3800](https://github.com/eshu-hq/eshu/issues/3800)). It diffs a live pipeline
run against the **B-12 golden snapshot**
(`testdata/golden/e2e-20repo-snapshot.json`) and proves the four B-7 acceptance
buckets.

## What it asserts

| Phase  | Bucket    | Required findings | Advisory findings |
|--------|-----------|-------------------|-------------------|
| drains | B-7(a)    | `fact_work_items` residual ≤ bound; `shared_projection_intents` nonterminal ≤ bound (B-13 / #3859 gate, incl. `repo_dependency` subset detail) | — |
| graph  | B-7(b)    | required correlations exist (rc-1 deployable-unit, rc-3 DEPENDS_ON, ...) | per-label node / per-relationship edge counts vs snapshot tolerances |
| query  | B-7(c)    | each `query_shapes.http` response is 2xx and carries its required fields / minimum results | — |
| timing | B-7(d)    | pipeline wall time ≤ `budget-multiplier` × baseline | — |

**Why node/edge counts are advisory:** the snapshot ranges are calibrated for
the full 20-repo corpus with all nine credentialed collectors. The first landing
of the gate runs a **minimal corpus** (`-graph-required-only`, the default), so
the existence-style required correlations — which hold at any corpus size — are
the blocking graph assertions. Widening the gate to assert the full count
tolerances on the full corpus is tracked as a follow-up.

## How it fits the gate

This command does **not** run the pipeline. The orchestrator
`scripts/verify-golden-corpus-gate.sh` brings up Postgres + the graph backend,
runs `bootstrap-index` over the minimal repo corpus, replays the B-10 cassette
collectors, drives the reducer drain, starts `eshu-api`, then invokes this
command once per phase. Keeping the assertions here (typed, unit-tested) keeps
the shell orchestrator thin.

## Running

```bash
# Drains only (poll Postgres until both queues terminal, or time out):
ESHU_POSTGRES_DSN=... golden-corpus-gate -phase=drains \
  -snapshot=testdata/golden/e2e-20repo-snapshot.json -drain-timeout=10m

# Graph + query (after the API is up):
ESHU_GRAPH_BACKEND=nornicdb NEO4J_URI=... ESHU_API_KEY=... \
  golden-corpus-gate -phase=graph,query -api-base-url=http://localhost:8080

# Timing (orchestrator passes observed wall time):
golden-corpus-gate -phase=timing -budget-seconds=900 -elapsed-seconds=1100 -budget-multiplier=2
```

Environment variables match the services under test: `ESHU_POSTGRES_DSN`,
`ESHU_GRAPH_BACKEND`, `NEO4J_URI` / `NEO4J_USERNAME` / `NEO4J_PASSWORD` /
`NEO4J_DATABASE`, and `ESHU_API_KEY` for authenticated data endpoints.

Exit status is non-zero when any **required** finding fails. Advisory findings
print as `WARN` and never fail the gate. An empty report (no phase ran) fails:
a gate that asserted nothing proved nothing.

## Files

- `snapshot.go` — typed view + loader for the B-12 snapshot.
- `evaluate.go` — pure assertion logic for every phase (unit-tested).
- `drains.go` — Postgres drain queries + the drain poll loop.
- `graph.go` — Bolt graph counts (nodes, edges, required correlations).
- `query.go` — authenticated HTTP query-shape checks.
- `report.go` — finding aggregation, severity, and rendering.
- `runner.go` / `main.go` — flag parsing and phase orchestration.
