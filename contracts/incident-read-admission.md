# IncidentContextReadAdmission v0 (W8b pin record)

Refs eshu-hq/eshu#9669. Wave-5 Incident tier-b core pin, per the arbiter
step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted pin is
invalid. Arbiter review of this pin precedes satellite termination of the
incident legs.

## Shape

Version: `IncidentContextReadAdmissionContractVersion = "v0"`
(go/internal/query/incident/context_read_admission.go).

Exactly 2 reads, re-declared in this incident-scoped shape instead of
embedding the model ports — no drift, IaC, TerraformDrift, CodeGraph,
CodeRelationships, SemanticSearch, or SupplyChain surface rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `ReadIncidentContext` | `model.IncidentContextStore` | `GET /api/v0/incidents/{incident_id}/context` (context read) |
| 2 | `ResolveDurableIncidentRepositories` | `model.IncidentRepositoryAuthorizer` | `GET /api/v0/incidents/{incident_id}/context` (scoped-authorizer leg) |

The authorizer is IN, not OUT: scoped callers hit the authorizer before the
store read (scope.go) while unscoped callers skip it, so the pin freezes
both — a pin over the context read alone would leave the fail-closed gate
unpinned.

Route oracle codes (oracle `8a78eb3f48`): `GET
/api/v0/incidents/{incident_id}/context`
(`paths/evidence/incident_context.go`: 200/400/403/404/409/503).

Transport: no constructor on this pin — the compile-time mirror assertion
in the pin file proves the incident-scoped re-declaration matches both
staying handler-facing sources, and the production-satisfier assertions
live in the contract test (package ownership rule: this file does not
import the store concretes). Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query/incident):

- `context_read_admission_contract_test.go` — frozen method-set test plus
  version pin (`TestIncidentContextReadAdmissionContractIsVersioned`),
  seal (`TestIncidentContextReadAdmissionWireShapesAreSealed` — exactly 2
  reads), serve-the-route through the canned double
  (`TestIncidentContextReadAdmissionWireShapesServeTheRoute`, backed by
  `incidentContextAdmissionCannedStore`), and the seeded-drift RED half
  (`TestIncidentContextReadAdmissionSeededDriftIsDetected` — a planted
  widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the incident context leg
through the pin, authorizer first for scoped callers, backed by the same
Postgres incident store and repository authorizer the leg runs against
today. A satellite PR consuming this pin for any other family is invalid
without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/incidents/{incident_id}/context` is natively mounted in-tree
and its OpenAPI fragment stays core-side (oracle
`paths/evidence/incident_context.go`, codes above) — disclosed here, not
silently dropped. No `contracts/upstream/*` stub covers this leg: the
wire-shape generator snapshots forwarded relay spellings only, and no
Incident leg has one.

## Explicitly OUT

- The identical-envelope denial is handler behavior, not port shape: there
  is no method set to freeze.
- The grant checks, the fixed failure messages, and `QueryProfile` are
  handler behavior, not port shape.
- No constructor or `ReadStore` surface: the satellite satisfies this port
  with its own wiring; this pin carries the read shape only.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends
on it yet — so rollback moves no traffic and changes no data.
