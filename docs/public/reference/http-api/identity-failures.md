# Identity Route Failures

The identity administration, provider-config, and local identity routes answer
a failed store, tester, or generator step with a fixed message for that step,
never with the backend error text (#7674). The backend error goes on the
request span as an `exception` event, and the span status is Error with the
fixed message as its description. The
[Dashboard browser sessions](dashboard-sessions.md) page describes what each
route does.

## Statuses

- A failed step answers `500` with the fixed message below.
- A client that cancels its request gets `499` with the same fixed message.
  The request span stays Unset and carries an `eshu.request.client_canceled`
  event with no exception. No route documents `499` in the OpenAPI spec,
  because the client has already gone. See
  [Failed and canceled query reads](../telemetry/traces.md#failed-and-canceled-query-reads-7626).
- A step whose generator returned an empty value with no error of its own
  still answers `500` with its fixed message; it never reads as a cancel.
- The existing `400`, `401`, `403`, `404`, `409`, and `422` answers, and the
  `503` answers for a store, reader, tester, or keyring that is not wired, do
  not change.
- These routes read and write through the writer store, except the two audit
  reads, which read through the guarded PostgreSQL reader. There a stale or
  timed-out reader answers the retryable `503` with `Retry-After` and no
  `capability` field, and a client cancel answers `499` with no error log
  line; see
  [Admin audit reads](status-evidence-failures.md#admin-audit-reads).

## Admin identity

| Route | Failed step | Fixed message |
| --- | --- | --- |
| `GET /api/v0/auth/local/invitations` | invitation read | `failed to list invitations` |
| `GET /api/v0/auth/admin/role-assignments` | assignment read | `failed to list role assignments` |
| `GET /api/v0/auth/admin/roles` | role read | `failed to list roles` |
| `GET /api/v0/auth/admin/idp-providers` | provider read | `failed to list idp providers` |
| `GET /api/v0/auth/admin/idp-group-mappings` | mapping read | `failed to list idp group mappings` |
| `GET /api/v0/auth/admin/api-tokens` | token read | `failed to list api tokens` |
| `GET /api/v0/auth/admin/audit/events` | audit read | `failed to list audit events` |
| `GET /api/v0/auth/admin/audit/summary` | audit summary | `failed to summarize audit events` |
| `POST /api/v0/auth/local/invitations/{invite_id}/revoke` | revoke | `failed to revoke invitation` |
| `POST /api/v0/auth/admin/role-assignments` | grant | `failed to grant role assignment` |
| `POST /api/v0/auth/admin/role-assignments/revoke` | revoke | `failed to revoke role assignment` |
| `POST /api/v0/auth/admin/idp-group-mappings` | create | `failed to create idp group mapping` |
| `DELETE /api/v0/auth/admin/idp-group-mappings/{mapping_ref}` | delete | `failed to delete idp group mapping` |

## Provider configs

| Route | Failed step | Fixed message |
| --- | --- | --- |
| `GET /api/v0/auth/admin/provider-configs` | list | `failed to list provider configs` |
| `GET /api/v0/auth/admin/provider-configs/{provider_config_id}` | read | `failed to read provider config` |
| `GET /api/v0/auth/admin/provider-configs/{provider_config_id}/revisions` | revision read | `failed to list provider config revisions` |
| `POST /api/v0/auth/admin/provider-configs` | id generation | `failed to generate provider config id` |
| create, update | revision id generation | `failed to generate provider config revision id` |
| create, update, revert, enable, disable | store write | `failed to write provider config` |
| `POST /api/v0/auth/admin/provider-configs/{provider_config_id}/test-connection` | tester | `connection test failed to run` |

A store write error the routes classify keeps its own answer: `409` for a
duplicate or a revision that changed, `404` for an unknown revision, `400` for
a kind mismatch or an environment-managed provider, and `503` when provider
secret encryption is not configured.

## Local identity

| Route | Failed step | Fixed message |
| --- | --- | --- |
| `POST /api/v0/auth/local/bootstrap` | identifier generation | `failed to create local identity bootstrap` |
| `POST /api/v0/auth/local/invitations` | identifier generation | `failed to create local identity invitation` |
| `GET /api/v0/auth/local/api-tokens` | token read | `failed to list api tokens` |
| `POST /api/v0/auth/local/api-tokens` | user lookup, token generation | `failed to create local identity api token` |
| `POST /api/v0/auth/local/api-tokens/{token_id}/rotate` | token generation | `failed to rotate local identity api token` |
| `POST /api/v0/auth/local/mfa/totp/begin` | user lookup, secret, factor, or provisioning URI generation | `failed to begin totp enrollment` |
| `POST /api/v0/auth/local/mfa/totp/confirm` | user lookup | `failed to confirm totp enrollment` |
| login, break-glass session, password rotation, setup wizard | session issuance | `failed to create local identity session` |

## A 499 on a mutation

A `499` says only that the client left. It does not say that the write did
not apply. Each admin identity and provider-config write is one store call:
a cancel that lands while it commits can leave it applied. Re-read the
affected list before you retry. Grants, revokes, and mapping writes are
idempotent, so a retry is safe.
