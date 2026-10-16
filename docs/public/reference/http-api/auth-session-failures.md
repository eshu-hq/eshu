# Auth And Session Route Failures

The SAML, OIDC, and GitHub sign-in routes, the browser-session routes, the
first-run setup wizard, provider discovery, the admin sign-in policy routes,
and the profile route answer a failed store, service, or generator step with a
fixed message for that step, never with the backend error text (#7674). The
backend error can quote SQL, hosts, provider configuration, or an identity
provider response, so it goes on the request span as an `exception` event,
and the span status is Error with the fixed message as its description. The
[Dashboard browser sessions](dashboard-sessions.md) page describes what each
route does.

## Statuses

- A failed step answers `500` with the fixed message below. Each message is
  the same text the route answered before this change.
- A client that cancels its request gets `499` with the same fixed message.
  The request span stays Unset and carries an `eshu.request.client_canceled`
  event with no exception. A cancel counts only when the error wraps
  `context.Canceled` and the request's own context was canceled; a cancel
  from inside the server is still a `500`. No route documents `499` in the
  OpenAPI spec, because the client has already gone. See
  [Failed and canceled query reads](../telemetry/traces.md#failed-and-canceled-query-reads-7626).
- A step that fails with no error of its own still answers `500` with its
  fixed message and records a fixed cause on the span. These steps are a
  SAML request id or relay state that hashed to empty, a resolved SAML
  principal with no tenant or workspace, a session secret that hashed to
  empty, and a login service that returned no redirect URL. A metadata
  render failure, a recovery-code entropy failure, and a failure of the
  session, CSRF, relay-state, or setup identifier generator never see the
  request context, so in production they answer `500` and never `499`.
- These routes read and write through the PostgreSQL writer, not the guarded
  reader, so no step answers the retryable reader-fence `503`.
- The existing `400`, `401`, `403`, `404`, `409`, and `410` answers, and the
  `503` answers for a store, service, or verifier that is not wired, do not
  change.
- The browser-session list, profile, and admin sign-in policy read and update
  routes log their own failure line at ERROR for a server fault, and not for
  a client cancel. On the sign-in policy update, a client cancel still logs
  `governance audit append failed` at ERROR: see
  [The audit trail on a 499](#the-audit-trail-on-a-499).

## SAML

| Route | Failed step | Fixed message |
| --- | --- | --- |
| `GET /api/v0/auth/saml/providers/{provider_id}/metadata` | provider lookup | `failed to load saml provider` |
| `GET /api/v0/auth/saml/providers/{provider_id}/metadata` | metadata render | `failed to render saml metadata` |
| `GET /api/v0/auth/saml/providers/{provider_id}/login` | provider lookup | `failed to load saml provider` |
| `GET /api/v0/auth/saml/providers/{provider_id}/login` | relay state generation, empty request hash | `failed to create saml request` |
| `GET /api/v0/auth/saml/providers/{provider_id}/login` | AuthnRequest build | `failed to build saml request` |
| `GET /api/v0/auth/saml/providers/{provider_id}/login` | request store write | `failed to persist saml request` |
| `POST /api/v0/auth/saml/providers/{provider_id}/acs` | provider lookup | `failed to load saml provider` |
| `POST /api/v0/auth/saml/providers/{provider_id}/acs` | request consume | `failed to consume saml request` |
| `POST /api/v0/auth/saml/providers/{provider_id}/acs` | replay key reserve | `failed to reserve saml replay key` |
| `POST /api/v0/auth/saml/providers/{provider_id}/acs` | principal resolve | `failed to resolve saml principal` |
| `POST /api/v0/auth/saml/providers/{provider_id}/acs` | principal without tenant or workspace | `saml principal resolved without tenant or workspace` |
| `POST /api/v0/auth/saml/providers/{provider_id}/acs` | session secret generation, session write | `failed to create browser session` |

A failed assertion check still answers the same `401` for every cause, so the
fixed `500` messages are no oracle for the assertion.

## Browser sessions and SSO callbacks

| Route | Failed step | Fixed message |
| --- | --- | --- |
| `POST /api/v0/auth/browser-session` | secret generation, empty secret hash, session write | `failed to create browser session` |
| `DELETE /api/v0/auth/browser-session` | revoke | `failed to revoke browser session` |
| `PATCH /api/v0/auth/browser-session/context` | workspace switch | `failed to switch browser session workspace` |
| `GET /api/v0/auth/sessions` | session list | `failed to list browser sessions` |
| `GET /api/v0/auth/oidc/login` | login service error | `oidc login failed` |
| `GET /api/v0/auth/oidc/login` | no redirect URL | `oidc login did not return a redirect URL` |
| `GET /api/v0/auth/oidc/callback` | login service error | `oidc login failed` |
| `GET /api/v0/auth/oidc/callback` | session issuance | `failed to create browser session` |
| `GET /api/v0/auth/github/login` | login service error | `github login failed` |
| `GET /api/v0/auth/github/login` | no redirect URL | `github login did not return a redirect URL` |
| `GET /api/v0/auth/github/callback` | login service error | `github login failed` |
| `GET /api/v0/auth/github/callback` | session issuance | `failed to create browser session` |

The OIDC and GitHub login services classify their own state-store,
code-exchange, identity, and grant failures as `503`, `400`, or `403` with the
existing fixed text. A cancel during one of those steps answers that status.
Only an error the service does not classify answers `500` `... login failed`,
or `499` when it is a cancel of the request.

## First-run setup

| Route | Failed step | Fixed message |
| --- | --- | --- |
| `GET /api/v0/auth/setup-state` | setup state read | `failed to determine setup state` |
| `POST /api/v0/auth/setup/claim`, `/admin`, `/mfa` | setup-open check | `failed to determine setup state` |
| `POST /api/v0/auth/setup/claim`, `/admin`, `/mfa` | bootstrap credential check | `failed to verify bootstrap credential` |
| `POST /api/v0/auth/setup/admin`, `/mfa` | owner resolve | `failed to resolve setup owner` |
| `POST /api/v0/auth/setup/admin` | credential id generation, password write | `failed to set administrator password` |
| `POST /api/v0/auth/setup/mfa` | recovery-code generation | `failed to generate recovery codes` |
| `POST /api/v0/auth/setup/mfa` | MFA factor id generation, completion | `failed to complete setup` |

The bootstrap credential check answers `500` only when the check itself
fails: a store error, or a bootstrap decryption key that is not configured. A
wrong, expired, or consumed credential answers the same `401` for every cause. The
session issuance inside `POST /api/v0/auth/setup/mfa` answers
`failed to create local identity session`, owned by the local identity
routes.

## Discovery, sign-in policy, and profile

| Route | Failed step | Fixed message |
| --- | --- | --- |
| `GET /api/v0/auth/providers` | provider list | `failed to list login providers` |
| `GET /.well-known/oauth-protected-resource` (and its path-suffixed form) | provider list | `failed to derive auth posture` |
| `GET /api/v0/auth/admin/sign-in-policy` | policy read | `failed to read sign-in policy` |
| `PATCH /api/v0/auth/admin/sign-in-policy` | policy write | `failed to update sign-in policy` |
| `GET /api/v0/auth/profile` | MFA status read | `failed to fetch mfa status` |

The public `GET /api/v0/auth/sign-in-policy` route is unchanged: a failed read
still answers `200` with `require_sso=false`, its documented fail-open hint.

## A 499 on a mutation

A `499` says only that the client left. It does not say that the write did not
apply. A cancel that lands while a write commits can leave it applied. These
are the writes, and what a retry does:

- SAML login: the pending AuthnRequest row can commit with no redirect sent.
  It is never used and expires after five minutes. A retried login is safe.
- SAML ACS: the request consume runs first, as one update. A consume that
  committed spends the AuthnRequest. Every later step runs only after a
  committed consume, so after a `500` or `499` at a later step, a retried ACS
  post answers `401`: start the login again. A `500` or `499` at the consume
  step itself does not say whether the update committed. If it did not, the
  AuthnRequest stays pending until it expires, and a retried post within five
  minutes can complete the login. If it did, the retry answers `401`. When
  the failure is at the session
  write, an allowed `sso_login_authenticated` audit event was already
  written, and a session row can commit whose secret never reached the
  client. That session cannot be used.
- `POST /api/v0/auth/browser-session` and the OIDC and GitHub callbacks: a
  session row can commit whose secret never reached the client. That session
  cannot be used. The callbacks spend their one-time login state before the
  session write, so a retried callback answers `403`: start the login again.
- Logout: the revoke can apply while the cookies stay in the browser. Retry
  the logout; a `401` on the retry means the revoke already applied.
- Workspace switch: the switch can apply. A retry with the same target is
  safe.
- Setup admin step: the password can be set. The bootstrap credential is
  still unconsumed, so a retry sets the password again and is safe.
- Setup MFA step: the browser session is issued before the factor id and the
  completion. A `500` or `499` from either still carries the session cookie,
  and the session row stays. The completion is one atomic transaction that
  stores the recovery-code hashes and consumes the bootstrap credential. A
  cancel that lands while it commits can seal setup with the recovery codes
  never shown. A retry then answers `410`. Run
  `eshu admin reset-initial-credential` to rotate the password and re-enroll
  the recovery-code factor, as the
  [User management runbook](../../operate/user-management-runbook.md)
  describes.
- Admin sign-in policy update: one upsert. A retry with the same body applies
  the same values and is safe.

## The audit trail on a 499

These failed steps write a governance audit event on the request context:
the SAML ACS provider lookup, consume, replay reserve, and principal resolve;
the OIDC and GitHub callback service errors; the setup bootstrap credential
check and owner resolve; and the sign-in policy update. The other setup
failures, such as the admin password write and the MFA completion, record
only the setup outcome metric. On a `499` that context is canceled, so the audit store rejects the
event, unless the append ran before the cancel landed. The sign-in policy
update then logs `governance audit append failed`
at ERROR. The SAML, SSO callback, and setup audits discard the append error,
so their event is lost with no log line. If the cancel lands while a write
commits, the applied write has no success audit event either.

## Related pages

- [Admin route failures](admin-failures.md)
- [Selector and read failures](selector-and-read-failures.md)
