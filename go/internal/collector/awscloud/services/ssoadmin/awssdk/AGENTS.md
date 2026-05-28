# AGENTS.md - internal/collector/awscloud/services/ssoadmin/awssdk guidance

## Read First

1. `README.md` - adapter purpose, exported surface, and invariants.
2. `client.go` - AWS interfaces, NewClient, Snapshot orchestration, telemetry.
3. `mapping.go` - sso-admin list/describe pagination and mapping.
4. `principals.go` - identity store principal display-name resolution.
5. `contract_test.go` - the metadata-only interface-shape proof.
6. `../README.md` - scanner contract and forbidden data classes.

## Invariants

- `ssoAdminAPI` and `identityStoreAPI` are the only AWS reach. Never add a
  mutation API, GetInlinePolicyForPermissionSet,
  GetPermissionsBoundaryForPermissionSet, GetApplicationAccessScope,
  ListApplicationAccessScopes, or identity store membership/structured reads.
  The reflection test in `contract_test.go` enforces this.
- Read only the identity store `DisplayName` for principals.
- Never map application access-scope attributes.
- Force the `us-east-1` control-plane region in `NewClient`.
- Wrap every AWS call in `recordAPICall` so telemetry stays consistent.
- Degrade to a warning on AccessDenied/Unauthorized for the first reads; do not
  fail the whole claim.

## Common Changes

- Add a new safe read by extending the relevant AWS interface, updating the
  reflection test want-list, and writing the fake-backed adapter test first.
- Extend pagination with the SDK paginators already used in `mapping.go`.

## What Not To Change Without An ADR

- Do not add any forbidden API to the interfaces.
- Do not read inline policy, permissions boundary, customer-managed policy
  bodies, or application access scopes.
- Do not read structured identity store attributes beyond `DisplayName`.
- Do not move scanner fact selection into this adapter.
