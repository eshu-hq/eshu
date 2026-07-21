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
| graph  | B-7(b)    | required correlations exist (rc-1 deployable-unit, rc-3 DEPENDS_ON, ...); required edge/node **properties** present (e.g. `source_tool` on Tier-2 edges, `language` on `File` nodes) | per-label node / per-relationship edge counts vs snapshot tolerances |
| query  | B-7(c)    | each `query_shapes.http` response is 2xx and carries its required fields, minimum results, and declared deep JSON fields / values | — |
| demo-answers | #4776 | each of the five `specs/demo-first-answers.v1.yaml` questions, executed live with its **specific** pinned arguments (a playbook via its `surface.execute` target), returns a populated answer (required fields present, `minimum_results` met) | — |
| timing | B-7(d)    | total pipeline wall time ≤ `budget-multiplier` × baseline; with `-phase-timings-file` (B-11 / #3804) each **gated** phase ≤ baseline band/slack in `e2e-baseline.json` | per-phase findings are advisory under `-phase-regression-advisory` (shared CI) |

**Node/edge count tolerances are required (#3866):** the gate runs the full
20-repo corpus with `-graph-required-only=false`, so every snapshot
`node_counts`/`edge_counts` range is a blocking assertion. The ranges are
calibrated to the **real deterministic corpus output** (not aspirational
projections) — floors catch a major projection drop (e.g. the #4019 nested-file
loss) and ceilings stay wide for parser growth. Families the corpus must not
produce assert `max: 0`: the SecretsIAM graph projection is governance-gated OFF
by `ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED` (ADR #1314), so any nonzero
SecretsIAM count would mean the gate enabled a governed feature — which it must
not. The existence-style required correlations (corpus-size-independent) remain
the backbone; the count tolerances lock cardinality on top of them.

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

## Property assertions (source-tool / language provenance)

Edge types and node labels alone do not prove **provenance** (#3997): a
shared-verb edge like `DEPENDS_ON` is emitted by several tools, and a `File`
carries a `language`. The snapshot can therefore assert *properties*, not just
counts:

- **Edge properties** on a `required_correlations` entry. `required_edge_properties`
  lists relationship properties every matching edge must carry (non-empty);
  `allowed_edge_property_values` optionally pins each to a canonical vocabulary.
  The matching set is the entry's `evidence_kinds`-narrowed edges, so the check is
  *absence-zero* (every isolated edge must be stamped) while the companion
  `minimum_count` guards that the set is non-empty.
- **Node properties** via a `required_nodes` entry (`required_node_properties`,
  `allowed_node_property_values`). The check is *presence-positive*: at least
  `minimum_count` nodes of the label must carry a non-empty (and, when pinned,
  allowed) value. A label like `File` legitimately holds property-less nodes
  (`LICENSE` has no `language`), so a floor of correctly-tagged nodes is asserted
  rather than the absence of any untagged node.

Both are additive and default to off, so an entry without property fields behaves
exactly as before. A missing or un-normalized property fails the gate with a
message naming the verb/label, the property, and the offending/short count — so a
provenance regression can no longer pass silently.

## Self-loop assertions (per-language recursion truth)

`required_self_loops` (issue #5349) pins the count of
`(n:Label {node_property: node_property_value})-[:Relationship]->(n)`
self-loop edges — same source and target node — to a closed
`[minimum_count, maximum_count]` range, not a floor. A floor-only assertion
cannot tell "genuine recursion survives" apart from "a declaration was
misclassified as a call to itself" (the [#5332](https://github.com/eshu-hq/eshu/issues/5332)
class of bug): both push the observed count up. `node_property`/
`node_property_value` (e.g. `language`/`dart`) scope the match to one
language sharing a node label such as `Function`, so one language's
self-loop count is never conflated with another's. See
`sl-dart-calls-recursion` in the committed snapshot, pinned exactly at 2
against `tests/fixtures/ecosystems/dart_comprehensive/calls.dart`'s
arrow-form and block-form recursive self-calls.

## Query shapes

`query_shapes.http` supports bounded `GET` reads and read-style `POST` queries
with a declared JSON `request_body`. Shapes that set `envelope: true` ask the API
for `application/eshu.envelope+json` and assert the returned `{data, truth,
error}` object directly. MCP shapes use the same flag to keep the tool envelope
instead of unwrapping `data`.

`query_shapes.cli` makes the CLI a first-class read surface for C-9. CLI rows
declare the `eshu` argv, required response fields, truth class, and optional
`parity_with` peers such as `http:GET /api/v0/repositories` or
`mcp:list_indexed_repositories`. The query phase evaluates this metadata
offline, so a CLI row cannot claim API/MCP parity without naming the exact peers
and matching their truth class.

Deep assertions use explicit dot paths. A segment ending in `[]` traverses a
non-empty array, so
`data.candidate_buckets.live_by_consumer[].consumer_evidence[].citation`
proves at least one cited consumer evidence row exists. `required_json_values`
pins deterministic values such as `truth.level`, `truth.basis`, and
`data.query_shape`. The matcher evaluates only the declared paths; it never
scans an entire response.

## Files

- `snapshot.go` — typed view + loader for the B-12 snapshot.
- `evaluate.go` — pure assertion logic for every phase (unit-tested).
- `drains.go` — Postgres drain queries + the drain poll loop.
- `graph.go` — Bolt graph counts (nodes, edges, required correlations) and
  edge/node property listing for the provenance property assertions.
- `query.go` — authenticated HTTP query-shape checks.
- `mcp.go` — live MCP tool query-shape checks over `POST /mcp/message`.
- `demoanswers.go` — the demo-answers phase: load `specs/demo-first-answers.v1.yaml`
  via `go/internal/demospec`, execute each question live (its `surface` or, for a
  playbook, its `surface.execute` target), and assert a populated answer (#4776).
- `report.go` — finding aggregation, severity, and rendering.
- `runner.go` / `main.go` — flag parsing and phase orchestration.
