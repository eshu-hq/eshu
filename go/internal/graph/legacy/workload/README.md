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

## Contract

- Eligible means all of: legacy shape and no `generation_id`; no instance by
  edge or by `workload_id`; a scoped twin with a `generation_id` that the same
  repository `DEFINES`; only safe relationships (`DEFINES` from the node's own
  repository, `SAME_NAME` to another `Workload`).
- The first reason that applies is reported: `has_instances`, then `no_twin`,
  then `relationships`.
- The negative clauses are decided in Go on purpose. NornicDB mis-evaluates
  `EXISTS {}` and `OPTIONAL MATCH ... count`, and a property-on-the-left
  comparison against an expression of the same row. See
  `docs/internal/evidence/8795-legacy-workload-retirement.md`.
- `DeleteNeo4jCypher` re-evaluates every clause inside the statement, so the
  dry run is advisory and the delete is the gate. It is Neo4j only.

## Used by

- `go/internal/query` hosted readiness (`workload_identity_leftovers`).
- `go/internal/reducer/workload/legacyretire` (dry run and execute).
