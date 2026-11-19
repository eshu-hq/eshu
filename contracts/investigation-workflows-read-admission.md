# InvestigationWorkflowReadAdmission v0 (W5 pin record)

Refs eshu-hq/eshu#9669. Wave-5 InvestigationWorkflows tier-b core pin, per
the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the
satellite records this pin in a follow-up. A satellite PR consuming an
unminted pin is invalid. Arbiter review of this pin precedes satellite
termination of the investigation-workflow legs.

## Shape

Version: `InvestigationWorkflowReadAdmissionContractVersion = "v0"`
(go/internal/query/investigation_workflow_read_admission.go).

Exactly 3 reads, re-declaring the staying static signatures whole in this
workflow-scoped shape instead of aliasing the catalog surface — no other
family can cite this pin:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `ListInvestigationWorkflows` | static `InvestigationWorkflowCatalog` | `GET /api/v0/investigation-workflows` (catalog list) |
| 2 | `LookupInvestigationWorkflow` | static `LookupInvestigationWorkflow` | `GET /api/v0/investigation-workflows`, `POST /api/v0/investigation-workflows/resolve` (shared lookup) |
| 3 | `ResolveInvestigationWorkflow` | `w.Resolve` | `POST /api/v0/investigation-workflows/resolve` (resolve) |

Route oracle codes (oracle `8a78eb3f48`,
`paths/evidence/investigation_workflows.go`): `GET
/api/v0/investigation-workflows` (200/400), `POST
/api/v0/investigation-workflows/resolve` (200/400/404).

Transport: no constructor on this pin — the compile-time adapter assertion
in the pin file proves the minimal static satisfier delegates each
re-declared read to the static catalog function the handler legs call
today. Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query):

- `investigation_workflow_read_admission_contract_test.go` — frozen
  method-set test plus version pin
  (`TestInvestigationWorkflowReadAdmissionContractIsVersioned`), witness
  (`investigationWorkflowReadAdmissionWitness` plus satisfier assertion),
  seal (`TestInvestigationWorkflowReadAdmissionWireShapesAreSealed` —
  exactly 3 reads), and the seeded-drift RED half
  (`TestInvestigationWorkflowReadAdmissionSeededDriftIsDetected` — a
  planted widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve both investigation-workflow
legs through the pin, backed by the same static catalog the legs run
against today. A satellite PR consuming this pin for any other family is
invalid without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/investigation-workflows` and `POST
/api/v0/investigation-workflows/resolve` are natively mounted in-tree and
their OpenAPI fragments stay core-side (oracle
`paths/evidence/investigation_workflows.go`, codes above) — disclosed here,
not silently dropped. No `contracts/upstream/*` stub covers these legs: the
wire-shape generator snapshots forwarded relay spellings only, and no
InvestigationWorkflows leg has one.

## Explicitly OUT

- The catalog versions list (`InvestigationWorkflowCatalogVersions`) is a
  schema marker, not a route read: no leg calls it, so there is no method
  set to freeze for it.
- The limit/offset/view parsing, the 400/404 mappings, and the truth
  envelope are handler behavior, not port shape: there is no method set to
  freeze for them.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends
on it yet — so rollback moves no traffic and changes no data.
