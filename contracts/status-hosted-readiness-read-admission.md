# StatusHostedReadinessReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669, Refs eshu-hq/eshu#10491. Status Wave-2 core pin,
per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted
core-side; the satellite records this pin in a follow-up. A satellite
PR consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the hosted-readiness graph legs.

## Shape

Version: `StatusHostedReadinessReadAdmissionContractVersion = "v0"`
(go/internal/query/status_hosted_readiness_read_admission.go).

Exactly 2 re-declared (never embedded) methods, sealed — no session,
no transaction, no write, no snapshot surface:

| # | Method | Mirrors (staying shared port) | Serves |
|---|--------|-------------------------------|--------|
| 1 | `Run(context.Context, string, map[string]any) ([]map[string]any, error)` | `querycontract.GraphQuery` (workload-leftovers detection leg via `legacyworkload.Detect`) | `GET /api/v0/status/hosted-readiness` (oracle `paths/status/hosted_readiness.go`: 200 + `text/plain`, 403/500/503) |
| 2 | `RunSingle(context.Context, string, map[string]any) (map[string]any, error)` | `querycontract.GraphQuery` (repository-count leg over `MATCH (r:Repository) RETURN count(r) as count`) | `GET /api/v0/status/hosted-readiness` (same route and codes) |

Transport: the satellite binds any `querycontract.GraphQuery` reader
through the pin (`bindStatusHostedReadinessPin` in the wire-shapes
test) — no constructor, no parallel wiring. The guarded-graph
fail-closed proof (stub graph plus production `Neo4jReader` with no
driver wired, probe-error isolation both ways) covers the satellite
path end to end. Concrete-double-only proof is rejected.

Evidence (not enforcement): the production `Neo4jReader` satisfies the
pin (`var _ StatusHostedReadinessReadAdmission = (*Neo4jReader)(nil)`),
so the pin is satisfiable with the minimal shape, no wider graph
surface.

Enforcement (all in go/internal/query):

- `status_hosted_readiness_read_admission_contract_test.go` — frozen
  method-set test plus version pin
  (`TestStatusHostedReadinessReadAdmissionContractIsVersioned`),
  witness (`statusHostedReadinessReadAdmissionWitness` + satisfier
  assertion), seeded-drift RED proof
  (`TestStatusHostedReadinessReadAdmissionSeededDriftIsDetected` — a
  planted `Exec` widening must not match the frozen set).
- `status_hosted_readiness_read_admission_contract_test.go` — seal
  (`TestStatusHostedReadinessReadAdmissionWireShapesAreSealed`) —
  exactly 2 methods, no Session/Transaction/Write/Snapshot/Exec/
  Summary/List surface.
- `status_hosted_readiness_read_admission_wire_shapes_test.go` —
  wire-shapes proof over nothing wider than the guarded graph handle
  (fail-closed both legs, production-reader drift legs with
  probe-isolation)
  (`TestStatusHostedReadinessReadAdmissionWireShapesAreSatelliteConsumable`).
- Route proof through the real handler:
  `TestStatusHandlerHostedReadinessReportsHealthyConvergence`,
  `TestStatusHandlerHostedReadinessReportsDeadLettersAndGraphUnavailable`,
  `TestStatusHandlerHostedReadinessTextSummary`.
- Seal (`TestStatusHostedReadinessReadAdmissionWireShapesAreSealed`).
- Compile-time mirror assertion in the pin file:
  `var _ StatusHostedReadinessReadAdmission = querycontract.GraphQuery(nil)`
  — names the shared port only as the shape this pin mirrors; no other
  family may cite this pin as coverage. One-directional on purpose.

## Satellite consumption

Bind any request-scoped `querycontract.GraphQuery` through the pin;
the two re-declared reads are the whole surface the legs admit
through.

## W8a disclosed-absence posture

`GET /api/v0/status/hosted-readiness` is natively mounted in-tree
(`StatusHandler.Mount`, go/internal/query/status.go) and its OpenAPI
fragment stays core-side (oracle `paths/status/hosted_readiness.go`,
200 + `text/plain`, 403/500/503) — disclosed here, not silently
dropped. No `contracts/upstream/*` stub covers these legs: the
wire-shape generator snapshots forwarded relay spellings only, and
these legs have no forwarded spelling. The mount + core-side fragment
+ spec-key list above is the complete termination record for the
hosted-readiness graph legs.

## Explicitly OUT (not widened by this pin)

- Snapshot leg on the same route (`status.Reader`) — covered by
  `StatusReadAdmission` v0 (see
  contracts/status-snapshot-read-admission.md), not widened here.
- `GraphBackend` selector (config, not a port) and the `Detect`
  algorithm (callee behavior: candidate paging, examined caps, twin
  classification) — the pin freezes only the Run/RunSingle transport.
- `NarrationPosture` func field (handler field, not a port).
- Suppression legs — none exists on this route; the suppressions
  mutation row stays the Decision 2 survivor, never a read here.
- Not combined with the governance Summary pin: each admits only its
  own legs.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
