# Evidence: stale-USES retract shapes live on the pinned Neo4j backend (#8517 P1)

Probe P1 from `docs/internal/design/5385-workload-identity-migration.md` §6,
run against the pinned primary backend. Every named retract shape deletes its
seeded edge: bare `DELETE` and the production retract both clear, so no shape
is inert and no hazard entry or retract-by-node writer change is needed.

## Backend

- Pin: `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
  (`docker-compose.neo4j.yml:3`), reporting `neo4j_version 2026.08.1`,
  `neo4j_edition community`.
- Proof container `eshu-8517-neo4j-proof` on this host (Bolt
  `127.0.0.1:19687`, no-auth pin), image digest verified identical to the pin.
- Eshu commit: `01d6e117dd` plus the probe file itself.

## Command

```bash
ESHU_CYPHER_BOLT_DSN=neo4j://127.0.0.1:19687 ESHU_CYPHER_BOLT_DATABASE=neo4j \
  go test ./internal/reducer/workload/retract/ \
  -run 'TestLiveRelationshipDeleteEffectiveness8517' -v -count=1
```

Probe:
`go/internal/reducer/workload/retract/delete_effectiveness_live_test.go`.
Per shape: seed, assert count, bare `DELETE`, settle to the want, clean,
re-seed, assert count, production retract, settle to the want. Every read
polls to a 30 s deadline, so a stale post-write read cannot pass. Ids carry a
per-run `p8517-<nanos>` prefix and are `DETACH DELETE`d afterwards.

## Results (all on the pinned backend, one trial per cell)

Binary works/does-not-work outcomes with a settling loop on every read, not
latencies: a single sample distinguishes 1 → 0 from 1 → 1 in a way it could
never distinguish two timings.

| Shape | Production statement (verbatim) | Bare `DELETE` | Production retract |
| --- | --- | --- | --- |
| `RUNS_ON` single-repo | `RetractSingleRepoRunsOnEdgesCypher` (`canonical_relationships.go:371`) | 1 → 0 | 1 → 0 |
| `DEFINES` | `definesCypher` via `RepositoryEdges` (`retract/retract.go:30`, reader=nil keep-list path) | 2 → 1, keep survives | 2 → 1, keep survives |
| `DEPENDS_ON` (workload) | `RetractWorkloadDependencyEdgesCypher` (`canonical.go:236`) | 1 → 0 | 1 → 0 |
| `RUNS_IN` | `RetractRunsInEdgesCypher` (`canonical_runs_in_edges.go:41`) | 1 → 0 | 1 → 0 |
| `DOCUMENTS` | `RetractDocumentationEdgesCypher` (`canonical_documentation_edges.go:55`) | 1 → 0 | 1 → 0 |
| cloud-`USES` scope-wide | `RetractWorkloadCloudRelationshipEdgesCypher` via `WorkloadCloudRelationshipWriter.RetractWorkloadCloudRelationshipEdges` — the exact method `commitEdges` dispatches (`workload_cloud_relationship_materialization.go:248`) | 1 → 0 | 1 → 0, other scope's edge survives |

Ledger: `8517-retract-*-neo4j2026` rows in `docs/internal/measurements.jsonl`.

## Harness errors caught

- Re-seeding with `CREATE` without an intermediate cleanup accumulated edges
  across phases (DEFINES read 3, want 2). Fixed by cleaning between the bare
  and production phases; the committed probe cleans before every re-seed.
- The cloud-`USES` scope-isolation assertion initially counted the
  accumulated other-scope edge from the first seed round (2, want 1) for the
  same reason. Same fix; the committed probe asserts the sibling scope stays
  at exactly 1.

## No-Regression Evidence:

Spelling-only production change alongside this probe (bare
`Intent`/`Result` → `reducercontract`-qualified at 4 sites, mirroring open
#8568) resolves to identical types and identical compiled code. The probe
itself is a new `_live_test.go` file: skipped without a backend DSN, so
default unit runs are unaffected. Full pass above in 0.208 s.

## No-Observability-Change:

No metric, span, log key, or status surface added or altered. The stale edge
remains visible as the `ambiguous` outcome on
`eshu_dp_workload_handle_resolution_total` for that surface until the
emitting scope re-materializes.
