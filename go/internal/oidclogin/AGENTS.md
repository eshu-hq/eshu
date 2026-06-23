# AGENTS — internal/oidclogin

OIDC Authorization Code login for dashboard browser sessions. Read `README.md`
and `doc.go` before editing.

## Invariants

- Store only hashes for state, nonce, redirect URI, subject, and external group
  claims. Never persist raw ID tokens, access tokens, group names, emails,
  client secrets, or claim bodies.
- Login must fail closed. Do not fall back to shared-token, all-scope, or
  partially mapped access when provider verification, nonce validation, group
  mapping, role grants, tenant/workspace state, or policy revision checks fail.
- IdP groups are input to Eshu role mapping only. They must never become raw
  permissions or query filters directly.
- Callback state is one-time-use. Duplicate or replayed callbacks must not
  create another browser session.
- Errors returned to HTTP callers must be generic and must not include provider
  endpoints, token material, claim values, file contents, group names, or
  customer identifiers.

## Boundaries

- This package returns `query.AuthContext`; it does not write cookies or
  browser-session rows. Browser-session issuance lives in `internal/query` and
  `cmd/api`.
- Durable state and role-target storage live in `internal/storage/postgres`.
- OIDC provider discovery and token verification should keep using a reviewed
  library. Do not hand-roll JWKS, signature, expiry, or audience validation.
- `Refresher` decides extend-or-revoke for stale sessions but owns no storage,
  cadence, or telemetry. Its `SessionRefreshStore`, `RoleGrantResolver`, and
  `ExternalSubjectLookup` ports are implemented in `internal/storage/postgres`
  and wired in `cmd/api`. Keep each refresh pass bounded by batch size; never
  scan the full session table. Revoke and proof-update writes must stay
  idempotent under duplicate and concurrent delivery. A provider or store
  failure must defer the decision, not revoke.

## Tests

`service_test.go` covers state/nonce hashing, nonce mismatch denial, and
group-to-role-to-grant mapping. `refresher_test.go` covers revoke-on-lost-grant,
extend-when-authorized, disabled-subject revocation, provider-unavailable
deferral, batch bounding, idempotent concurrent passes, and empty-queue no-op.
Add a focused test for any new claim, provider, grant, or refresh behavior
before changing production code.
