# TerraformDriftReadAdmission v0 (O6 pin record)

Refs eshu-hq/eshu#9669. Wave-5 TerraformConfigStateDrift tier-b core pin,
per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side;
the satellite records this pin in a follow-up. A satellite PR consuming an
unminted pin is invalid. Arbiter review of this pin precedes satellite
termination of the config-state-drift findings legs.

## Shape

Version: `TerraformDriftReadAdmissionContractVersion = "v0"`
(go/internal/query/terraform/drift/read_admission.go).

Exactly 2 reads, re-declared in this drift-scoped shape instead of
embedding or aliasing `FindingStore` — every other `FindingStore`-shaped
consumer keeps its own per-route standing:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `ListActiveFindings` | `FindingStore` (with its `FindingFilter` double guard: scoped reads intersect `AllowedScopeIDs`, empty scope returns zero rows without querying) | `POST /api/v0/terraform/config-state-drift/findings` (active-finding list) |
| 2 | `CountActiveFindings` | `FindingStore` (same double guard) | `POST /api/v0/terraform/config-state-drift/findings` (active-finding count) |

Route oracle codes (oracle `8a78eb3f48`): `POST
/api/v0/terraform/config-state-drift/findings`
(`paths/iac/terraform_config_state_drift.go`: 200/403/501/503).

Transport: no constructor on this pin — the compile-time mirror assertion
in the pin file proves the drift-scoped re-declaration matches
`FindingStore`, one direction only: a store addition never widens this pin.
`POST /api/v0/cloud/runtime-drift/findings` terminates on the v0 lane via
the mcp37#2 same-handler precedent until this pin lands, then follows it.
Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query/terraform/drift):

- `read_admission_contract_test.go` — frozen method-set test plus version
  pin (`TestTerraformDriftReadAdmissionContractIsVersioned`, satisfier
  witness), seal
  (`TestTerraformDriftReadAdmissionWireShapesAreSealed` — exactly 2
  reads), the seeded PROOF positive
  (`TestTerraformDriftReadAdmissionSeededProof`), and the seeded-drift RED
  half (`TestTerraformDriftReadAdmissionSeededDriftIsDetected` — a planted
  widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the config-state-drift
findings leg through the pin, backed by the same finding store the leg runs
against today, composed by the consumer. A satellite PR consuming this pin
for any other `FindingStore` family is invalid without arbiter review.

## W8a disclosed-absence posture

`POST /api/v0/terraform/config-state-drift/findings` is natively mounted
in-tree and its OpenAPI fragment stays core-side (oracle
`paths/iac/terraform_config_state_drift.go`, codes above) — disclosed here,
not silently dropped. No `contracts/upstream/*` stub covers this leg: the
wire-shape generator snapshots forwarded relay spellings only, and no
TerraformConfigStateDrift leg has one.

## Explicitly OUT

- Profile and capability gating are handler behavior, not port shape: there
  is no method set to freeze. Capability-gating parity is owed at
  termination time, alongside scoped-grant binding, pagination, and
  truth-label parity.
- The grant check and the fixed failure messages are handler behavior, not
  port shape: the same standing as `QueryProfile` in the Compare handler
  and `Clock`/`FinalizeTimeout` in the mutation pins.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends
on it yet — so rollback moves no traffic and changes no data.
