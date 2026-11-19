# WorkItemEvidenceReadAdmission v0 (W8b pin record)

Refs eshu-hq/eshu#9669. Wave-5 WorkItems tier-b core pin, per the arbiter
step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted pin is
invalid. Arbiter review of this pin precedes satellite termination of the
work-item legs.

## Shape

Version: `WorkItemEvidenceReadAdmissionContractVersion = "v0"`
(go/internal/query/workitem/evidence_read_admission.go).

Exactly 1 read, re-declared in this work-item-scoped shape instead of
embedding `EvidenceStore` — no drift, IaC, TerraformDrift, CodeGraph,
CodeRelationships, SemanticSearch, or SupplyChain surface rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `ListWorkItemEvidence` | `EvidenceStore` | `GET /api/v0/work-items/evidence` |

Route oracle codes (oracle `8a78eb3f48`): `GET
/api/v0/work-items/evidence` (`paths/evidence/work_item.go`:
200/400/403/500/501/503).

Transport: no constructor on this pin — the compile-time mirror assertion
in the pin file proves the work-item-scoped re-declaration matches the
staying handler-facing port, and the satisfier assertion proves the
production Postgres store serves the pinned read with the minimal shape, no
wider store surface. Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query/workitem):

- `evidence_read_admission_contract_test.go` — frozen method-set test plus
  version pin
  (`TestWorkItemEvidenceReadAdmissionContractIsVersioned`), seal
  (`TestWorkItemEvidenceReadAdmissionWireShapesAreSealed` — exactly 1
  read), and the seeded-drift RED half
  (`TestWorkItemEvidenceReadAdmissionSeededDriftIsDetected` — a planted
  widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the work-item evidence leg
through the pin, backed by the same Postgres evidence store the leg runs
against today. A satellite PR consuming this pin for any other family is
invalid without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/work-items/evidence` is natively mounted in-tree and its
OpenAPI fragment stays core-side (oracle `paths/evidence/work_item.go`,
codes above) — disclosed here, not silently dropped. No
`contracts/upstream/*` stub covers this leg: the wire-shape generator
snapshots forwarded relay spellings only, and no WorkItems leg has one.

## Explicitly OUT

- The scoped-token grant predicate and the scoped empty-grant
  short-circuit are handler behavior, not port shape: there is no method
  set to freeze.
- The grant checks, the fixed failure messages, and `QueryProfile` are
  handler behavior, not port shape.
- No constructor or `ReadStore` surface: the satellite satisfies this port
  with its own wiring; this pin carries the read shape only.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends
on it yet — so rollback moves no traffic and changes no data.
