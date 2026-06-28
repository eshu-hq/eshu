# Evidence — NornicDB grouped writes commit per dependency phase (#4027)

Scope: `go/cmd/bootstrap-index/nornicdb_wiring.go` and
`go/cmd/ingester/wiring_nornicdb_config.go`. The
`ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` toggle previously routed NornicDB
canonical writes through the bare whole-materialization `GroupExecutor` (one
`ExecuteGroup` for every node phase). Both toggle states now route to the
per-dependency-phase executor (`nornicDBPhaseGroupExecutor` /
`bootstrapNornicDBPhaseGroupExecutor`).

## Correctness (the bug)

On NornicDB an `UNWIND $rows … MATCH {prop: row.x}` does not observe a node
`MERGE`'d earlier in the same transaction (no within-transaction read-your-writes
for row-property MATCH). Under whole-materialization atomic writes the
directory-edge, file, entity, and containment phases MATCH directory/file/entity
nodes the node phases MERGE in that same transaction, resolve nothing, and the
entire file tree is silently dropped.

- Backend: NornicDB (default canonical backend), 20-repo golden corpus.
- Statement shapes affected: `canonicalNodeDirectoryDepthNEdgeCypher`,
  `canonicalNodeFile*`, entity-upsert, entity-containment — all
  `UNWIND … MATCH {path|uid: row.x}` against same-transaction MERGEs.

## No-Regression Evidence:

Golden-corpus gate (`scripts/verify-golden-corpus-gate.sh`) run with the toggle
forced on, `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=true`:

- Before this change: 0 File nodes corpus-wide; 16 Directory nodes; 0 directory
  edges (every downstream UNWIND-MATCH phase resolved nothing).
- After this change: gate GREEN — `rc-12 (Class)-[:INHERITS]->(Class) count=6`
  (>=1), `rc-35 (HelmTemplateValueUsage)-[:REFERENCES]->(HelmValueDefinition)`
  with `evidence_kinds⊇[HELM_TEMPLATE_VALUE_REFERENCE] count=8` (>=8); summary
  `51 pass, 0 required-fail, 0 advisory-warn`; elapsed 37s (budget ceiling
  1800s). rc-12 and rc-35 both require the nested file/entity tree to project, so
  green proves the full tree now materializes under the toggle.

No throughput regression: the per-phase executor is the default production path
(toggle off already used it). This change only stops the conformance toggle from
selecting the broken whole-materialization path; it adds no new write, no extra
round trip, and no serialization. The default (toggle-off) path is byte-for-byte
unchanged. The reducer semantic-entity grouped path is deliberately unchanged —
it is MERGE-by-uid and MATCHes pre-committed nodes, proven correct by the same
toggle-on gate (rc-12 needs semantic Class nodes).

## Observability Evidence:

When the toggle is set, both writers emit a `slog.Warn` at the routing decision:
`"NornicDB canonical grouped writes requested; committing per dependency phase —
whole-materialization atomic is unsupported on NornicDB (#4027)"` with
`graph_backend` and `env_var` attributes, so an operator who enables the
conformance toggle sees, in the bootstrap-index / ingester logs, that the request
was honored as a per-phase commit rather than silently degraded. No metric or
span contract changes.

## Unit guards

- `go/cmd/ingester/wiring_nornicdb_phase_group_logging_test.go`:
  `TestCanonicalExecutorForGraphBackendGroupedWritesUsePhaseGroupNornicDB`,
  `TestCanonicalExecutorForGraphBackendGroupedFullStackUsesPhaseGroups` — assert
  the grouped executor is the per-phase executor (never `GroupExecutor`) and the
  writer commits per phase.
- `go/cmd/bootstrap-index/nornicdb_grouped_4027_test.go`:
  `TestBootstrapCanonicalExecutorGroupedWritesStillUsesPerPhase`.
