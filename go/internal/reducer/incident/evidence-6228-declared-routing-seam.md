# Evidence: incident-routing declared-evidence decode seam adds no hot-path cost (#6228)

Records the performance and observability evidence for the seam slice
(exported `DecodeIncidentRoutingDeclaredEvidence` plus `pagerduty.tf`
fixture and vacuity guard), so the hot-path evidence gate
(`scripts/verify-performance-evidence.sh`) has a tracked, in-repo record
specific to this change rather than relying on PR prose.

## Change shape

Mostly additive: new `routing_evidence_decode.go` (exported seam over the
unchanged builder), new guard `incident_routing_evidence.go`, new Odu,
fixture, and test files, plus one-line catalog/`Resolve` registrations.
One existing hot file is touched:
`incident_routing_evidence_loader.go` moves its per-row metadata mapping
into the seam (SQL untouched, `incidentRoutingPayloadBool` deleted with its
logic preserved in the seam). `doc.go` is comment-only. No query, batch
size, worker knob, lease, retry, or queue path is modified.

## Performance Evidence:

- **Baseline / after** (same host, AMD EPYC 9R14, Go go1.26.2,
  `-benchtime 100000x -count 3`, hermetic, no backend; base helpers
  copied verbatim from `e531aa471e`):

  | Benchmark | base ns/op | branch ns/op |
  |---|---|---|
  | Declared-evidence decode, full 11-field metadata | 143 | 152 |
  | Declared-evidence decode, sparse 2-field metadata | n/a (new) | 117 |

- 0 B/op, 0 allocs/op on every row, base and branch alike. The ~9 ns
  delta is call overhead plus run-to-run noise, not a shape change: no
  new allocation, no new statement per row, string fields byte-identical
  (`AnyToString` renders non-strings exactly as the old `fmt.Sprint`).
- Guard replay (offline test-only: HCL parse plus builder plus
  extractor, no backend): ~170 us/op, 67,753 B/op, 594 allocs/op
  (`-benchtime 20x -count=2`). It never runs in production.

## No-Regression Evidence:

The loader still scans the same columns under the same SQL and decodes
each row through the seam; the guard reproduces the hand-derived 3-edge
set exactly across all 3 registry types and 4 fact envelopes (tombstoned
applied fact yields no row), and RED-pins zero rows without Declared.
Backend/version: no Cypher text altered, no knob changed. Input shape:
11-field declared metadata per row. Focused suites green:
`go/internal/reducer/incident` and the `IncidentRouting` guard tests.

## No-Observability-Change:

No metric, span, log key, or status surface added or altered (verified:
the only `span`/`log` matches in the diff are prose words such as
"spanning" in comments). Nothing for an operator to watch beyond
existing loader telemetry.
