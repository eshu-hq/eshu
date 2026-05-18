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
| `specs/capability-matrix.v1.yaml` | User-facing capability and truth contract by runtime profile. |
| `specs/backend-conformance.v1.yaml` | Backend behavior classes, profile gates, and promotion status for official graph adapters. |
| `go/internal/backendconformance/` | Go harness for parsing the backend matrix and running shared read/write corpora. |

## What The Harness Covers

The default Go harness is intentionally DB-free. It proves the matrix is valid,
that it names the same official backends as the capability matrix, and that the
shared read/write corpora can run against any adapter that satisfies Eshu's
graph ports.

The read corpus targets `GraphQuery`.

The write corpus targets the current Cypher executor family:

- `Executor`
- `GroupExecutor`
- `PhaseGroupExecutor`

The live corpus now includes the source-local canonical containment path:
repository, directory, file, function, and `File-[:CONTAINS]->Function`. The
live test runs those writes twice and then reads the relationship back with a
single-edge assertion. A backend can no longer pass the canonical-write smoke
test while skipping file/entity containment.

Eshu does not currently expose one concrete Go interface named `GraphWrite`.
When older ADR language says `GraphWrite`, read that as this Cypher write
executor family unless a later ADR formalizes a narrower interface.

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

Chunk 5b is tracked in the same backend matrix under `profile_matrix`.
NornicDB must carry a gate for each profile that can use an authoritative
graph backend:

- `local_authoritative`
- `local_full_stack`
- `production`

The `local_authoritative` gate is backed by the opt-in local-host performance
tests plus the latest full-corpus API/MCP evidence. The `local_full_stack` gate
is backed by the NornicDB Compose matrix, now running with
`ESHU_QUERY_PROFILE=local_full_stack`. The `production` gate now has a
latest-main full-corpus proof,
`eshu-full-pr138-a2c630af-b68b4ef-20260504T120630Z`:
it drained `8458/8458` queue rows in `878s`, kept retrying, failed, and
dead-letter rows at `0`, and passed API/MCP health plus relationship-evidence
drilldowns.

The early comparison evidence looked bad for Neo4j but did not include the
normal graph-schema bootstrap. The 2026-05-04 Neo4j run
`eshu-neo4j-baseline-pr138-a2c630af-20260504T123656Z` was stopped by request
after `1946s`; it was still clean (`0` retrying, failed, or dead-letter rows)
but had only `553/896` source-local projector items succeeded. NornicDB had
already finished the whole corpus at `878s`.

That stopped Neo4j snapshot was missing the product graph-schema bootstrap.
Product Compose and Kubernetes apply graph schema with
`eshu-bootstrap-data-plane` before indexing. When the remote Neo4j proof did the
same thing, `eshu-neo4j-schema-profile-20260504T182712Z` drained the full corpus
in `577s`, reached `8458/8458` succeeded queue rows with `0` retrying, failed,
or dead-letter rows, and passed API/MCP health plus relationship-evidence
drilldowns.

Neo4j now uses the shared row-scoped batched entity-containment writer through
ingester and bootstrap to reduce canonical statement count. NornicDB should not
inherit that default from Neo4j: its latest-main full-corpus path already
finished in `878s`, so cross-file batched containment remains an explicit
NornicDB evaluation switch. Future backend changes should stay inside the
shared Cypher contract and be measured as backend-specific evidence, not copied
blindly between backends.

The longer-term support bar is broader than these two databases. Eshu should be
able to support backends that speak the Bolt/Cypher shape, but only through
narrow, documented adapter seams. A new backend should not force handler-level
branches or a pile of one-off query workarounds. Neo4j is still useful for
companies that already pay for and operate Neo4j, but the Neo4j path cannot
remain dramatically slower than the default NornicDB path if we call it
production-promoted.

## Promotion Rule

Chunk 5 adds the deterministic harness, backend matrix, and live Compose-backed
adapter check. Chunk 5b records the profile-matrix proof across:

- `local_authoritative`
- `local_full_stack`
- `production`

NornicDB remains the default. Local and Compose profile gates pass against
latest `main`, production has NornicDB full-corpus evidence, and the corrected
schema-first Neo4j proof shows the official alternative can run the same corpus
inside the target envelope. Keep `eshu-bootstrap-data-plane` as a completed
schema bootstrap step in every production-profile proof before timing graph
writes.
