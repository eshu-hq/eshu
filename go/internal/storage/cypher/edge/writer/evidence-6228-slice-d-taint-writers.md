# Evidence: slice-D taint-family coverage adds no hot-path cost (#6228)

Records the performance and observability evidence for the slice-D offline
coverage commit (code_taint_evidence emitting HAS_TAINT_EVIDENCE,
observability_coverage emitting AWS_COVERS_*, code_interproc_evidence
emitting TAINT_FLOWS_TO), so the hot-path evidence gate
(`scripts/verify-performance-evidence.sh`) has a tracked, in-repo record
specific to this change rather than relying on PR prose.

## Change shape

Additive only: three new family Odu files under
`go/internal/ifa/familyodu/` (`code_interproc_evidence_family_odu.go`,
`code_taint_evidence_family_odu.go`,
`observability_coverage_family_odu.go`), three new vacuity-guard files
under `go/internal/ifa/materializededges/` (`code_interproc_evidence.go`,
`code_taint_evidence.go`, `observability_coverage.go`) plus a three-case
`Resolve` registration in `materialized_edges.go`, exported alias consts
beside the existing writer in
`go/internal/storage/cypher/code_interproc_evidence_writer.go`, one new
registry row (`code_interproc_evidence`) in
`go/internal/storage/cypher/edge/materialized/single_type_families.go`,
hand-derived expected-edge fixtures, and Odu/guard/fixture test code.
No existing writer, query, batch size, worker knob, lease, retry, or queue
path is modified. The `code_taint_evidence` and `observability_coverage`
families land Odu, guard, and fixture coverage only in this slice: they
have no production writer and no registry row yet, so they contribute no
runtime path at all. The new family guards reproduce bounded exact sets
per fixture (2-edge HAS_TAINT_EVIDENCE, 5-edge AWS_COVERS_* across all
five vocabulary members and arn/bare_id modes, 2-edge TAINT_FLOWS_TO
with the evidence_uid identity pin); the `code_interproc_evidence`
registry row holds the production consts themselves, never a copy of
their Cypher text.

## Performance Evidence:

- **Baseline / after** (same host, AMD EPYC 9R14, Go go1.26.2,
  `-benchtime 100x -count=1`,
  `go/internal/storage/cypher/edge/writer`, hermetic Go test, no backend):
  base is `76cebf18ef` (slice-C merge), branch is `355358027c`.

  | Benchmark | base ns/op | branch ns/op |
  |---|---|---|
  | CodeCall retract+write, full refresh 5000 rows | 7,991,648 | 8,130,686 |
  | CodeCall retract+write, delta 50 files | 8,093,649 | 7,841,098 |
  | DeployableUnit correlation writer | 6,337,961 | 6,451,999 |
  | RepoDependency write | 6,844,601 | 6,853,524 |
  | Unroutable row loop, all routable | 3,860,337 | 3,752,117 |
  | Unroutable row loop, all unroutable | 1,702,284 | 1,512,880 |

- Bytes/op and allocs/op match between base and branch on every row
  (e.g. 80,194 vs 80,195 allocs on the refresh row, B/op within 0.003%).
  Time deltas (worst row +1.8% on a writer row at 100 iterations, other
  rows flat or faster) are run-to-run noise, not a shape change: no new
  allocation, no new statement per row.

## No-Regression Evidence:

The change adds no new production write path beyond one static registry
row that reuses the existing writer consts; no existing hot path changed
shape, so no existing budget holder can regress. Backend/version: no
Cypher text of an existing writer altered, no batch size or worker knob
changed. Input shape: fixture-bounded 2-edge/5-edge/2-edge sets replayed
offline. Live proof (Neo4j projection of these families at corpus scale)
belongs to the CI live matrices at landing; waivers stand with
`LiveProven` unset.

## No-Observability-Change:

No metric, span, log key, or status surface added or altered (verified: no
otel/prometheus/telemetry/log/span additions in the diff). Nothing for an
operator to watch beyond existing writer telemetry.
