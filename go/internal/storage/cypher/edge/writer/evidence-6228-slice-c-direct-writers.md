# Evidence: slice-C direct-family writers add no hot-path cost (#6228)

Records the performance and observability evidence for the slice-C offline
coverage commit (built_from, derived_from, publishes,
cloud_resource_container_image, crossplane_satisfied_by), so the hot-path
evidence gate (`scripts/verify-performance-evidence.sh`) has a tracked,
in-repo record specific to this change rather than relying on PR prose.

## Change shape

Additive only: new edge writers under
`go/internal/storage/cypher/edge/writer/` (derived_from, provenance) and
`go/internal/storage/cypher/cloud/aws/image/`, new registry rows in
`direct_families.go`, plus Odu/guard/fixture test code. No existing writer,
query, batch size, worker knob, lease, retry, or queue path is modified. New
writers emit bounded exact sets per fixture (2-edge and 3-edge sets listed in
the PR body); the twin-template publishes path reuses one MERGE identity
instead of duplicating writes.

## Performance Evidence:

- **Baseline / after** (same host, AMD EPYC 9R14, `-benchtime 100x -count=1`,
  `go/internal/storage/cypher/edge/writer`, hermetic Go test, no backend):

  | Benchmark | base ns/op | branch ns/op |
  |---|---|---|
  | CodeCall retract+write, full refresh 5000 rows | 7,773,118 | 7,909,007 |
  | CodeCall retract+write, delta 50 files | 7,569,572 | 7,648,878 |
  | DeployableUnit correlation writer | 6,249,799 | 6,630,515 |
  | RepoDependency write | 6,816,137 | 6,831,493 |
  | Unroutable row loop, all routable | 3,742,408 | 3,788,725 |
  | Unroutable row loop, all unroutable | 1,593,167 | 1,537,232 |

- Bytes/op and allocs/op match between base and branch on every row
  (e.g. 80,195 vs 80,196 allocs on the refresh row). Time deltas (±6% worst
  row at 100 iterations) are run-to-run noise, not a shape change: no new
  allocation, no new statement per row.

## No-Regression Evidence:

The change adds new write paths; no existing hot path changed shape, so no
existing budget holder can regress. Backend/version: no Cypher text of an
existing writer altered, no batch size or worker knob changed. Input shape:
fixture-bounded 2-edge/3-edge sets. Live proof (Neo4j projection of the new
writers at corpus scale) belongs to the CI live matrices at landing; waivers
stand with `LiveProven` false.

## No-Observability-Change:

No metric, span, log key, or status surface added or altered (verified: no
otel/prometheus/telemetry/log/span additions in the diff). Nothing for an
operator to watch beyond existing writer telemetry.
