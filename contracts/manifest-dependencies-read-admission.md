# ManifestDependenciesReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-5 ManifestDependencies tier-b core pin,
per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted
core-side; the satellite records this pin in a follow-up. A satellite
PR consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the manifest-dependencies leg.

## Shape

Version: `ManifestDependenciesReadAdmissionContractVersion = "v0"`
(go/internal/query/manifest/read_admission.go).

Exactly 1 re-declared (never embedded) method, sealed — the bounded
page read, lookahead bound and refused signal included:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `ListRepositoryManifestDependencyFacts` | `ManifestDependencyStore` | `GET /api/v0/repository/manifest-dependencies` bounded page read |

Route oracle codes (oracle `8a78eb3f48`): `GET
/api/v0/repository/manifest-dependencies`
(`paths/repository/manifest_dependencies.go`:
200/400/403/501/503/504).

Transport: no constructor, selector, or wider store surface on this
pin — the satellite satisfies it with its own wiring. The
compile-time mirror assertion keeps the re-declaration honest in one
direction, and the satisfier assertion (`*PostgresManifestDependencyStore`)
proves the pinned read serves with the minimal shape. Unlike its
sibling pins this seam is already handler-wired: `Handler.Store` is
the pin type, never the narrow `ManifestDependencyStore` directly.
Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query/manifest):

- `read_admission_contract_test.go` — frozen method-set test plus
  version pin
  (`TestManifestDependenciesReadAdmissionContractIsVersioned`), seal
  (`TestManifestDependenciesReadAdmissionWireShapesAreSealed` —
  exactly 1 method), serve-the-route
  (`TestManifestDependenciesReadAdmissionWireShapesServeTheRoute` —
  the pin-typed canned store, including the refused delta-base signal
  the list route serves with no wider surface), and the seeded-drift
  RED half
  (`TestManifestDependenciesReadAdmissionSeededDriftIsDetected`).
- `read_admission_wire_test.go` — nil-store 503
  (`TestListManifestDependenciesNilStoreReturns503` — the fixed 503
  names the missing fact store instead of panicking).

## Satellite consumption

Depend on the shape, not a constructor: serve the leg through the
existing manifest store, the same read the leg runs against today.
This is the G2 satellite leaf terminating this family's #9669
MUST-TERMINATE rows. A satellite PR consuming this pin for any other
backend is invalid without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/repository/manifest-dependencies` is natively mounted
in-tree and its OpenAPI fragment stays core-side (oracle
`paths/repository/manifest_dependencies.go`, codes above) —
disclosed here, not silently dropped. No `contracts/upstream/*` stub
covers this leg: the wire-shape generator snapshots forwarded relay
spellings only, and no ManifestDependencies leg has one.

## Explicitly OUT

Also OUT (each keeps its own standing until a deliberate pin covers
it):

- `LoadContextSection` stays unpinned by construction: the bounded
  repository-context section consumes the same store but is a
  different read the list route never issues.
- The row mapping, ecosystem and scope normalization, and freshness
  rendering (store construction, not port shape).
- The grant checks, the fixed failure messages, the cursor and limit
  bounds, the include-dev default, and `QueryProfile` (handler
  behavior, not port shape).

## Rollback

Revert this pin plus the handler `Store` field swap. The manifest
leg admits through the pin-typed `Store` field — rollback restores
the narrow `ManifestDependencyStore` type and moves no traffic beyond
the handler seam.
