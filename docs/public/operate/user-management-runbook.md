# User Management Runbook

Use this runbook when preparing or operating Eshu user management, identity
federation, dashboard sessions, API/MCP tokens, roles, grants, audit, and
revocation. It turns the user-management ADR into operator-safe setup and proof
steps without publishing provider secrets, private tenant names, token values,
claims, assertions, or raw audit bodies.

The current implementation is intentionally staged. Scoped API/MCP tokens,
server-managed dashboard browser sessions, dormant identity schema, and
aggregate hosted governance audit proof exist today. Production local identity,
generic OIDC, SAML SSO, personal-token lifecycle, service-principal token
lifecycle, and the full console UX are tracked in the open user-management child
issues. Do not describe those future lanes as enforcing until their
implementation issues land.

## Public-Safe Boundary

Never put these values in docs, issues, PR text, committed values files,
onboarding artifacts, logs, metrics, status payloads, or proof summaries:

- raw API tokens, session cookies, CSRF secrets, recovery codes, provider keys,
  client secrets, private keys, signed URLs, or cloud credentials;
- private hostnames, tenant domains, tenant names, workspace names, repository
  names, local file paths, source payloads, or machine-specific paths;
- raw OIDC tokens, claims, JWKS payloads, SAML assertions, SAML attributes,
  provider responses, prompts, or failure payloads;
- direct personal identifiers, private contact details, raw email addresses, or
  raw external subjects.

Use public-safe placeholders such as `https://eshu.example.com`,
`https://idp.example.com`, `<issuer-url>`, `<client-id>`,
`<metadata-url>`, `sha256:<digest>`, aggregate counts, reason codes, event
families, role names, grant families, and data-class names.

## Current Boundary

| Surface | Current operator posture | Follow-up |
| --- | --- | --- |
| Shared API/MCP bearer token | Still available for local development, bootstrap, and compatibility. It is not a tenant boundary. | Move teams to scoped or identity-backed tokens before claiming isolation; see [MCP Client Authentication](mcp-client-auth.md#shared-key). |
| Generated personal/service-principal tokens | API and MCP resolve hash-only `identity_token_metadata` rows through active identity subjects, role assignments, and repository/source-scope targets. All-scope admins can create, revoke, and rotate generated tokens through the local identity admin API; raw bearer values are returned once and only hashes are stored. Successful lookups update last-used metadata. | Console UX continues through the user-management console work. |
| MCP client onboarding | `eshu mcp setup` is posture-aware (issue #5169, F-8): it detects per-user token vs. SSO and emits the matching client snippet, never a raw secret. | See [MCP Client Authentication](mcp-client-auth.md) for the posture model and per-org walkthroughs. |
| Scoped API/MCP tokens | `ESHU_SCOPED_TOKENS_FILE` maps bearer-token hashes to tenant, workspace, repository, and source-scope grants. The registry is hash-only, optional, and tried after generated identity tokens. | Keep for bootstrap, migration, and operator-managed team tokens. |
| Browser sessions | `POST /api/v0/auth/browser-session` exchanges an explicit API credential for `__Host-eshu_session`; unsafe cookie-authenticated requests require `X-Eshu-CSRF`. | Full login/profile/admin console UX is tracked by #3462. |
| Identity schema | Additive and dormant tables model users, provider configs, MFA handles, memberships, roles, grants, sessions, service principals, and token metadata with opaque IDs, hashes, and credential handles. | Local identity, OIDC, and SAML enforcement are tracked by #3455, #3457, and #3458. |
| Roles and grants | The v1 authorization catalog defines product roles, permission families, data classes, and bootstrap-owner posture. | API/MCP/Ask/search propagation is tracked by #3460. |
| Audit and revocation | Hosted governance audit stores validation-safe fields, reports aggregate status only, and has an auth-audit/revocation proof summary gate. | Detailed user-management event writers expand through #3463 and later enforcement slices. |

## Identity Modes

### Local Identity

Local identity is the production-supported no-IdP mode once #3455 lands. Treat
it as a real identity provider owned by Eshu, not a demo fallback.

Operator rules:

1. Bootstrap the first owner/admin through the generated-credential and
   first-run setup wizard flow (issues #4963, #4965): retrieve the one-time
   admin credential from the API startup log banner or
   `eshu admin initial-credential`, then complete the Console's Claim ->
   Create admin -> Secure wizard, which enrolls MFA recovery codes and signs
   the admin in. See
   [Console First Five Minutes](../getting-started/console-first-five-minutes.md)
   for the full walkthrough. `ESHU_AUTH_BOOTSTRAP_MODE` controls this: the
   default `generated` mode requires a configured data-encryption key
   (`ESHU_AUTH_SECRET_ENC_KEY`); `sso-only` and `disabled` skip local admin
   seeding entirely. An operator may instead seed a specific admin identity
   with `ESHU_ADMIN_USERNAME`/`ESHU_ADMIN_PASSWORD` before first boot, which
   still requires the wizard's Secure step to enroll MFA. If the one-time
   credential is lost, expired under a rotated `ESHU_AUTH_SECRET_ENC_KEY`, or
   already consumed, `eshu admin reset-initial-credential` atomically rotates
   the password AND re-enrolls the MFA recovery-code factor (issue #5602), so
   the printed recovery code authenticates; it never touches a TOTP factor the
   admin enrolled after bootstrap.
2. Require MFA for admin accounts before protected tenants become active.
3. Keep non-admin MFA policy explicit; do not imply it is mandatory unless the
   tenant policy says so.
4. Store credential hashes, MFA factor handles, and recovery-code hashes only.
5. Keep broad self-signup disabled for protected tenants.
6. Keep break-glass disabled by default, time-boxed when enabled, and audited.

Public proof should report only setup state, MFA-required status, bootstrap
event counts, break-glass event counts, and pass/fail outcomes. It must not
include usernames, emails, recovery codes, password reset links, or session
values.

### Generic OIDC

OIDC uses backend Authorization Code flow. Eshu validates the issuer metadata,
redirect URI, state, nonce, token signature, token audience, expiry, and mapped
claims. IdP groups map into Eshu roles; Eshu grants remain the authorization
source of truth.

Safe configuration record:

```yaml
provider_kind: external_oidc
issuer: <issuer-url>
client_id: <client-id>
client_secret_source: kubernetes_secret
redirect_uri: https://eshu.example.com/auth/oidc/callback
scopes:
  - openid
  - profile
  - email
group_claim: groups
role_mapping_revision: sha256:<digest>
```

Operator checks:

1. Confirm the issuer discovery document and JWKS are reachable from the API
   runtime through the intended egress path.
2. Confirm the redirect URI exactly matches the provider app integration.
3. Confirm state and nonce validation are enabled.
4. Confirm group claims map to built-in Eshu roles and do not encode raw
   route-level permissions.
5. Revoke or remove a group membership and prove Eshu refreshes within the
   security-reviewed bounded window.

Public proof may list provider kind, credential-source class, redirect path,
group-claim name, mapped role names, aggregate login counts, denied-access
counts, and the refresh window. Keep ID tokens, access tokens, claims, issuer
tenant domains, JWKS bodies, and provider responses private.

### SAML SSO

SAML uses Eshu as the service provider. Eshu validates IdP metadata, issuer,
audience, assertion signatures, certificate state, ACS URL, NameID, timestamps,
replay protection, and mapped group attributes. Metadata or certificate refresh
failure must fail closed for new logins.

Safe configuration record:

```yaml
provider_kind: external_saml
idp_metadata_source: secret_or_private_url
sp_entity_id: https://eshu.example.com/auth/saml/metadata
acs_url: https://eshu.example.com/auth/saml/acs
name_id_policy: persistent
group_attribute: groups
role_mapping_revision: sha256:<digest>
```

Operator checks:

1. Confirm the SP entity ID and ACS URL exactly match the IdP app integration.
2. Confirm IdP metadata and signing certificates come from an operator-managed
   private source.
3. Confirm signed assertions are required and replay protection is enabled.
4. Confirm NameID and group attributes map to stable opaque identity links and
   Eshu roles.
5. Rotate or expire metadata in a private test environment and prove new logins
   fail closed while existing valid sessions follow the configured revocation
   policy.

Public proof may list provider kind, metadata-source class, ACS path,
NameID policy class, mapped role names, aggregate login counts, denied-access
counts, and certificate rotation pass/fail. Keep assertions, attributes,
certificates, metadata bodies, tenant domains, and provider responses private.

### Okta Test Flows

Okta is the first live proof target for both OIDC and SAML. Use a dedicated
Okta test org and separate app integrations for the OIDC web app and SAML 2.0
app. The public runbook can link to Okta's redirect-model web sign-in and SAML
concept docs, but the actual org URL, app IDs, client secrets, metadata URLs,
groups, and test users stay in private operator storage.

Proof checklist:

1. OIDC: create a web app integration, set the Eshu redirect URI, map the group
   claim, sign in through the redirect flow, verify role mapping, revoke or
   remove a group, and prove denied access after the bounded refresh window.
2. SAML: create a SAML app integration, set SP entity ID and ACS URL, map the
   group attribute, sign in, verify role mapping, rotate metadata/certificate in
   test, and prove fail-closed behavior for new logins.
3. MCP: with the OIDC app configured, confirm an unauthenticated MCP request
   gets the 401 discovery challenge, `eshu mcp setup --hosted` auto-detects
   SSO posture, and the OAuth flow completes to a successful tool call. See
   [MCP Client Authentication § Okta org](mcp-client-auth.md#okta-org); the
   scripted end-to-end proof is tracked by F-9 (#5170).
4. For all three: capture only event-family counts, decision counts, role
   names, provider kind, refresh-window class, and pass/fail status.

For OIDC, convert the private Okta run into a public-safe aggregate summary in
the repo-ignored `.proof-artifacts/` directory with:

```bash
mkdir -p .proof-artifacts
scripts/verify-okta-oidc-live-proof.sh \
  --input .proof-artifacts/okta-oidc-proof.json \
  --output-json .proof-artifacts/okta-oidc-proof.summary.json \
  --output-markdown .proof-artifacts/okta-oidc-proof.summary.md
```

The input manifest is operator-local. It may describe only source classes,
public API paths, aggregate login, denied-access, refresh, and revocation
counts, role names, decision families, timing classes, and pass/fail proof
steps. Raw Okta org URLs, app IDs, client secrets, users, group values, OIDC
tokens, cookies, provider responses, and audit bodies must stay private.

For SAML, convert the private Okta run into a public-safe aggregate summary in
the repo-ignored `.proof-artifacts/` directory with:

```bash
mkdir -p .proof-artifacts
scripts/verify-okta-saml-live-proof.sh \
  --input .proof-artifacts/okta-saml-proof.json \
  --output-json .proof-artifacts/okta-saml-proof.summary.json \
  --output-markdown .proof-artifacts/okta-saml-proof.summary.md
```

The input manifest is operator-local. It may describe only source classes,
public API paths, aggregate counts, role names, decision families, timing
classes, and pass/fail proof steps. Raw Okta org URLs, app IDs, metadata XML,
certificates, users, group values, SAML assertions, SAML attributes, cookies,
provider responses, and audit bodies must stay private.

### Optional Brokers

Cognito, Auth0, and similar systems can act as optional IdPs or IdP brokers.
They are not required Eshu infrastructure. When a broker fronts multiple
upstream IdPs, Eshu still owns tenant membership, active workspace context,
roles, grants, token lifecycle, sessions, audit, and revocation.

Broker setup must record:

- whether Eshu receives OIDC tokens or SAML assertions from the broker;
- the stable subject claim or NameID class;
- the group or role attribute class;
- the credential-source class;
- the callback, ACS, or metadata path shape;
- the revocation and group-refresh window.

Public docs must not expose broker tenant URLs, upstream IdP names, raw
assertions, raw claims, or credential handles.

## Tokens And Sessions

API tokens and dashboard sessions are different credentials.

- CLI, MCP, automation, and integrations use explicit API tokens.
- Dashboard browser sessions use server-managed cookies and CSRF proof.
- Personal tokens cannot exceed the issuing user's effective grants.
- Service-principal tokens are independent automation subjects with owners,
  grants, expiry, rotation, status, and last-used metadata.
- Scoped tokens remain operator-issued hash-only registry entries and are
  suitable for bootstrap, migration, and bounded team read access.

Engineers get their MCP credential through `eshu mcp setup` (posture-aware;
issue #5169, F-8), which detects per-user token vs. SSO and prints the
matching client snippet. Personal tokens themselves are issued from the
console's `/profile` page (Profile -> API tokens, issue #5164). See [MCP
Client Authentication](mcp-client-auth.md).

Generated token lifecycle routes:

- `POST /api/v0/auth/local/api-tokens` creates a personal or
  service-principal token for an active subject in the caller's tenant and
  workspace. The response returns `api_token` once; storage receives only the
  SHA-256 hash.
- `POST /api/v0/auth/local/api-tokens/{token_id}/revoke` immediately marks an
  active generated token revoked in the caller's tenant and workspace.
- `POST /api/v0/auth/local/api-tokens/{token_id}/rotate` atomically creates a
  replacement token hash and revokes the old generated token.

These routes require all-scopes admin authentication and emit
`token_lifecycle` audit events. They never accept existing bearer values,
return token hashes, or expose display labels after creation.
Shared-operator callers whose auth context has no tenant/workspace must include
`tenant_id` and `workspace_id` in create, revoke, and rotate request bodies.

For scoped tokens, issue, rotate, and revoke through `ESHU_SCOPED_TOKENS_FILE`:

```json
{
  "version": 1,
  "tokens": [
    {
      "token_sha256": "<lowercase hex sha256 of the bearer token>",
      "tenant_id": "<tenant-id>",
      "workspace_id": "<workspace-id>",
      "subject_class": "team_token",
      "subject_id_hash": "sha256:<digest>",
      "policy_revision_hash": "sha256:<digest>",
      "all_scopes": false,
      "allowed_scope_ids": ["<source-scope-id>"],
      "allowed_repository_ids": ["<repository-id>"]
    }
  ]
}
```

The registry stores token hashes, not token values. Error messages, logs,
metrics, docs, and proof summaries must not include token hashes either unless
the field is explicitly documented as a safe digest.

## Roles, Grants, And Data Classes

Use [Authorization Catalog](../reference/authorization-catalog.md) as the role
and grant vocabulary. External IdP groups can assign Eshu roles, but they do
not become direct route permissions. Sensitive-data visibility stays separate
from tenant administration.

Minimum operator checks:

1. Confirm each IdP group maps to a built-in role or approved role mapping.
2. Confirm `tenant_admin` does not imply sensitive-data reads.
3. Confirm the bootstrap owner starts with admin plus sensitive-data grants and
   can delegate or remove those grants after setup.
4. Confirm every API route and MCP tool used by the workflow maps to a
   permission family before exposing it to scoped users.
5. Confirm Ask, search, citations, and narration filter unauthorized evidence
   before retrieval, counts, truncation, citation construction, or final answer
   generation.

## Bootstrap And Break-Glass

Bootstrap and break-glass are recovery paths, not normal access paths.

Bootstrap proof should show:

- first-owner creation happened once;
- admin MFA requirement was enforced;
- bootstrap owner received admin plus sensitive-data grants;
- bootstrap state moved to closed or complete;
- no raw credential, recovery-code, or session value was exported.

Break-glass proof should show:

- break-glass was disabled by default;
- enablement was time-boxed and operator-authorized;
- actions were audited with event family, actor class, scope class, decision,
  reason code, correlation id, and timestamp;
- revocation returned the system to normal access;
- no raw identity, token, assertion, or provider payload reached public
  artifacts.

## Proof Gates

Run the smallest gate that proves the changed surface, then add broader proof
only when the runtime changed.

| Change | Minimum proof |
| --- | --- |
| This runbook, docs, or nav only | `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml` and `git diff --check`. |
| Hosted governance local posture | `scripts/test-verify-hosted-governance-proof.sh` and `scripts/verify-hosted-governance-proof.sh`. |
| Scoped-token team isolation | `scripts/test-verify-two-team-governance-proof.sh`; live Compose proof uses `scripts/run-two-team-governance-proof.sh` from a private operator environment. |
| Kubernetes scoped-token isolation | `scripts/test-verify-k8s-two-team-governance-proof.sh`; live cluster proof uses `scripts/run-k8s-two-team-governance-proof.sh`. |
| Auth audit and revocation summaries | `scripts/test-verify-hosted-auth-audit-proof.sh` and `scripts/verify-hosted-auth-audit-proof.sh --input auth-audit-proof.json --output-json auth-audit-proof.summary.json --output-markdown auth-audit-proof.summary.md`. |
| Negative leakage across public artifacts | `scripts/test-verify-hosted-governance-negative-leakage-proof.sh` and `scripts/verify-hosted-governance-negative-leakage-proof.sh --manifest leakage-proof.json --output-json leakage-proof.summary.json --output-markdown leakage-proof.summary.md`. |
| Hosted Helm auth, secret refs, pprof, or docs exposure posture | `scripts/test-verify-hosted-security-posture.sh`, `scripts/verify-hosted-security-posture.sh -f values.eshu.yaml`, and `helm lint deploy/helm/eshu -f values.eshu.yaml`. |
| OIDC implementation or Okta OIDC proof | Focused implementation tests from #3457, `scripts/test-verify-okta-oidc-live-proof.sh`, `scripts/verify-okta-oidc-live-proof.sh --input .proof-artifacts/okta-oidc-proof.json --output-json .proof-artifacts/okta-oidc-proof.summary.json --output-markdown .proof-artifacts/okta-oidc-proof.summary.md`, auth audit proof, denied-access proof, negative-leakage proof, and docs build. |
| SAML implementation or Okta SAML proof | Focused implementation tests from #3458, `scripts/test-verify-okta-saml-live-proof.sh`, metadata/certificate fail-closed proof, auth audit proof, denied-access proof, negative-leakage proof, and docs build. |
| Token lifecycle implementation | Focused implementation tests from #3461, immediate revocation proof, API/MCP parity proof, auth audit proof, and docs build. |
| API/MCP/Ask/search authorization propagation | Focused implementation tests from #3460, API/MCP parity proof, unauthorized-evidence filtering proof, negative-leakage proof, and docs build. |

The auth-audit proof manifest is operator-local input. It may include only
public-safe event families, aggregate counts, and revocation timing classes.
The generated summaries are the public artifacts.

Required auth-audit event families:

- `api_mcp_authentication`
- `identity_authentication`
- `mfa_lifecycle`
- `session_lifecycle`
- `token_lifecycle`
- `idp_config_change`
- `role_grant_change`
- `read_authorization`
- `tenant_switch`
- `sensitive_data_access`
- `ask_search_run`
- `export`
- `bootstrap`
- `break_glass`
- `audit_read`

## Troubleshooting

### Login Fails

1. Check process health and dependency readiness.
2. Confirm the provider mode is enabled for the tenant.
3. For OIDC, check issuer discovery, redirect URI, state, nonce, audience,
   expiry, and JWKS refresh from private logs or traces.
4. For SAML, check ACS URL, entity ID, issuer, audience, signature,
   certificate state, timestamp, and replay cache from private logs or traces.
5. Put only provider kind, decision, reason code, correlation id, and time
   window in a public ticket.

### Access Is Denied After Login

1. Confirm the active tenant/workspace context.
2. Confirm role and grant mapping through the authorization catalog.
3. Check `/api/v0/status/governance` or MCP `get_hosted_governance_status` for
   aggregate denied decision counts and reason classes.
4. Query private audit only by event type, actor class, scope class, decision,
   reason code, correlation id, and a narrow time window.
5. Do not paste raw subjects, group names, tenant names, repository names,
   source IDs, or response bodies into public tickets.

### Group Revocation Looks Stale

1. Confirm whether the credential is an Eshu-owned session/token or an external
   IdP group mapping.
2. Eshu-owned sessions and tokens must revoke immediately.
3. External group changes follow the configured bounded refresh window.
4. If the window is exceeded, capture only provider kind, refresh-window
   seconds, aggregate event counts, and bounded reason codes in public proof.

### Session Or CSRF Failure

1. Confirm the request is using a browser-session cookie, not an API bearer
   token.
2. Unsafe cookie-authenticated requests must send `X-Eshu-CSRF`.
3. Browser session storage keeps only session and CSRF digests and rejects
   expired, revoked, policy-stale, or CSRF-mismatched sessions.
4. Public tickets may name the route, status code, and reason class only.

### Audit Review

1. Start with `/api/v0/status/governance` or
   `get_hosted_governance_status`.
2. Review aggregate event, denied, unavailable, actor-class, scope-class,
   reason, and ACL-state counts.
3. Use private audit storage for detailed event fields only after confirming
   the operator is authorized for that scope.
4. Keep detailed queries bounded and do not query by raw names, paths, URLs,
   titles, prompts, credential handles, or provider payloads.

## External References

- [OpenID Connect Core 1.0](https://openid.net/specs/openid-connect-core-1_0.html)
- [Okta redirect-model web sign-in](https://developer.okta.com/docs/guides/sign-into-web-app-redirect/main/)
- [Okta SAML concepts](https://developer.okta.com/docs/concepts/saml/)
- [Amazon Cognito user-pool identity federation](https://docs.aws.amazon.com/cognito/latest/developerguide/cognito-user-pools-identity-federation.html)
- [Auth0 OpenID Connect protocol](https://auth0.com/docs/authenticate/protocols/openid-connect-protocol)
- [Auth0 SAML configuration](https://auth0.com/docs/authenticate/protocols/saml/saml-configuration)
- [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
