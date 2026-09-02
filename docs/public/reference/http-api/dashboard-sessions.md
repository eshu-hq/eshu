# Dashboard Browser Sessions

Dashboard sessions are separate from explicit bearer tokens for CLI, MCP, and
automation clients. Programmatic clients should continue to send
`Authorization: Bearer ...`; they do not need CSRF headers.

The Console browser flow uses these `/api/v0` routes:

| Route | Purpose |
| --- | --- |
| `GET /api/v0/auth/providers` | **Public pre-auth, tenant-scoped.** Derives the tenant's sign-in posture (issue #5165): active OIDC/SAML/GitHub providers for the login page (`provider_kind` one of `oidc`, `saml`, `github` — issue #5166 adds `github`, F-5), whether the local username/password form is offered (`local_login_offered`, false under `require_sso`), and whether self-service personal API tokens are offered (`self_service_tokens_offered`). This is the single reusable posture derivation (`query.DeriveAuthPosture`) issue #5163's MCP OAuth-discovery route also consumes. When `tenant_id` is absent the response is always the safe zero-configuration default (empty providers, both offered flags true) — the endpoint never performs a global cross-tenant scan. Providers carry only opaque `provider_config_id` values, a display label, and an `icon_hint` — for `oidc`/`saml` these are safe generic protocol-class labels with no IdP brand information; `github` is the one deliberate exception (display label `"GitHub"`, icon hint `"github"`; the console composes this into the button text "Continue with GitHub") since a GitHub provider IS github.com or a GitHub Enterprise Server instance by construction, not an operator-chosen brand to protect. No secrets, metadata URLs, IdP hostnames, or org names are ever returned. Response carries `Cache-Control: public, max-age=60`. |
| `GET /api/v0/auth/oidc/login` | Starts a backend OIDC Authorization Code login and redirects the browser to the configured provider. |
| `GET /api/v0/auth/oidc/callback` | Completes OIDC login, validates state/nonce/provider proof, maps external groups to Eshu roles/grants, and issues browser-session cookies. |
| `GET /api/v0/auth/github/login` | Starts a backend GitHub Authorization Code login (issue #5166, F-5) and redirects the browser to the configured GitHub (or GitHub Enterprise Server) provider. GitHub is plain OAuth2, not OIDC — see `go/internal/githublogin`'s package docs — so there is no discovery, no ID token, and no nonce parameter. |
| `GET /api/v0/auth/github/callback` | Completes GitHub login: validates state, exchanges the code for an access token, resolves verified primary email/org membership/team membership from the GitHub REST API, denies (audited, no session) a caller with no verified email or no active membership in the provider's `allowed_orgs`, maps team handles (`org/team-slug`) to Eshu roles through the same group-to-role resolver an OIDC login's groups use, and issues browser-session cookies. |
| `POST /api/v0/auth/browser-session` | Exchanges an already-authenticated explicit API credential for a browser session. Existing browser sessions cannot mint another browser session. |
| `GET /api/v0/auth/browser-session` | Reads the current browser-session auth context without exposing raw secrets. |
| `DELETE /api/v0/auth/browser-session` | Revokes the current session by hash and clears browser cookies. Requires `X-Eshu-CSRF`. |
| `PATCH /api/v0/auth/browser-session/context` | Switches the active tenant/workspace for the current all-scopes browser session. Requires `X-Eshu-CSRF`. |
| `POST /api/v0/auth/local/bootstrap` | Shared-operator setup route that creates the first local owner/admin once. Admin MFA recovery material is required. |
| `GET /api/v0/auth/setup-state` | **Public pre-auth.** First-run setup wizard (#4965). Reports `{needs_setup, bootstrap_mode}`; the console routes to the wizard instead of the login page while `needs_setup` is true. `needs_setup` reflects whether the generated bootstrap admin credential (`ESHU_AUTH_BOOTSTRAP_MODE=generated`) remains unconsumed — never a session or cache. |
| `POST /api/v0/auth/setup/claim` | **Public pre-auth.** Wizard step 1: verifies the generated one-time bootstrap credential without mutating state. 401 on a wrong/expired credential (with a pointer to `eshu admin initial-credential`/`reset-initial-credential`); 410 once any identity exists. |
| `POST /api/v0/auth/setup/admin` | **Public pre-auth.** Wizard step 2: reproves the bootstrap credential and replaces its password with the operator's own choice. The bootstrap tenant/workspace slot is fixed — no operator-invented IDs. 410 once any identity exists. |
| `POST /api/v0/auth/setup/mfa` | **Public pre-auth.** Wizard step 3: reproves the bootstrap credential, enrolls a fresh set of MFA recovery codes (returned once in the response body, never logged or persisted in clear text), permanently consumes the bootstrap credential (sealing every setup route with 410 forever after), and issues a browser session. |
| `POST /api/v0/auth/local/login` | Public local identity login route. Passwords are verified against stored bcrypt hashes; admin accounts require MFA proof (a `totp_code` authenticator-app code, checked first when both are submitted, or a `recovery_code`, issue #4986) before a browser session is issued. When the caller's tenant has `require_sso=true` (`GET /api/v0/auth/admin/sign-in-policy`), a session is issued only if the authenticated identity is an admin — this is the break-glass path, reachable at the same endpoint regardless of the console's `/login?local=1` UI hint, which carries no server-side meaning. A credential flagged `must_change_password=true` (issue #4976) returns the `must_change_password` status instead of a session, even after a fully correct password and MFA proof; the caller must complete `POST /api/v0/auth/local/password/rotate` first. |
| `GET /api/v0/auth/sign-in-policy` | **Public pre-auth, tenant-scoped.** Returns only `require_sso`, scoped by the required `tenant_id` query parameter; an absent `tenant_id` or a read failure both default to `require_sso=false`. Compatibility endpoint: the console login page no longer calls it directly. As of issue #5165 the login page reads the same `require_sso` signal from `GET /api/v0/auth/providers`' `local_login_offered` field. Either way it is a UX hint only, not the enforcement boundary. |
| `POST /api/v0/auth/local/invitations` | All-scopes admin route that creates an assignment invite. Open self-signup is not supported. |
| `POST /api/v0/auth/local/invitations/accept` | Public invite-acceptance route. A valid active invite code is required to create a non-bootstrap local user. |
| `POST /api/v0/auth/local/users/{user_id}/password` | All-scopes admin route that resets a local password, revokes old credentials, and clears lockout state. |
| `POST /api/v0/auth/local/password/rotate` | **Public pre-session.** Self-service forced-rotation route (issue #4976). Re-proves `current_password` (and MFA proof — `totp_code` checked first, else `recovery_code` — when the account has an active MFA factor, issue #4986) instead of relying on an existing session, then stores the new password and clears `must_change_password`. This is the only way the `ESHU_ADMIN_USERNAME`/`ESHU_ADMIN_PASSWORD[_FILE]`-seeded bootstrap admin — which always starts with `must_change_password=true` — can obtain a session; any local user may also use it to voluntarily rotate their own password. Returns the same session-response shape as login, including `mfa_required` (202) and `locked` (423). |
| `POST /api/v0/auth/local/users/{user_id}/mfa-reset` | All-scopes admin route that revokes active MFA factors and stores replacement recovery-code hashes. |
| `POST /api/v0/auth/local/users/{user_id}/disable` | All-scopes admin route that disables the user and revokes local credentials, MFA factors, and browser sessions. |
| `POST /api/v0/auth/local/api-tokens` | Self-service create (issue #5164). Any authenticated caller may create a `personal` token bound to their OWN identity: `user_id` resolves from the session subject, and naming another `user_id` or a `service_principal_id` is rejected with 403. An all-scope admin may additionally create for another user (explicit `user_id`) or create `service_principal` tokens. The `api_token` value is returned once and only its hash is persisted. An optional `display_label` is persisted as plaintext (non-secret, issue #3708) for list surfaces to render; it is separate from the `token_hash`. Shared-operator callers must include `tenant_id` and `workspace_id`. |
| `GET /api/v0/auth/local/api-tokens` | Self-service read that lists the authenticated caller's own generated API tokens: `token_id`, `token_class`, `display_label` (when set), and issued/expires/revoked timestamps. Never returns the token hash, the display-label hash, or another subject's tokens. |
| `POST /api/v0/auth/local/api-tokens/{token_id}/revoke` | Self-service revoke (issue #5164). Any authenticated caller may revoke a token they OWN; ownership is enforced atomically in storage, so a token the caller does not own returns 404 without disclosing whether it exists. An all-scope admin may revoke any token in the tenant/workspace. Shared-operator callers must include `tenant_id` and `workspace_id`. |
| `POST /api/v0/auth/local/api-tokens/{token_id}/rotate` | Self-service rotate (issue #5164). Any authenticated caller may rotate a token they OWN, atomically creating a replacement and revoking the old token (carrying the old token's `display_label` forward); a token the caller does not own returns 404 without disclosing whether it exists. An all-scope admin may rotate any token in the tenant/workspace. Shared-operator callers must include `tenant_id` and `workspace_id`. |
| `POST /api/v0/auth/local/mfa/totp/begin` | Self-service route for the caller's own local identity (issue #4986, any authenticated session — not admin-only). Generates a fresh TOTP shared secret, seals it, and persists a `pending` MFA factor. Returns the plaintext secret exactly once as an `otpauth://` provisioning URI (for QR rendering) and a base32 manual-entry string; the factor cannot satisfy an MFA login challenge until `.../mfa/totp/confirm` verifies a first code. |
| `POST /api/v0/auth/local/mfa/totp/confirm` | Self-service route for the caller's own local identity (issue #4986). Verifies the first submitted authenticator-app code against the pending factor named by `factor_id` and activates it on match; a wrong code leaves the factor pending so the caller may retry. |
| `GET /api/v0/auth/local/invitations` | All-scopes admin read that lists invitations within the caller's own tenant/workspace (invite id, role, status, lifecycle timestamps). Never returns the invite code, invitee handle, or inviter identity. |
| `GET /api/v0/auth/admin/role-assignments` | All-scopes admin read that lists membership-role assignments in the caller's tenant/workspace, optionally filtered by `user_id`. |
| `GET /api/v0/auth/admin/roles` | All-scopes admin read that lists the caller's tenant roles and the capability grants each role confers. Never returns role key hashes or hashed scope selectors. |
| `GET /api/v0/auth/admin/idp-providers` | All-scopes admin read that lists the caller's tenant identity providers (config id, kind, status only). Never returns issuer/metadata/entity/client hashes or credential handles. |
| `GET /api/v0/auth/admin/provider-configs` | All-scopes admin read that lists the caller's tenant's DB-backed and env/file-registered identity provider configs, merged (env-file authoritative; a colliding DB row is `shadowed_by_environment=true`). Never returns a secret — only `has_secret`, `secret_fingerprint`, and `key_id`. |
| `GET /api/v0/auth/admin/provider-configs/{provider_config_id}` | All-scopes admin read for one provider config's full metadata, including its non-secret `configuration` (issuer/client_id/scopes/group_claim for OIDC; metadata_url/entity_id/group_attribute/service_provider_entity_id/service_provider_acs_url for SAML; client_id/base_url/api_base_url/scopes/allowed_orgs for GitHub, issue #5166). Never returns a secret. |
| `GET /api/v0/auth/admin/provider-configs/{provider_config_id}/revisions` | All-scopes admin read that lists a provider config's revision history, newest first. Never returns a secret — only `has_secret` per revision. |
| `GET /api/v0/auth/admin/idp-group-mappings` | All-scopes admin read that lists the caller's tenant/workspace external group→role mappings via an opaque mapping reference. Never returns the external group hash. |
| `GET /api/v0/auth/admin/api-tokens` | All-scopes admin read that lists every user's generated API tokens in the caller's tenant/workspace (token id, class, owner, status, `display_label` when set, timestamps). Never returns the token hash. |
| `GET /api/v0/auth/admin/audit/events` | All-scopes admin read that lists governance audit events filtered by `event_type`, `decision`, `reason_code`, `occurred_after`/`occurred_before`, and bounded `limit`. Returns audit-safe fields only. Tenant-scoped: a tenant admin sees only their own tenant's events; a shared-operator caller sees all tenants (global system events with no tenant attribution are visible only to the shared operator). |
| `GET /api/v0/auth/admin/audit/summary` | All-scopes admin read that returns aggregate-only governance audit counts, scoped the same way as `/audit/events` (own-tenant for a tenant admin, all-tenant for a shared operator). |
| `POST /api/v0/auth/local/invitations/{invite_id}/revoke` | All-scopes admin mutation that soft-revokes one pending invitation in the caller's tenant/workspace. Idempotent: already-revoked, accepted, or expired invitations are a no-op returning the current status. Never returns or echoes the invite code. Every allowed and denied attempt is governance-audited. |
| `POST /api/v0/auth/admin/role-assignments` | All-scopes admin mutation that grants a role assignment to a user in the caller's tenant/workspace. Validates the role is active and the user has an active tenant membership; an unknown role or non-member returns 400. Idempotent upsert on the full primary key. Every allowed and denied attempt is governance-audited. |
| `POST /api/v0/auth/admin/role-assignments/revoke` | All-scopes admin mutation that revokes an active role assignment in the caller's tenant/workspace. Idempotent: an already-revoked or absent assignment is a no-op. Every allowed and denied attempt is governance-audited. |
| `POST /api/v0/auth/admin/idp-group-mappings` | All-scopes admin mutation that creates an external group→role mapping in the caller's tenant/workspace. The raw `external_group` value is hashed server-side with the same hash the OIDC login path uses; the raw group name is never stored or returned. This same mapping table and hash also serves GitHub team→role mapping (issue #5166) — set `external_group` to a GitHub team handle in `"org/team-slug"` form; `identity_provider_group_role_mappings` has no `provider_kind` column, so no separate GitHub-specific mapping route exists. Returns an opaque `mapping_ref` for subsequent deletion. Idempotent upsert on the full primary key. Every allowed and denied attempt is governance-audited. |
| `DELETE /api/v0/auth/admin/idp-group-mappings/{mapping_ref}` | All-scopes admin mutation that deletes an external group→role mapping identified by its opaque `mapping_ref` (a non-secret md5 digest over the composite key). Scoped strictly to the caller's tenant/workspace. Idempotent: an absent or already-deleted mapping is a no-op. Every allowed and denied attempt is governance-audited. |
| `POST /api/v0/auth/admin/provider-configs` | All-scopes admin mutation that creates a DB-backed identity provider config in `draft` status with one active revision carrying the sealed secret (`client_secret` for OIDC; `sp_private_key`/`sp_certificate` for SAML; `client_secret` for GitHub, issue #5166 — `provider_kind: "github"` additionally requires a non-empty `allowed_orgs` list). Secret fields are write-only and never echoed back. Every allowed and denied attempt is governance-audited. |
| `POST /api/v0/auth/admin/provider-configs/{provider_config_id}` | All-scopes admin mutation that creates a new active revision for an existing provider config, superseding the current one. The full secret must be resupplied on every update — write-only secrets are never carried forward automatically. Every allowed and denied attempt is governance-audited. |
| `POST /api/v0/auth/admin/provider-configs/{provider_config_id}/revert` | All-scopes admin mutation that activates a prior revision, restoring its sealed secret automatically (no secret re-entry). Idempotent: reverting to the already-active revision is a no-op. Every allowed and denied attempt is governance-audited. |
| `POST /api/v0/auth/admin/provider-configs/{provider_config_id}/enable` | All-scopes admin mutation that re-runs a test-connection for the current active revision synchronously and only transitions the provider config to `active` if it passes — a draft provider without a passing test cannot be enabled. For a login-capable kind (`oidc`, `saml`, `github`), enable additionally rejects with `400` if the stored configuration is missing a field its login resolver requires but that create/test-connection leave optional (`redirect_url` for OIDC/GitHub; `service_provider_entity_id`, `service_provider_acs_url`, or inline `metadata_xml` for SAML) — issue #5604: without this check, a provider could pass test-connection, activate, and then 503 on every login attempt. Every allowed and denied attempt is governance-audited. |
| `POST /api/v0/auth/admin/provider-configs/{provider_config_id}/disable` | All-scopes admin mutation that transitions an active provider config back to `draft`. Idempotent. Every allowed and denied attempt is governance-audited. |
| `POST /api/v0/auth/admin/provider-configs/{provider_config_id}/test-connection` | All-scopes admin route that validates OIDC discovery/JWKS reachability (or SAML IdP metadata, or GitHub API-host reachability for `provider_kind: "github"`, issue #5166) and that the stored secret decrypts to well-formed material. Does not perform a live OAuth2 authorization-code round trip or a live SAML SSO exchange — that requires an interactive browser session and cannot be automated from an admin API call. Every allowed and denied attempt is governance-audited. |
| `GET /api/v0/auth/admin/sign-in-policy` | All-scopes admin read for the tenant's full sign-in policy, including SSO-admin-proof metadata (`sso_admin_verified_at`, `sso_admin_verified_provider_config_id`). |
| `PATCH /api/v0/auth/admin/sign-in-policy` | All-scopes admin mutation that partially updates the tenant sign-in policy. Setting `require_sso=true` is guarded: rejected with `400` unless the tenant has at least one provider config with a passing connection test (`status=active`) AND at least one admin has completed at least one SSO sign-in (`sso_admin_verified_at` set). Break-glass local admin sign-in always stays reachable, so this guardrail cannot lock a tenant out. Every allowed and denied attempt — including a guardrail rejection — is governance-audited. |
| `POST /api/v0/auth/local/break-glass` | Shared-operator route that enables one audited, time-boxed break-glass window. Disabled by default when no active window exists. |
| `POST /api/v0/auth/local/break-glass/session` | Public recovery route that issues a browser session only for an active, unexpired break-glass code. |
| `GET /api/v0/auth/saml/providers/{provider_id}/metadata` | Returns public SAML service-provider metadata for a configured provider. |
| `GET /api/v0/auth/saml/providers/{provider_id}/login` | Starts SP-initiated SAML login by storing a RelayState hash and redirecting to the IdP. Accepts an optional `return_to` query parameter (same-origin path only; absolute URLs and protocol-relative paths are silently discarded). |
| `POST /api/v0/auth/saml/providers/{provider_id}/acs` | Completes SAML login from IdP POST binding after RelayState, signature, replay, clock, NameID, and group-claim validation. Returns `201` with a JSON session body when no return path was stored, or `303` redirecting to the stored same-origin path when one was. |

A GitHub provider configured only through the admin API
(`POST /api/v0/auth/admin/provider-configs`) needs a second activation step
(issue #5605): `ESHU_AUTH_GITHUB_ENABLED=true` at API startup. Enabling the DB
provider config does not mount the route by itself — see
`ESHU_AUTH_GITHUB_ENABLED` in the
[Environment Variable Reference](../env-registry.md#api). Until the flag is set
and the API restarted, `GET /api/v0/auth/github/login` returns 404 even with an
active DB provider. A deployment that provides `ESHU_AUTH_GITHUB_CONFIG_FILE`
instead mounts the route from that config and does not need the flag.

OIDC login is optional and disabled until API startup receives an
operator-managed OIDC config file. The callback verifies provider metadata/JWKS,
state, nonce, redirect URI proof, and subject claims before creating a session.
Group claims map only to Eshu roles and grants; raw provider tokens and raw
group names are not persisted. If group mappings or grant targets are missing,
expired, or revoked, login is denied and no browser session is created. OIDC
sessions also carry hash-only provider proof metadata; when
`ESHU_AUTH_OIDC_SESSION_REFRESH_WINDOW` elapses, the API revokes the browser
session and requires fresh provider reauthentication before returning another
auth context.

When `ESHU_AUTH_OIDC_SESSION_REFRESH_ENABLED` is `true`, the API also runs a
bounded background active-session revocation refresh worker. On the cadence set
by `ESHU_AUTH_OIDC_SESSION_REFRESH_INTERVAL`, it scans up to
`ESHU_AUTH_OIDC_SESSION_REFRESH_BATCH_SIZE` stale OIDC sessions per pass and,
per session, either extends the bounded proof window after re-confirming the
Eshu-owned authorization snapshot or revokes the session. Disabled external
subjects, tombstoned or expired role mappings, revoked role targets, and
workspace policy-revision drift deny subsequent access within the window without
waiting for the next request. Provider or store failures defer the decision
rather than revoke, leaving the request-time stale check as the fail-closed
backstop. The worker persists only hash-only identity and emits
`eshu_auth_oidc_session_refresh_*` metrics for refresh passes, scanned sessions,
revocations, extensions, and provider-unavailable decisions.

Session cookies are server-managed:

- `__Host-eshu_session` contains the raw session secret and is set with
  `HttpOnly`, `Secure`, `SameSite=Strict`, and `Path=/`.
- The server stores only the SHA-256 session hash and can revoke the session
  immediately by that hash.
- `__Host-eshu_csrf` contains the CSRF secret, is readable by the browser, and
  is set with `Secure`, `SameSite=Strict`, and `Path=/`.
- Unsafe cookie-authenticated requests must send
  `X-Eshu-CSRF: <csrf secret>`. The server verifies the hash bound to the
  active session; missing or mismatched CSRF proof returns `403`.
- Session records enforce idle and absolute expiry before a request is treated
  as authenticated; successful session requests refresh the idle deadline,
  capped by the absolute expiry.
- OIDC-backed session records enforce the configured provider-proof staleness
  window before a request is treated as authenticated; stale sessions are
  revoked without storing provider tokens or raw group values.
- Workspace switching is limited to all-scopes browser sessions until the
  identity/grant UX can model explicit cross-workspace grants.
- Tenant-and-workspace-bound all-scopes browser sessions may use Console reads
  that do not yet implement repository filtering only when
  `ESHU_GOVERNANCE_MODE` is unset (the `local_no_policy` default),
  `local_no_policy`, or `hosted_single_tenant`. `hosted_multi_tenant` and
  unrecognized non-empty modes keep those routes fail-closed with `403`.
  The same mode rule now covers most of the scoped-route allowlist. An
  all-scopes browser session skips the mode check only on the identity and
  admin routes under `/api/v0/auth/` and on the static catalog routes, which
  hold no tenant data for a repository grant to filter. Grant-filtered data
  routes, deployment status routes, and `POST /api/v0/ask` follow the same
  fail-closed mode rule, because an all-scopes caller makes the handler's own
  repository filter inert.
  Restricted browser sessions and scoped bearer tokens remain limited to the
  existing scoped-route allowlist; live-data routes on that list apply their
  allowed repository/scope ids before counts, limits, and truncation, while
  static catalog routes read no tenant data.
- Local identity routes persist only hashes or credential handles for login
  identifiers, invite codes, MFA recovery codes, break-glass codes, and browser
  session secrets. Bootstrap and break-glass enablement require the shared
  operator bearer token. Admin lifecycle operations require an all-scopes admin
  context. Public local login, invite acceptance, and break-glass session routes
  do not bypass storage checks; they succeed only with valid hash-matched
  credentials or active invitation/recovery windows.
- Generated API-token lifecycle routes persist token hashes, active subject
  metadata, status, expiry, last-used timestamps, and an optional plaintext,
  non-secret `display_label` (issue #3708) that list endpoints render as-is.
  Creation and rotation responses return `api_token` exactly once; clients
  must store it immediately because later reads expose neither raw token
  values nor token hashes.
- OIDC-backed sessions carry `role_ids` in the returned auth context for UI
  display and audit correlation; repository and scope filtering still uses the
  resolved `allowed_scope_ids` and `allowed_repository_ids`.

SAML SSO uses the same server-managed session cookies after assertion
validation succeeds. The public SAML routes are unauthenticated because the IdP
must be able to read metadata and POST assertions before the browser has an
Eshu session. Eshu stores only hashes for RelayState, replay, session, CSRF,
external subject, and group-claim material. Raw SAML assertions, raw NameID
values, raw group values, provider secrets, and private operator endpoints must
not appear in API responses, logs, docs, issues, or proof artifacts.

SAML routes are enabled by `ESHU_SAML_PROVIDERS_JSON`. Each provider entry
uses a `provider_config_id` that already exists as an active
`identity_provider_configs` row, references IdP metadata through an environment
handle, validates the expected issuer and configured group claim names, and
maps normalized group claims to durable identity state. Login resolution
requires an active external subject row with the current group-claim hash plus
active membership, admin role, and all-scope role grant rows; missing identity
rows, stale group claims, or revoked grants fail closed. Malformed provider
JSON, unknown fields, or missing metadata env values fail closed during API
wiring.

Once the SAML runtime is enabled by at least one `ESHU_SAML_PROVIDERS_JSON`
entry, an enabled (`status: active`) DB-backed `external_saml` provider config
also resolves for login: it decrypts its sealed `sp_private_key`/
`sp_certificate` only in the `samlauth` login/authn path (never in any read
surface), and requires `service_provider_entity_id`/`service_provider_acs_url`/
`metadata_xml` in its `configuration` to resolve — a provider missing any of
them fails closed rather than presenting a non-functional login button. A DB
row that collides on `provider_config_id` with an env-registered provider is
always served from env config (`shadowed_by_environment`), matching OIDC's
precedence. A deployment with zero `ESHU_SAML_PROVIDERS_JSON` entries has the
SAML runtime disabled entirely, regardless of DB-backed provider configs
(unlike OIDC, SAML has no DB-only activation toggle yet).
