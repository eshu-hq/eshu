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

The live check runs the same corpus against a real Bolt endpoint. It is opt-in
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
