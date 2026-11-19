# ObservabilityCoverageCorrelationReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-5 ObservabilityCoverage tier-b core pin,
per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted
core-side; the satellite records this pin in a follow-up. A satellite
PR consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the observability coverage leg.

## Shape

Version:
`ObservabilityCoverageCorrelationReadAdmissionContractVersion = "v0"`
(go/internal/query/observability/coverage/correlation_read_admission.go).

Exactly 1 re-declared (never embedded) method, sealed — the handler's
Content field is never read on this leg, so the pin carries no
Content method:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `ListObservabilityCoverageCorrelations` | `ObservabilityCorrelationStore` | `GET /api/v0/observability/coverage/correlations` bounded correlation read |

Route oracle codes (oracle `8a78eb3f48`): `GET
/api/v0/observability/coverage/correlations`
(`paths/infrastructure/observability_coverage.go`:
200/400/403/500/501/503).

Transport: no constructor or `ReadStore` surface on this pin — the
satellite satisfies it with its own wiring. The compile-time mirror
assertion keeps the re-declaration honest in one direction, and the
satisfier assertion (the concrete `PostgresCorrelationStore` built by
the former in-tree wiring) proves the pinned read serves with the
minimal shape. Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query/observability/coverage):

- `correlation_read_admission_contract_test.go` — frozen method-set
  test plus version pin
  (`TestObservabilityCoverageCorrelationReadAdmissionContractIsVersioned`),
  seal
  (`TestObservabilityCoverageCorrelationReadAdmissionWireShapesAreSealed`
  — exactly 1 method), and the seeded-drift RED half
  (`TestObservabilityCoverageCorrelationReadAdmissionSeededDriftIsDetected`
  — the planted `seededObservabilityCoverageCorrelationDrift`
  widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the leg through the
existing Postgres correlation store, the same read the leg runs
against today. This is the W8b-corr leaf, per the arbiter
SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. A satellite PR
consuming this pin for any other backend is invalid without arbiter
review.

## W8a disclosed-absence posture

`GET /api/v0/observability/coverage/correlations` is natively mounted
in-tree and its OpenAPI fragment stays core-side (oracle
`paths/infrastructure/observability_coverage.go`, codes above) —
disclosed here, not silently dropped. No `contracts/upstream/*` stub
covers this leg: the wire-shape generator snapshots forwarded relay
spellings only, and no ObservabilityCoverage leg has one.

## Explicitly OUT

Also OUT (each keeps its own standing until a deliberate pin covers
it):

- The grant checks, the fixed failure messages, and `QueryProfile`
  (handler behavior, not port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
