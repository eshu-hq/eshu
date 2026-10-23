# legacy/workload

Detection and retirement-safety rules for the name-only `Workload` nodes left
over from the repository-scoped identity change (#5385, tracked in #8795).

## Why it exists

After the cutover a `Workload` should be keyed `workload:<repo_id>:<name>`.
A deployment that missed the cutover's delete step still holds the old
`workload:<name>` node beside the new one. A by-name lookup then sees two
candidates for one service and the resolver (correctly) refuses to guess, and
the reducer's handle ladder skips edges as `workload_handle_ambiguous`.

## What it owns

| piece | file | what it does |
|---|---|---|
| `Candidate`, `Facts`, `Verdict`, `Evaluate`, `IsLegacyID`, `TwinID` | `evaluate.go` | pure decision logic over facts already read from the graph |
| `Detect`, `Options`, `Report`, `Reader` | `detect.go` | pages the candidates and reads the facts through a graph read port |
| read statements, `DeleteNeo4jCypher`, `MaxBatch` | `cypher.go` | the Cypher text; one definition shared by every caller |
| `RetireRequest`, `RetireResponse`, `RetireCandidate`, `ErrInvalidRequest`, `ErrBackendUnvalidated`, `ErrWriterUnavailable` | `retirement.go` | the admin route contract, shared by `internal/runtime` (HTTP) and the reducer runner |

## Contract

- Eligible means all of: legacy shape and no `generation_id`; no instance by
  edge or by `workload_id`; a scoped twin with a `generation_id` that the same
  repository `DEFINES`; only safe relationships (`DEFINES` from the node's own
  repository, `SAME_NAME` to another `Workload`).
- The first reason that applies is reported: `has_instances`, then `no_twin`,
  then `relationships`.
- A name or repository id with leading or trailing whitespace has no twin, because
  the delete joins them byte for byte (`TwinID` returns empty). The node is
  reported `no_twin`, and `WithScopedTwin` does not count it, so readiness can
  pass while it still makes the name ambiguous. Known limit; the operator fixes
  the padded value at the source.
- The negative clauses are decided in Go on purpose, so one code path serves
  readiness, the dry run, and the pre-delete verification, and the reads avoid
  the shapes `docs/public/reference/nornicdb-pitfalls.md` records as unreliable
  on NornicDB. Only Neo4j was tested, by owner direction. See
  `docs/internal/evidence/8795-legacy-workload-retirement.md`.
- `DeleteNeo4jCypher` takes each named node's write lock first and then
  re-evaluates every clause inside the statement, so the dry run is advisory
  and the delete is the gate. A writer that changes the node or its
  relationships while the delete runs either makes the delete wait and re-read
  (a property write) or makes Neo4j abort one of the two transactions (a new
  relationship); `TestLiveDeleteNeo4jRechecksAfterTheLockWait` proves both. A
  writer that touches neither (a `WorkloadInstance` naming the node only by its
  `workload_id` property, or the twin losing its `DEFINES`) is not seen, so
  retirement runs in a quiet window against one reducer pod. It is Neo4j only.

## Why a separate leaf package

`go/internal/query` (hosted readiness), `go/internal/runtime` (the admin
route), and the reducer all need the same detection or its contract types.
`query` and `runtime` must not import the reducer, and this code has nothing
to do with the canonical writers, so it is not in a reducer or
`storage/cypher` package. This package depends only on `workloadid` and the
standard library, so every side imports it and none imports another.

## Used by

- `go/internal/query` hosted readiness (`workload_identity_leftovers`).
- `go/internal/reducer/workload/legacy/retire` (dry run and execute).
- `go/internal/runtime` admin route (request, response, and error types only).
- `go/internal/query/entity` service story: the `legacy_id` marker on a
  service workload candidate (`service_workload_candidate_maps.go`).
- `go/cmd/reducer` wires the retirement runner onto the reducer's Neo4j
  executor.
