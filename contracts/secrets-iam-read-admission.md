# SecretsIAMReadAdmission v0 (secrets-iam pin record)

Refs eshu-hq/eshu#9669. Wave-5 SecretsIAM tier-b core pin, per the arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the satellite records this pin in a follow-up. A satellite PR consuming an unminted pin is invalid. Arbiter review of this pin precedes satellite termination of the SecretsIAM legs.

## Shape

Version: `SecretsIAMReadAdmissionContractVersion = "v0"` (`go/internal/query/secrets/read_admission.go`).

Exactly 5 re-declared (never embedded) methods, sealed — no grant method, no write surface rides along:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `ListSecretsIAMIdentityTrustChains` | identity-trust-chain store | trust-chains leg |
| 2 | `ListSecretsIAMPrivilegePostureObservations` | privilege-posture-observation store | posture-observations leg |
| 3 | `ListSecretsIAMSecretAccessPaths` | secret-access-path store | access-paths leg |
| 4 | `ListSecretsIAMPostureGaps` | posture-gap store | posture-gaps leg |
| 5 | `SummarizeSecretsIAMPosture` | posture-summary store | posture-summary rollup |

Route oracle codes (oracle `8a78eb3f48`, all `paths/infrastructure/secrets_iam.go`): `GET /api/v0/secrets-iam/posture-summary` (200/400/403/500/501/503/504), `GET /api/v0/secrets-iam/identity-trust-chains` (200/400/403/500/501/503), `GET /api/v0/secrets-iam/privilege-posture-observations` (200/400/403/500/501/503), `GET /api/v0/secrets-iam/secret-access-paths` (200/400/403/500/501/503), `GET /api/v0/secrets-iam/posture-gaps` (200/400/403/500/501/503).

Transport: constructor `NewReadAdmissionWithReadStore(db.ReadStore)` builds the composition-only satisfier over a guarded read-only query port — the satellite consumes the pin through one handle without a direct table or queue handle. It delegates to the five existing `NewPostgresIAM*StoreWithReadStore` constructors deliberately instead of minting parallel wiring; the transport proof (fail-closed over a guarded `ReadStore` in the wire-shapes test) covers the satellite path end to end. The compile-time mirror assertion in the pin file proves the five stores satisfy the pin.

Enforcement (all in `go/internal/query/secrets/`):

- `read_admission_contract_test.go` — frozen method-set test plus version pin (`TestSecretsIAMReadAdmissionContractIsVersioned`), witness (`secretsIAMReadAdmissionWitness` + satisfier assertion) over the parameterized `MethodSet` (`secretsIAMReadAdmissionMethodSet`), and the seeded-drift RED half (`TestSecretsIAMReadAdmissionSeededDriftIsDetected` — a planted widening must not match the frozen set).
- `read_admission_wire_shapes_test.go` — seal (`TestSecretsIAMReadAdmissionWireShapesAreSealed` — exactly the 5-method frozen set) and satellite-consumability (`TestSecretsIAMReadAdmissionWireShapesAreSatelliteConsumable`).

## Satellite consumption

Consume the pin through `NewReadAdmissionWithReadStore` with the request-scoped `db.ReadStore`, the same guarded reader the API wiring passes today: it serves exactly the five pinned reads over nothing wider. A satellite PR consuming this pin for any other backend combination is invalid without arbiter review.

## W8a disclosed-absence posture

All five GETs are natively mounted in-tree and their OpenAPI fragment stays core-side (oracle `paths/infrastructure/secrets_iam.go`, codes above) — disclosed here, not silently dropped. No `contracts/upstream/*` stub covers these legs: the wire-shape generator snapshots forwarded relay spellings only, and no SecretsIAM leg has one.

## Explicitly OUT — the S3 grant-posture leg stays OUT

The S3 external-principal grant-posture leg (`GraphIAMGrantPostureStore.SummarizeS3ExternalPrincipalGrantPosture`, the canonical `GRANTS_ACCESS_TO` graph truth blended into the summary response) is not pinned here: there is no grant method in the frozen set, so a dest consumer cannot acquire graph reads through it. The handler keeps blending the graph section from its own GrantPosture handle. Also OUT: grant checks, fixed failure messages, and the capability profile (handler behavior, not port shape — no method set to freeze).

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends on it yet — so rollback moves no traffic and changes no data.
