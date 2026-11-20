# CodeHardcodedSecretReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-4 Code tier-b core pin, per the arbiter
step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite
records this pin in a follow-up. A satellite PR consuming an unminted
pin is invalid. Arbiter review of this pin precedes satellite
termination of the secrets-investigate leg.

## Shape

Version: `CodeHardcodedSecretReadAdmissionContractVersion = "v0"`
(go/internal/query/codequery/code_hardcoded_secret_read_admission.go).

Exactly 2 re-declared (never embedded) methods, sealed — no graph
traversal, search, list, file, topic, inventory, ledger, or snapshot
surface rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `InvestigateHardcodedSecrets` | staying handler port | `POST /api/v0/code/security/secrets/investigate` finding rows |
| 2 | `InvestigateHardcodedSecretsWithSource` | staying handler port | finding rows plus the read-source tag |

Route oracle codes (oracle `8a78eb3f48`,
`paths/code/security.go`): `POST
/api/v0/code/security/secrets/investigate` (200/400/403/500/503/504).

Transport: no `WithReadStore` constructor on this pin — the secrets
seam admits only the investigator's two reads, and minting a store
constructor would pick a wiring the satellite never runs. The
compile-time mirror assertion in the pin file proves the shape the leg
runs against today satisfies the pin.

Evidence (not enforcement): the wire-shapes proof through the real
`Mount`
(`code_hardcoded_secret_read_admission_wire_shapes_test.go` —
`TestCodeHardcodedSecretReadAdmissionWireShapesAreSatelliteConsumable`).

Enforcement (all in go/internal/query/codequery):

- `code_hardcoded_secret_read_admission_contract_test.go` — frozen
  method-set test plus version pin
  (`TestCodeHardcodedSecretReadAdmissionContractIsVersioned`), witness
  (`codeHardcodedSecretReadAdmissionWitness` + satisfier assertion)
  and its re-signed twin
  (`TestCodeHardcodedSecretReadAdmissionRejectsResignedMethod`), seal
  (`TestCodeHardcodedSecretReadAdmissionWireShapesAreSealed` —
  exactly 2 methods, no graph/search/list/file/topic/inventory/ledger
  surface), and the seeded-drift RED half
  (`TestCodeHardcodedSecretReadAdmissionSeededDriftIsDetected` — the
  planted `Run` widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the leg through the
existing investigator shape, the same one the leg runs against today. A
satellite PR consuming this pin for any other backend shape is invalid
without arbiter review.

## W8a disclosed-absence posture

`POST /api/v0/code/security/secrets/investigate` is natively mounted
in-tree (`CodeHandler.Mount`,
go/internal/query/codequery/handler.go) and its OpenAPI fragment stays
core-side (oracle `paths/code/security.go`,
200/400/403/500/503/504) — disclosed here, not silently dropped. No
`contracts/upstream/*` stub covers this leg: the wire-shape generator
snapshots forwarded relay spellings only, and no secrets leg has one.

## Explicitly OUT (not widened by this pin)

- `Run`/`RunSingle` graph reads — the secrets lane reaches the graph
  only through its own investigator, never as a pinned read. Proven at
  the seal, not just in the frozen set.
- Topics investigate and structure inventory — the content
  investigation pin covers them.
- Divergence investigate/report — unpinned legs with their own
  standing; a satellite PR must not cite this pin as their coverage.
- Grant checks, fixed failure messages, and `QueryProfile` (handler
  behavior, not port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
