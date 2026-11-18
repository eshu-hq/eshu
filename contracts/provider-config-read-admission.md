# ProviderConfigReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. ProviderConfig Wave-3 core pin record, per
the arbiter step-2 scope ruling (decisions 2, 6, 7). The pin itself is
the #8872 admission boundary (`ProviderConfigReadAdmission` v0 in
go/internal/query/admin/provider/config/admission.go, aliasing the
family `ReadStore` so the boundary is identity-verified by the frozen
contract test); this record mints the missing tier-b contracts side
core-side. The satellite records this pin in a follow-up. A satellite
PR consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the legs below.

## Shape

Version: `ProviderConfigReadAdmissionContractVersion = "v0"`
(go/internal/query/admin/provider/config/admission.go).

Exactly the 3 admin reads over redacted non-secret configuration,
sealed — no mutation, no connection-test, no secret surface:

| # | Method | Family port | Serves |
|---|--------|-------------|--------|
| 1 | `ListProviderConfigDetails(context.Context, string) ([]Detail, error)` | `ReadStore` (go/internal/query/admin/provider/config/types.go) | `GET /api/v0/auth/admin/provider-configs` (oracle `paths/auth/admin_provider_configs.go` get block: 200/403/500/503) |
| 2 | `GetProviderConfigDetail(context.Context, string, string) (Detail, bool, error)` | `ReadStore` | `GET /api/v0/auth/admin/provider-configs/{provider_config_id}` (same fragment get block: 200/400/403/404/500/503) |
| 3 | `ListProviderConfigRevisions(context.Context, string, string) ([]RevisionItem, error)` | `ReadStore` | `GET /api/v0/auth/admin/provider-configs/{provider_config_id}/revisions` (same fragment get block: 200/400/403/500/503) |

No method ever returns a secret: `has_secret`, `secret_fingerprint`,
and `key_id` are the only secret-adjacent fields (see
`leakage_test.go`).

Transport: the read legs admit through the family `ReadStore` the
`config.ReadHandler` already depends on — re-reading stored non-secret
configuration to back the mutation enable-time login-readiness guard
(issue #5604). No parallel wiring.

Evidence (not enforcement): the pin-satisfying witness
(`providerConfigReadAdmissionWitness` in
`read_admission_contract_test.go`) implements exactly the three reads
over zero values, and the real-handler tests (`reads_test.go`:
`TestHandleListAdminProviderConfigs`,
`TestHandleGetAdminProviderConfigFound`,
`TestListRevisionsAdminProviderConfig`,
`TestProviderConfigReadsRequireAllScope`) prove each leg serves
through `config.ReadHandler.Mount` with all-scope authorization.

Enforcement (all in go/internal/query/admin/provider/config):

- `read_admission_contract_test.go` — frozen method-set test plus
  version pin (`TestProviderConfigReadAdmissionContractIsVersioned`),
  witness (`providerConfigReadAdmissionWitness` + satisfier
  assertion), seeded-drift RED proof
  (`TestProviderConfigReadAdmissionSeededDriftIsDetected` — a planted
  `Create` mutation must not match the frozen set).
- `read_admission_contract_test.go` — seal
  (`TestProviderConfigReadAdmissionWireShapesAreSealed`) — exactly
  the 3 reads, no mutation/connection-test/secret surface.
- Route proof through the real handler: the `reads_test.go` tests
  above.
- Seal (`TestProviderConfigReadAdmissionWireShapesAreSealed`).
- Identity assertion in the pin file:
  `type ProviderConfigReadAdmission = ReadStore` — an alias, so the
  pin IS the staying handler-facing port (not a parallel shape), and
  any breaking change to `ReadStore` fails the frozen method-set test
  until the list is deliberately updated.

## Satellite consumption

Depend on the `ProviderConfigReadAdmission` port; satisfy it with the
existing family `ReadStore`. The `config.ReadHandler` legs already
admit exactly the pinned reads through it.

## W8a disclosed-absence posture

The three read legs are natively mounted in-tree
(`config.ReadHandler.Mount`,
go/internal/query/admin/provider/config/reads.go) and their OpenAPI
fragment stays core-side (oracle
`paths/auth/admin_provider_configs.go` with the codes in the table
above) — disclosed here, not silently dropped. No
`contracts/upstream/*` stub covers these legs: the wire-shape
generator snapshots forwarded relay spellings only, and these legs
have no forwarded spelling. The mount + core-side fragment + spec-key
list above is the complete termination record for the three
provider-config read legs.

## Explicitly OUT (not widened by this pin)

- The six `AdminProviderConfigMutations` rows (POST create, update,
  revert, enable, disable on
  `/api/v0/auth/admin/provider-configs…`, plus the `test-connection`
  POST) — covered by `ProviderConfigMutationAdmission` v0 (aliasing
  `MutationStore`) and the `ConnectionTester` port, neither widened to
  cover the other.
- Write-only secrets — no response, log line, or audit event ever
  carries plaintext or sealed ciphertext (`leakage_test.go`); every
  attempt is governance-audited. There is no secret method set to
  freeze.
- The `test-connection` POST (`ConnectionTester`: bounded OIDC
  discovery/JWKS reachability plus SAML validation, implemented
  outside go/internal/query in cmd/api) — a writer-adjacent probe,
  not a read here.
- D1-style append/audit surfaces and the setup-claim/admin/mfa legs —
  each keeps its own standing until a deliberate pin covers it.

## Rollback

Revert this pin. The read legs already admit through the
`ReadHandler`'s `ReadStore` field — the alias names the port the
handler uses, so rollback renames nothing on the request path and
moves no traffic.
