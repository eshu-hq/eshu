# Backend Conformance

Backend conformance is the gate that keeps graph adapters honest.

Eshu supports two official graph backends today:

- NornicDB, the default backend
- Neo4j, the official alternative backend

Both backends serve the same user-facing API and MCP capabilities. They do not
get to be called supported just because they accept Cypher. They have to pass
the same contract checks for reads, writes, traversal shape, dead-code
readiness, and performance evidence.

## Files

| File | Purpose |
| --- | --- |
| `specs/capability-matrix.v1.yaml` and fragments under `specs/capability-matrix/` | User-facing capability and truth contract by runtime profile. |
| `specs/backend-conformance.v1.yaml` | Backend behavior classes, profile gates, and promotion status for official graph adapters. |
| `go/internal/backendconformance/` | Go harness for parsing the backend matrix and running shared read/write corpora. |

## What The Harness Covers

The default Go harness is DB-free. It validates the matrices and shared corpora.
The live harness runs those corpora against a real Bolt endpoint.

| Corpus | Target | Required behavior |
| --- | --- | --- |
| Read | `GraphQuery` | Same bounded read contract for each official backend. |
| Write | `Executor`, `GroupExecutor`, `PhaseGroupExecutor` | Same Cypher executor contract for canonical and reducer writes. |
| Canonical containment smoke | repository, directory, file, function, and `File-[:CONTAINS]->Function` | Run writes twice, then read the edge back with a single-edge assertion. |

Function/Class source-local identity parity is covered by projector and schema
unit evidence, not by the live backend-conformance corpus yet. Neo4j enforces
`(name, path, line_number)` directly; NornicDB enforces the projector-derived
`uid` created from the same identity tuple before graph write.

Eshu does not currently expose one concrete Go interface named `GraphWrite`.
When older docs say `GraphWrite`, read that as this Cypher write executor
family unless a current reference page formalizes a narrower interface.

## Live Backend Check

The live check runs the shared corpus against a real Bolt endpoint. It is opt-in
so normal unit tests stay fast:

```bash
ESHU_GRAPH_BACKEND=nornicdb ./scripts/verify_backend_conformance_live.sh
ESHU_GRAPH_BACKEND=neo4j ./scripts/verify_backend_conformance_live.sh
```

The script defaults to the local Compose credentials and database names:
`nornic` for NornicDB and `neo4j` for Neo4j. Override the usual Bolt variables
when you are testing a different target:

```bash
ESHU_GRAPH_BACKEND=nornicdb \
ESHU_NEO4J_URI=bolt://localhost:7687 \
ESHU_NEO4J_USERNAME=neo4j \
ESHU_NEO4J_PASSWORD=change-me \
ESHU_NEO4J_DATABASE=nornic \
./scripts/verify_backend_conformance_live.sh
```

GitHub Actions runs this live check in the end-to-end matrix before
`bootstrap-index`, so both official backends prove the shared read/write corpus
against a clean graph service.

### Exact-row cases

Most read cases assert a minimum row count. Some shapes can return the right
number of rows with the wrong values, so those cases carry the exact expected
rows instead, compared as a multiset (row order is ignored):

- the value-flow cloud sink statements the reducer runs, pinned to the
  production constants by equality (#6690);
- the aggregation and optional-match shapes that returned wrong answers with no
  error on older NornicDB builds: `count(DISTINCT)` and `collect(DISTINCT)` over
  repeated rows, a node-only anchor followed by two `OPTIONAL MATCH` clauses, and
  an `OPTIONAL MATCH` after an aggregating `WITH` (#6689).

They are part of the default corpus, so the end-to-end matrix runs them on both
backends on every change that selects it. A backend regression on any of these
shapes fails the blocking live check instead of reaching an API or MCP answer.

The value-flow cases used to have their own workflow, which ran them with the
expectation inverted while the old single-statement query returned zero rows on
NornicDB. After #6690 they pass on both backends, so that workflow is retired
and the end-to-end matrix is their gate.

## B-7 Golden Corpus On Both Backends

The B-7 golden corpus gate (`scripts/verify-golden-corpus-gate.sh`) runs the
real pipeline end to end and diffs the graph and every HTTP/MCP query shape
against one snapshot, `testdata/golden/e2e-20repo-snapshot.json`. It runs once
per backend: `corpus-gate (nornicdb)` and `corpus-gate (neo4j)` in
`golden-corpus-gate.yml`. Locally, `ESHU_GRAPH_BACKEND=neo4j` selects Neo4j; see
[Golden Corpus Gate](local-testing/golden-corpus-gate.md#running-it-on-neo4j).

The snapshot was calibrated on NornicDB, so the first Neo4j run is a
differential oracle, not a formality. A shape that passes on one backend and
fails on the other means one backend's behavior disagrees with the documented
contract. Decide which one from the contract and fix Eshu there; never loosen
the snapshot to make the pair agree. The first run (#6782) found one defect on
each side:

| Shape | Backend at fault | Cause | Fix |
| --- | --- | --- | --- |
| `find_function_call_chain` from `recursionFib` to itself | Neo4j route | Legacy `shortestPath()` raises `Neo.DatabaseError.Statement.ExecutionFailed` when start = end, so a self-recursive request returned HTTP 500. | The compat builder runs a GQL `SHORTEST 1` search in a scoped `CALL` subquery, with each hop bound inside the quantified pattern. It needs Neo4j 5.23 or later. |
| `get_repo_context` `source_tool_breakdown` for `orders-api` | NornicDB write path | The batched repo-dependency writer omitted `source_tool` from UNWIND rows that had none. NornicDB v1.3.3 stored the literal text `row.source_tool`, while Neo4j left the edge unstamped as the [provenance contract](edge-source-tool-provenance.md) requires. | The writer always sends the key, `nil` when absent. |

Both fixes carry live tests (build tag `live_nornicdb_answer_truth`) that select
their backend in each test's preamble: NornicDB unless the preamble's test-only
backend knob names Neo4j, with `ESHU_NEO4J_URI` pointing at that leg's container
(see the preamble in `go/internal/query/codequery/chain/self_recursion_live_test.go`
for the exact knob). Setting the URI without the knob silently runs the NornicDB
path, so run each test on both backends before changing either path.

Two known differences still pass on both backends, and neither is settled yet.
Transitive `CALLS` on `POST /api/v0/code/relationships` returns the start node
and repeat depths on the Neo4j route, but each node once on the NornicDB
breadth-first route (#6849). And the Class `INHERITS` count varies run to run
on both backends, 22 or 23 on each, so it is not a backend divergence (#6850).
Both are recorded in `docs/internal/evidence/6782-b7-neo4j-divergences.md`.

## Profile Matrix

The backend matrix carries a `profile_matrix` gate for every authoritative graph
profile: `local_authoritative`, `local_full_stack`, and `production`.

Current evidence:

| Profile | Required proof |
| --- | --- |
| `local_authoritative` | Opt-in local-host performance tests plus API/MCP truth checks against the completed graph. |
| `local_full_stack` | Compose matrix with `ESHU_QUERY_PROFILE=local_full_stack`. |
| `production` | Full-corpus, schema-first proof with queue-zero and API/MCP relationship-evidence checks. |

The durable lesson from the Neo4j comparison is schema-first timing. A stopped
Neo4j snapshot without `eshu-bootstrap-data-plane` was not production evidence;
the corrected run applied schema first and drained the full corpus cleanly.
Keep schema bootstrap complete before timing either backend.

Future backend changes must stay inside the shared Cypher/Bolt contract and be
measured as backend-specific evidence. Do not copy a NornicDB or Neo4j tuning
default into the other backend unless a same-shape proof supports it.

## Promotion Rule

NornicDB remains the default. Neo4j is the official alternative when it passes
the same matrices, live corpus, profile gates, and schema-first performance
evidence. Keep `eshu-bootstrap-data-plane` complete before every
production-profile graph timing.
