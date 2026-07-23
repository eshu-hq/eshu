# HTTP API Reference

The HTTP API is versioned under `/api/v0` and shares the same query model as
CLI and MCP. Use it for AI agents, automation, Console, and internal tools that
need stable JSON contracts.

This page is the map. The detailed route contracts live in focused pages so the
API reference stays readable.

## OpenAPI Source Of Truth

The live OpenAPI spec is canonical. If a narrative page and the spec disagree,
the spec wins.

- `GET /api/v0/openapi.json` - machine-readable schema
- `GET /api/v0/docs` - Swagger UI
- `GET /api/v0/redoc` - ReDoc reference

The mounted Go runtime admin OpenAPI contract lives in
`docs/openapi/runtime-admin-v1.yaml`. That contract is separate from the public
`/api/v0` schema because it describes service-local probes and admin status.

## Route Families

| Need | Start here |
| --- | --- |
| Health, readiness, index status, queue/admin controls, ingester status | [Status and admin routes](http-api/status-admin.md) |
| Capability maturity catalog (`GET /api/v0/capabilities`) | [Capability Catalog](capability-catalog.md#surfaces) |
| Surface inventory readiness (`GET /api/v0/surface-inventory`) | [Surface Inventory](surface-inventory.md#drift-gate) |
| Dashboard browser sessions, SAML SSO, and CSRF-safe Console auth | [Dashboard browser sessions](#dashboard-browser-sessions) |
| Component extension inventory and diagnostics | [Status and admin routes](http-api/status-admin.md#component-extension-inventory) and [Component Package Manager](component-package-manager.md) |
| Optional semantic observations and code hints | [Semantic evidence routes](http-api/semantic-evidence.md) |
| Repository-bounded semantic retrieval over curated search documents | [Semantic search route](http-api/semantic-search.md) |
| Deployment evidence, admission decisions, citations, documentation findings, packages, CI/CD, SBOM, vulnerability impact, codeowners ownership | [Evidence and supply-chain routes](http-api/evidence-and-supply-chain.md) |
| Investigation evidence packets for supply-chain impact, deployable-unit truth, and runtime drift | [Investigation Evidence Packet Contract](investigation-evidence-packet.md#http-and-mcp-surfaces) |
| Source repository to container image identity bridge | [Container image source bridge](http-api/container-image-source-bridge.md) |
| Secrets/IAM trust chains, posture evidence, access paths, gaps, and posture summary | [Secrets/IAM routes](http-api/secrets-iam.md) |
| Entity resolution, incident context, catalog, repository/service/workload stories, investigations | [Context and story routes](http-api/context-and-stories.md) |
| Code search, symbols, relationships, call chains, dead-code, complexity, quality, language queries | [Code routes](http-api/code.md) |
| IaC cleanup, AWS drift, content reads/search, infra impact, environment comparison | [IaC, content, and infra routes](http-api/iac-content-infra.md) |
| Multi-cloud canonical resource inventory (AWS/GCP/Azure, bounded, paginated) | [Cloud inventory readback](#cloud-inventory-readback) |
| Repository catalog, repository context/stats/coverage, ingester status, bundle search | [Repository, ingester, and bundle routes](http-api/repositories-ingesters-bundles.md) |

## Shared Wire Contracts

Programmatic HTTP clients should opt in to the canonical envelope with:

```http
Accept: application/eshu.envelope+json
```

Without that header, handlers may emit older payload shapes for backward
compatibility. The canonical envelope, truth levels, freshness states, cache
rules, and error-code list are owned by
[Truth Label Protocol](truth-label-protocol.md).

Runtime profile ceilings are owned by
[Capability Conformance Spec](capability-conformance-spec.md). High-authority
capabilities such as transitive call graphs, call-chain paths, dead-code
cleanup, and cross-repo impact must return `unsupported_capability` when the
active profile cannot answer correctly.

## Shared Model Rules

- `workload` is the canonical deployable compute model.
- `service` is a convenience alias over workloads whose normalized kind is
  `service`.
- Environment-scoped calls return the logical workload plus a resolved
  `WorkloadInstance` when that evidence exists.
- Repository identity is remote-first when a git remote exists.
- Repository objects expose `repo_slug`, `remote_url`, and `local_path`.
- Repository list rows expose additive `group_*` evidence fields for
  source-backed grouping; missing evidence remains explicit.
- `local_path` is server-local metadata. It is not a portable client path.
- File-bearing results should be interpreted with `repo_id + relative_path`,
  not an absolute server path.
- `repo_access` tells a client whether it may need to ask the user for a local
  checkout path or clone decision.
- Path-based context routes require canonical entity IDs.
- Repository-oriented routes accept a public repository selector and normalize
  it to the canonical `repo_id` server-side.

## Authentication And Headerless Reads

A request presents its credential as `Authorization: Bearer <token>` (shared
`ESHU_API_KEY`, a scoped-token-file token, or an IdP-issued OIDC bearer token)
or, for the dashboard, a browser-session cookie. Public routes (`/health`,
`/api/v0/health`, `/api/v0/openapi.json`, the pre-auth setup/login routes, and
the rest of `publicHTTPPaths`) are always served without a credential.

Headerless requests to non-public routes are served open only when **no**
explicit credential source is configured. Configuring any one of `ESHU_API_KEY`,
`ESHU_SCOPED_TOKENS_FILE`, or `ESHU_AUTH_RESOURCE_URI` enables enforcement: a
non-public request with no `Authorization` header and no valid session cookie is
then rejected with `401`. With none of the three set the read surface is open
(the deliberate local/demo dev-mode; see
[Docker Compose](../run-locally/docker-compose.md)). Seeded bootstrap identities
and console-minted tokens are not enforcement signals on their own — close the
open read surface with one of those three environment variables. Both `cmd/api`
and `cmd/mcp-server` log the resolved posture (`auth.enforcement.configured` or
`auth.enforcement.open`) once at startup.

When `ESHU_AUTH_RESOURCE_URI` and at least one OIDC bearer provider are
configured, `cmd/mcp-server` also publishes an
[RFC 9728](https://www.rfc-editor.org/rfc/rfc9728.html) OAuth 2.0 Protected
Resource Metadata document at the unauthenticated
`/.well-known/oauth-protected-resource` route so OAuth-capable MCP clients can
discover where to obtain an access token, and adds a
`WWW-Authenticate: Bearer resource_metadata="…"` challenge to a credential-less
or unrecognized-credential `401`. A valid credential is served with no
challenge. See [MCP OAuth 2.1 Discovery](../operate/mcp-oauth-discovery.md).

## Dashboard Browser Sessions

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
[Environment Variable Reference](env-registry.md#api). Until the flag is set
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

## Ask Eshu — POST /api/v0/ask

Natural-language answer endpoint. **Default-off**: returns
`{"state":"unavailable","reason":"..."}` with HTTP 503 unless
`ESHU_ASK_ENABLED=true` and a valid `agent_reasoning` provider profile is
configured via `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON`.

**Request body:**
```json
{"question": "string (required)", "format": "auto|markdown|mermaid|json|yaml|csv (optional)"}
```

**JSON response (200)** — default, no special `Accept` header required:
```json
{
  "answer_prose":     "string (LLM narration when available)",
  "artifacts":        [{"format":"string","content":"string","issues":["string"]}],
  "truth_class":      "deterministic|derived|fallback|semantic_observation|code_hint|unsupported",
  "result_ref":       "string (addressable canonical API result)",
  "result":           {"total": 123},
  "evidence_handles": [...],
  "citation_ref":     "string (citation packet that hydrates the evidence handles; coverage anchor for derived prose)",
  "applied_facets":   {
    "source_tool":      "string (canonical tool token, e.g. 'helm'; omitted when not detected)",
    "language":         "string (language name, e.g. 'go'; omitted when not detected)",
    "unknown_tool_note":"string (human note when question names a non-canonical tool; omitted when absent)"
  },
  "query_trace":      [{"tool":"string","args":{},"supported":bool,"truth_class":"string","err":"string"}],
  "partial":          false,
  "limitations":      ["string"]
}
```

The `123` value is illustrative. Exact repository-count answers use the current
authorized `list_indexed_repositories.total`, so the value varies by caller and
corpus rather than representing a fixed product count.

`applied_facets` is omitted when the question has no detectable tool or language scope. When
present, it records what was detected before the agent loop ran: `source_tool` and `language`
are used to steer the LLM toward passing those values as `source_tool`/`languages` arguments
to `list_relationship_edges` and `search_semantic_context`; the actual server-side filter
executes deterministically inside those tool handlers. `unknown_tool_note` is set when the
question appears to name a specific tool that is not in the canonical vocabulary; the answer
is then returned without a tool filter and the note also appears in `limitations`.

Narrated prose and rendered artifacts pass through runtime answer guardrails
before they are returned. A guardrail failure for citation coverage or
publish-safety suppresses `answer_prose` and `artifacts`, sets `partial: true`,
and adds a bounded limitation such as
`runtime answer guardrail blocked publishable prose: publish_safety` without
echoing the rejected value. The same pure guardrail logic is used by the
answer-quality scorecard, so runtime Ask and CI scoring share the citation and
publish-safety rules.

**SSE variant** — send `Accept: text/event-stream` to receive a
`text/event-stream` response with `Cache-Control: no-cache`. When the
configured provider adapter supports streaming, tool-trace events are emitted
live as the engine runs. Narration token deltas are buffered and emitted only
after runtime guardrails pass for both the final answer and the buffered stream.
Event sequence:

| Event          | Data payload                                                             |
|----------------|-------------------------------------------------------------------------|
| `token`        | `{"delta":"string"}` — validated narration prose, emitted only after final answer and buffered-stream guardrails pass |
| `trace`        | `{"tool":"string","supported":bool,"truth_class":"string"}` — one per completed tool call |
| `answer`       | Full JSON response identical to the 200 JSON path                       |
| `error`        | `{"state":"unavailable","reason":"string"}` — on engine failure         |
| `done`         | `{}` — end-of-stream marker                                             |

`token` events carry validated assistant prose and are therefore subject to the
same default-closed governance as `answer_prose`: they are emitted **only when
the governed answer-narration posture is available** for the request and both
the final answer and buffered stream pass guardrails. Raw provider text-token
deltas are never emitted. When narration is not enabled (the default) or runtime
guardrails suppress narration, no `token` events are sent — clients receive the
live `trace` events plus the final governed `answer` (whose `answer_prose` is
present only when `Narrated` is true and guardrails pass). This keeps the SSE and
JSON paths consistent and prevents unvalidated LLM prose from reaching the
client.
When the adapter does not support streaming (e.g. a synchronous-only profile),
the handler falls back to a synchronous run and emits `trace`, `answer`, and
`done` without `token` events. Clients should handle all cases.

### Agent loop budget (tunable)

The agent loop bounds both how many reasoning rounds it runs and how many tool
calls it dispatches per round. Weaker or slower providers (for example
`deepseek-chat`) sometimes need more rounds to converge than the default
budget allows; without a knob they return a partial answer with limitations
such as `tool calls truncated to 4 per turn` and `reached max reasoning
iterations`. Two environment variables make the budget tunable. They are read
once at startup by `BuildAskHandler`.

| Variable | Default | Ceiling | Meaning |
|----------|---------|---------|---------|
| `ESHU_ASK_MAX_ITERATIONS` | 6 | 32 | Maximum LLM completion / tool-call rounds before the loop stops and marks the answer partial. |
| `ESHU_ASK_MAX_TOOL_CALLS_PER_TURN` | 4 | 16 | Maximum tool calls dispatched in a single completion turn. Extra calls in a turn are truncated. |

Safety rules (the knobs never silently loosen the bound):

- Unset, empty, non-numeric, zero, or negative values keep the default.
- Values above the ceiling are clamped to the ceiling and a clamp is logged at
  `WARN`.
- The resolved budget is logged at startup
  (`ask: engine budget resolved max_iterations=… max_tool_calls_per_turn=…`).

Operators raising these knobs should weigh provider cost: each iteration is at
least one provider completion, and each turn may issue up to
`ESHU_ASK_MAX_TOOL_CALLS_PER_TURN` in-process tool calls.

### Partial-answer narration

When the answer is partial (the loop hit its iteration budget, a result was
truncated, or a packet carries limitations), governed narration must surface
that partial signal — narration that presents a partial answer as complete is
rejected by the narration validator. The narration prompt is partial-aware: for
a partial packet it instructs the model to add one sentence with a
`limitation` / `unsupported_reason` / `freshness` provenance reference drawn
from the packet, so legitimate evidence-backed narration of a partial answer is
accepted instead of being dropped with a `narration rejected by validator`
limitation.

Disabled endpoint (`h.Asker == nil`) or validation failures (empty question,
bad JSON) are returned as plain JSON with the appropriate HTTP status code
**before** the event stream is opened.

**Error responses:** 400 (empty/missing question), 401 (unauthenticated),
503 (disabled or provider absent). The engine never echoes provider prompts,
raw provider bodies, or credentials.

**Authentication:** This endpoint accepts both the **shared token**
(admin/full-scope `ESHU_API_KEY`) and **scoped tokens**. A scoped caller's
answer is bounded to its grant: the engine's in-process runner re-dispatches
every inner tool call through the same scoped-route gate under the caller's
token, so the model can only reach routes that are themselves scope-safe (the
allowlist in `scopedHTTPRouteSupportsTenantFilter`). A tool that maps to a
non-allowlisted whole-graph route (e.g. `get_ecosystem_overview`) is denied with
`403` to the runner and surfaces as an unsupported tool in the answer — never as
cross-scope data. The Ask endpoint itself holds no graph query; its scoping is
enforced entirely through those inner dispatches.

**Follow-ups (out of scope for this PR):** Tier-2 Cypher/SQL sandbox wiring.

## Cloud Inventory Readback

`GET /api/v0/cloud/inventory` returns reducer-owned canonical
`reducer_cloud_resource_identity` rows (one per `cloud_resource_uid`). It is
filterable by `provider` (aws/gcp/azure), `scope_id` (or its aliases
`account_id`, `project_id`, `subscription_id`), and `management_origin`
(declared/applied/observed). Results are paginated via `limit` and `cursor`
parameters. `local_lightweight` returns `unsupported_capability`.

Each resource item in the `resources` array carries:

| Field | Description |
| --- | --- |
| `cloud_resource_uid` | Canonical shared identity key |
| `provider` | Normalized provider token: `aws`, `gcp`, or `azure` |
| `resource_type` | Provider resource type string |
| `management_origin` | Strongest contributing evidence layer |
| `scope_id` | Canonical scope (account/project/subscription) |
| `generation_id` | Evidence generation that produced this row |
| `source_state` | Provider-neutral truth label derived from `management_origin` |
| `evidence` | Per-layer boolean flags: `declared`, `applied`, `observed` |
| `tag_value_fingerprints` | Optional keyed non-reversible tag value markers; raw tag values are never returned |
| `identity_policy_evidence` | Optional bounded Azure identity-policy rows (keyed fingerprints only; no raw principal GUIDs or assignment scopes) |
| `resource_change_freshness` | Optional sanitized Azure Resource Graph change rows (no raw provider targets or actor ids) |
| `attributes` | Optional bounded provider-specific attributes. See below for what each provider surfaces. |

The `attributes` field is present only when the provider source fact carried
attribute evidence the route is allowed to surface, and its contract differs
by provider:

- **GCP** surfaces its typed-depth payload as a bounded redaction-safe
  passthrough (e.g. `table_type`, `schema_field_count`, `kms_key_name`,
  `clustering_fields` for BigQuery tables; `routing_mode`,
  `auto_create_subnetworks`, `mtu`, `subnetwork_count` for VPC networks).
  Values are redaction-safe scalars and string-arrays.
- **AWS** surfaces a CLOSED image/version allowlist only, scoped to the
  strongest deployed-code signals the collector already observes:
  `task_definition_arn`, `image_uri`, `resolved_image_uri`, `code_sha256`,
  `version`, and a `containers` array (from ECS running tasks) reduced per
  element to `{image, image_digest}`. Every other AWS attribute key (for
  example `cluster_arn`, `role_arn`, `kms_key_arn`, `network_interfaces`,
  `environment`, `vpc_config`, or a container's `name`/`runtime_id`) is
  dropped before the route ever sees it.
- **Azure** uses the same closed-allowlist mechanism as AWS, but the
  allowlist is currently empty: the `azure_cloud_resource` fact this route
  reads carries no image or version key today (Azure's runtime image
  evidence is emitted as a separate `azure_image_reference` fact kind not
  yet wired into this admission path), so no Azure resource surfaces an
  `attributes` field yet. Every raw Azure attribute (`arm_resource_id`,
  `subscription_id`, `resource_group`, `tenant_id`, `tags`, the redacted
  `extension` object, ...) is dropped.

`attributes` surfaces deployed-code identity evidence — image references
(`image_uri`, `resolved_image_uri`, the ECS `containers[].image`) and the
owning `task_definition_arn` — which necessarily name the image, registry, and
repository for the caller's own resources. This route is account-scoped (it is
filtered by `scope_id`/`account_id`), so those identifiers are the operator's
own, not another tenant's. What is never present is any credential, secret, or
non-image infrastructure locator: `cluster_arn`, `role_arn`, `kms_key_arn`,
`network_interfaces`, `environment`, `vpc_config`, a container's
`name`/`runtime_id`, or the Azure `arm_resource_id`/`subscription_id`/`tags`
bag are all dropped before the route ever sees them.

## Cloud Resource Graph Paging

`GET /api/v0/cloud/resources` returns a bounded browse page from the authoritative CloudResource graph. Optional `provider`, `resource_type`, `region`, and
`account_id` filters are applied before paging. Continue a truncated page by
passing both `next_cursor.after_resource_type` and `next_cursor.after_id`;
sending only one cursor field returns HTTP 400.

The route first selects a current, authorized `limit+1` identity page from the
Postgres graph-owner ledger, then hydrates only the returned `uid` values from
the graph. Scoped-token grants are evaluated before the page limit. An empty
grant returns an empty page without reading either backend, and graph/ledger
disagreement fails closed rather than serving partial data.

The response remains ordered by `resource_type`, then `id`, and includes
`resources`, `count`, `limit`, `truncated`, the applied `scope`, and `next_cursor`
only when another page exists. `local_lightweight` returns `unsupported_capability`.

## Related References

- [Truth Label Protocol](truth-label-protocol.md)
- [Capability Conformance Spec](capability-conformance-spec.md)
- [Runtime Admin API](runtime-admin-api.md)
- [Local Testing](local-testing.md)

## Answer-narration status seam — hot-path evidence (issue #3263 follow-up)

`StatusHandler.NarrationPosture` is an optional `func() status.AnswerNarrationStatus`
field that wires `GET /api/v0/status/answer-narration` to the in-memory
governance-resolved posture from the `POST /api/v0/ask` narration path.

No-Regression Evidence: when `NarrationPosture` is nil (the default for all
existing callers) the handler is byte-for-byte unchanged — no branch is taken
and no extra work is performed. When set, the field calls a bounded in-memory
`governance.ResolvePosture` value and issues NO database query, graph read,
Cypher statement, worker claim, lease, or queue operation (strictly cheaper than
the prior path). No Cypher, graph write, worker/lease/queue, concurrency knob,
or batching change. Verified: `go test ./internal/query ./cmd/api -count=1`
green.

No-Observability-Change: no new metric, span, log line, audit table, schema
column, or status field is introduced. The answer-narration status response
shape is unchanged; the existing redacted fields now carry real governed values
when the posture func is wired.
