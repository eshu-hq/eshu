# MetricsReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-5 Metrics tier-b core pin, per the
arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side;
the satellite records this pin in a follow-up. A satellite PR
consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the metrics time-series leg.

## Shape

Version: `MetricsReadAdmissionContractVersion = "v0"`
(go/internal/query/metrics/read_admission.go).

Exactly 1 re-declared (never embedded or aliased) method, sealed:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `RangeQuery` | `TimeSeriesSource` | `GET /api/v0/metrics/timeseries` bounded range read |

Route oracle codes (oracle `8a78eb3f48`): `GET
/api/v0/metrics/timeseries` (`paths/status/metrics.go`: 200/400).

Transport: no constructor on this pin. The compile-time mirror
assertion (`var _ MetricsReadAdmission = TimeSeriesSource(nil)`)
keeps the re-declaration honest in one direction only: removal,
rename, or re-signing on the shared handle fails compilation here,
while shared-handle addition deliberately does not widen this pin.
`fakeMetricsSource` proves the shape servable in tests.
Concrete-double-only proof beyond the frozen set is rejected.

Enforcement (all in go/internal/query/metrics):

- `read_admission_contract_test.go` — frozen method-set test plus
  version pin
  (`TestMetricsReadAdmissionContractIsVersioned`), satisfier
  (`fakeMetricsSource` assertion) and seal
  (`TestMetricsReadAdmissionWireShapesAreSealed` — exactly 1 method),
  and the seeded-drift RED half
  (`TestMetricsReadAdmissionSeededDriftIsDetected` — the planted
  `seededMetricsDrift` widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the leg through the
existing time-series source, the same read the leg runs against
today. This is O6 pin 2, per the arbiter O6 ruling minting order. A
satellite PR consuming this pin for any other backend is invalid
without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/metrics/timeseries` is natively mounted in-tree and its
OpenAPI fragment stays core-side (oracle
`paths/status/metrics.go`, codes above) — disclosed here, not
silently dropped. No `contracts/upstream/*` stub covers this leg: the
wire-shape generator snapshots forwarded relay spellings only, and no
Metrics leg has one.

## Explicitly OUT — `POST /api/v0/code/call-graph/metrics` stays codeGraphWave-owned

`POST /api/v0/code/call-graph/metrics` is a `GraphQuery` reader
(`CodeHandler.CallGraphMetricsData` reads via `h.Neo4j.Run`), not a
`TimeSeriesSource` consumer: it stays outside this pin and keeps its
own per-route relay reason. A satellite PR citing this record as
metrics-leg coverage for that route is invalid.

Also OUT (each keeps its own standing until a deliberate pin covers
it):

- The grant check, the metric allow-list, and the fixed failure
  messages (handler behavior, not port shape).
- Every other `GraphQuery` consumer: the code-graph-metrics leg and
  every other graph reader keep their own per-route relay reasons
  until deliberate pins cover them.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
