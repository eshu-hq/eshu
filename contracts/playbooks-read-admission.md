# PlaybookReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-5 Playbooks tier-b core pin, per the
arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side;
the satellite records this pin in a follow-up. A satellite PR
consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the playbook legs.

## Shape

Version: `PlaybookReadAdmissionContractVersion = "v0"`
(go/internal/query/playbook/read_admission.go).

Exactly 3 re-declared (never aliased) reads, sealed:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `ListPlaybookDefinitions` | static `Catalog()` | `GET /api/v0/query-playbooks` catalog list |
| 2 | `LookupPlaybook` | static `Lookup(id)` | `POST /api/v0/query-playbooks/resolve` playbook lookup |
| 3 | `ResolvePlaybook` | `pb.Resolve(inputs)` | same resolve leg, playbook resolve |

Route oracle codes (oracle `8a78eb3f48`): `GET
/api/v0/query-playbooks` (`paths/catalog/playbooks.go`: 200/400),
`POST /api/v0/query-playbooks/resolve`
(`paths/catalog/playbooks.go`: 200/400/404).

Transport: no constructor on this pin. The `playbookStaticCatalog`
adapter delegates each re-declared read to the static catalog
function the handler legs call today, so removal, rename, or
re-signing of any of the three fails compilation here.
Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query/playbook):

- `read_admission_contract_test.go` — frozen method-set test plus
  version pin (`TestPlaybookReadAdmissionContractIsVersioned`),
  witness (`playbookReadAdmissionWitness` satisfier assertion), seal
  (`TestPlaybookReadAdmissionWireShapesAreSealed` — exactly the 3
  re-declared catalog reads), and the seeded-drift RED half
  (`TestPlaybookReadAdmissionSeededDriftIsDetected` — the planted
  `seededPlaybookDrift` widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the two legs through
the existing static catalog, the same reads the legs run against
today. This is the W5 playbooks family (both oracle Playbooks rows).
A satellite PR consuming this pin for any wider catalog surface is
invalid without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/query-playbooks` and `POST
/api/v0/query-playbooks/resolve` are natively mounted in-tree and
their OpenAPI fragment stays core-side (oracle
`paths/catalog/playbooks.go`, codes above) — disclosed here, not
silently dropped. No `contracts/upstream/*` stub covers these legs:
the wire-shape generator snapshots forwarded relay spellings only,
and no Playbooks leg has one.

## Explicitly OUT

Also OUT (each keeps its own standing until a deliberate pin covers
it):

- The catalog versions list (`CatalogVersions`) is a schema marker,
  not a route read: no leg calls it, so there is no method set to
  freeze for it.
- The limit/offset/view parsing, the 400/404 mappings, and the truth
  envelope (handler behavior, not port shape).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
