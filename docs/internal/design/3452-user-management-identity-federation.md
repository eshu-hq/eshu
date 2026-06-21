# User Management, Identity Federation, And Permissioned Dashboard Access (#3452)

Status: **PROPOSED - SECURITY AND PRODUCT REVIEW REQUIRED BEFORE RUNTIME ENFORCEMENT.**

Refs #3452. Refs #1852, #1900, #1902, #1903, #2047, #2048, #2049,
#2050, #2056, #2062, #2110, #2122, #2124, #2158, #2159, #2178.
See also [Hosted Governance Policy Model](1900-hosted-governance-policy-model.md),
[Tenant And Workspace Isolation](1902-tenant-workspace-isolation.md),
[Hosted Governance Posture](../../public/operate/hosted-governance.md),
[Hosted Security Posture](../../public/operate/hosted-security-posture.md),
and [Eshu Console](../../../apps/console/README.md).

This is a maintainer design gate. It defines the user-management, identity,
session, token, and authorization contract for the Eshu dashboard, API, MCP,
CLI, and automation surfaces. It does not add runtime enforcement, storage
schema, API routes, MCP tools, dashboard pages, Helm values, or deployment
defaults by itself.

## Context

Eshu currently has three relevant foundations:

- API and MCP can authenticate with a shared bearer token.
- Hosted governance added scoped per-team bearer tokens that resolve to
  `query.AuthContext` with tenant, workspace, repository, and ingestion-scope
  grants.
- `apps/console` is the private end-user dashboard. It can point at a live API,
  but production auth is not implemented. Its current API key handling keeps
  bearer keys in memory only and intentionally avoids localStorage because
  bearer tokens in browser storage are easy to exfiltrate.

Those foundations are not enough for production user management. Eshu needs
human login, user profiles, generated personal tokens, service-principal
tokens, dashboard sessions, permissioned navigation, backend-enforced
authorization, audit, revocation, and provider integration for customers with
or without an enterprise IdP.

## Decision

Eshu will own authorization and session state. External identity providers
identify users; they do not become the Eshu permission engine.

V1 ships all three human identity modes:

| Mode | Required for v1 | Purpose |
| --- | --- | --- |
| `local_identity` | Yes | Production-supported users for self-hosted, labs, small companies, and no-IdP customers. |
| `external_oidc` | Yes | Generic OpenID Connect login for Okta, Cognito, Auth0, Entra, Keycloak, and similar providers. |
| `external_saml` | Yes | Enterprise SAML SSO for customers whose workforce identity is SAML-first. |

Okta developer testing is the first live proof target for both OIDC and SAML.
Cognito, Auth0, and similar brokers are supported as optional IdPs or IdP
brokers, but they are not required Eshu infrastructure. Self-hosted Eshu must
not require AWS Cognito just to log in.

GitHub has two different roles:

- GitHub Actions OIDC is workload identity for automation and cloud access.
- GitHub human login is OAuth or GitHub App user authorization and is not the
  same as Actions OIDC.

GitHub human login can be added later as another external identity provider
without changing the authorization model.

## Ownership Boundaries

| Owner | Responsibility | Must not do |
| --- | --- | --- |
| Identity providers | Authenticate external users and return stable subject, email, and group claims. | Encode Eshu route-level permissions or data-class decisions. |
| Eshu identity service | Own local users, provider configs, external identity links, sessions, tenant memberships, roles, grants, tokens, and audit. | Store raw provider secrets, raw assertions, raw tokens, or credentials in logs, docs, status, or audit. |
| API and MCP | Authoritatively enforce tenant, workspace, role, grant, action, data-class, token, and session decisions. | Trust dashboard hiding or client-provided capabilities as security. |
| Dashboard | Provide login, profile, token, tenant switcher, and permission-aware UX. | Become the authorization boundary or persist API tokens in browser storage. |
| Reducer and projection | Preserve facts-first graph and read-model truth. | Rewrite graph truth differently per caller or infer authorization from names. |
| Operators | Configure bootstrap, break-glass, IdP metadata, secrets, and proof gates. | Put private endpoints, tokens, SAML assertions, claims, or provider secrets in public docs or issues. |

## Authentication And Session Model

Dashboard login completes on the backend and creates a server-managed browser
session. The browser receives an `HttpOnly`, `Secure`, `SameSite` cookie. The
session store keeps only a session-id hash plus subject, active tenant,
workspace, provider, MFA state, expiry, revocation state, and policy revision.

Dashboard sessions are separate from API tokens. API tokens are explicit
credentials for CLI, MCP, API clients, service principals, integrations, and
automation.

Local identity is production-supported:

- bootstrap creates the first owner/admin through an operator-controlled flow;
- bootstrap owner receives admin plus all sensitive-data grants by default;
- admin accounts require MFA;
- non-admin MFA is optional in v1;
- invitation or explicit assignment is required by default;
- broad open self-signup is not supported for protected tenants;
- break-glass recovery is operator-enabled, time-boxed, audited, and disabled
  by default.

OIDC uses backend Authorization Code flow. Implementations must validate issuer
metadata, redirect URI, state, nonce, token signature, token audience, expiry,
and mapped claims. Group claims are input to Eshu role mapping, not raw
permissions.

SAML uses Eshu as the service provider. Implementations must validate IdP
metadata, issuer, audience, assertion signatures, certificate state, ACS URL,
NameID, timestamps, replay protection, and mapped group attributes. Metadata
and certificate refresh failure must fail closed for new logins without
invalidating already-valid sessions unless policy requires it.

## Authorization Model

V1 uses built-in product roles backed by fine-grained capability grants. A
custom policy language is intentionally deferred, but the data model must leave
room to add one later.

Every non-public request resolves:

- authenticated subject;
- subject class: local user, external user, personal token, service principal,
  workload identity, scoped token, bootstrap, or break-glass;
- active tenant and workspace;
- product roles;
- grants by tenant, workspace, repository, source scope, action, feature, and
  data class;
- policy revision;
- revocation and expiry state.

A single identity may belong to multiple tenants, but each browser session and
API/MCP request has exactly one active tenant/workspace context. Cross-tenant
blended sessions are out of scope for v1.

Sensitive-data visibility is separate from admin power. A tenant admin may
manage users and provider settings without seeing source content, secret-risk
evidence, cloud topology, security findings, or audit details unless those data
classes are granted. The bootstrap owner starts with both admin and sensitive
grants so first setup is possible, then can delegate or remove grants.

## V1 Permission Inventory

The grant catalog must cover at least these product capabilities:

| Capability family | Examples |
| --- | --- |
| Identity and tenant admin | manage users, invites, memberships, provider configs, MFA reset, sessions, tenant switch, workspace settings |
| Roles and grants | view grants, assign built-in roles, grant data classes, revoke grants, inspect effective permissions |
| Tokens | create personal tokens, create service principals, rotate tokens, revoke tokens, view token metadata |
| Repository and content | list repositories, resolve repository, read source tree, read file content, search content, view code graph |
| Service and runtime | service context, workload context, deployment story, runtime topology, incidents, freshness |
| Cloud and IaC | cloud inventory, IaC resources, unmanaged resources, drift findings, import candidates |
| Supply chain and CI/CD | SBOM, image identities, vulnerabilities, impact findings, security alert reconciliation, CI/CD correlations |
| Secrets and IAM | identity trust chains, secret access paths, privilege posture, posture gaps |
| Docs and semantic evidence | documentation facts, documentation findings, semantic code hints, semantic documentation observations |
| Ask and search | run Ask Eshu, run semantic search, view cited evidence, view reasoning trace when allowed |
| Operations and status | index status, ingester status, collector readiness, capability matrix, governance status |
| Audit and export | view audit, export data, inspect denied decisions, run incident/audit reports |
| Admin and recovery | backfill, reindex, replay, dead-letter operations, bootstrap, break-glass |

Grant decisions are the intersection of tenant/workspace scope, repository or
source-scope scope, action, feature, data class, subject status, and policy
revision. Route names may be an implementation detail, but every route and MCP
tool must map to explicit capabilities.

## Ask Eshu And Search

Ask Eshu, semantic search, code search, and synthesized answers require two
checks:

1. The caller can run the feature.
2. The caller can read every underlying source, repository, action, and data
   class used in retrieved evidence, citations, summaries, and narration.

Unauthorized evidence must be filtered before retrieval, counts, truncation,
citation construction, reasoning traces, or final answer generation. A caller
must never learn that hidden evidence exists through counts, timing-class text,
partial citations, or answer wording.

## Token Model

V1 supports both personal tokens and service-principal tokens.

Personal tokens are generated from a user's profile and cannot exceed the
issuing user's effective grants. They are revoked when the user is disabled,
removed from the tenant, or the token is revoked.

Service-principal tokens are independent subjects for integrations and
automation. They have their own owner, grants, expiry, rotation, status,
last-used metadata, and audit. They survive employee offboarding only when
another authorized owner remains.

All tokens are shown once, stored hash-only, and never logged. Existing shared
token behavior is retained only for local development, bootstrap, and
break-glass compatibility while the new model is rolled out.

## Audit, Revocation, And Telemetry

Eshu-owned sessions and tokens revoke immediately. External IdP group changes
refresh within a bounded, configurable window. The exact default maximum
staleness window must be set during security review before enforcement ships.

Durable audit covers:

- login, logout, MFA, password and recovery-code changes;
- session creation, revocation, timeout, and tenant switches;
- provider configuration and metadata changes;
- user, invite, membership, role, grant, and data-class changes;
- personal-token and service-principal lifecycle;
- denied access;
- bootstrap and break-glass;
- Ask Eshu/search runs;
- sensitive-data access;
- exports and audit reads.

Ordinary non-sensitive reads stay in metrics, spans, and structured logs unless
they touch sensitive data classes or explicit export/report paths.

## Persistence Surfaces

Implementation should add or extend durable Postgres state for:

- users and status;
- external identities and provider links;
- provider configuration revisions;
- local credential hashes;
- MFA factors and recovery codes;
- invitations and assignments;
- tenant memberships;
- roles and grants;
- sessions;
- personal tokens;
- service principals and service-principal tokens;
- audit events.

Existing tenant/workspace grant stores and scoped-token tables should be reused
or migrated rather than replaced. Additive migration is required so current
shared-token and scoped-token behavior stays unchanged until the new enforcement
mode is explicitly enabled.

## Edge Cases

Implementation issues must cover:

- no IdP available;
- first setup with no local user;
- all admins locked out;
- disabled user with active sessions and tokens;
- admin without MFA;
- stale external groups;
- IdP outage;
- OIDC issuer or JWKS rotation;
- SAML certificate rotation and expired metadata;
- SAML assertion replay;
- duplicate external subject across providers;
- recycled email address;
- user belongs to multiple tenants;
- tenant switch while requests are in flight;
- service-principal owner offboarding;
- token theft and immediate revocation;
- route not mapped to a capability;
- Ask/search retrieves mixed authorized and unauthorized evidence;
- dashboard hides a control but direct API/MCP call is attempted;
- audit sink unavailable during admin action.

## Required Proof

Each implementation slice must prove:

- local identity login, MFA, bootstrap, and break-glass behavior;
- Okta OIDC login, group mapping, revocation refresh, and denied access;
- Okta SAML login, group mapping, revocation refresh, and denied access;
- backend session cookie attributes and revocation;
- personal-token and service-principal token revocation;
- API and MCP parity for the same subject and grant set;
- dashboard capability UX matches backend capabilities but is not trusted;
- Ask/search evidence filtering cannot leak denied sources;
- sensitive data class gates block raw content, cloud, IAM, secret-risk,
  supply-chain, semantic, audit, and export surfaces;
- negative leakage in logs, metrics, status, audit, API/MCP bodies, and console
  surfaces.

## Child Issue Mapping

| Slice | Issue |
| --- | --- |
| ADR and permission inventory | #3453 |
| Identity schema and provider config model | #3454 |
| Local identity, bootstrap, MFA, and break-glass | #3455 |
| Dashboard sessions and CSRF-safe auth flow | #3456 |
| Generic OIDC and Okta OIDC proof | #3457 |
| SAML SSO and Okta SAML proof | #3458 |
| Role, grant, and data-class capability catalog | #3459 |
| API/MCP/Ask/search authorization propagation | #3460 |
| Personal and service-principal tokens | #3461 |
| Console login, profile, token, tenant, and admin UX | #3462 |
| Audit, revocation, and negative-leakage proof | #3463 |
| Operator docs and proof gates | #3464 |

## Evidence For This PR

No-Regression Evidence: docs-only design gate; no Go, TypeScript, schema,
OpenAPI, MCP, Helm, Compose, queue, storage, graph, runtime-default, or
dashboard runtime files are changed.

No-Observability-Change: docs-only design gate; it defines future telemetry and
audit requirements but emits no metrics, spans, logs, status fields, or pprof
signals.

Source check date: 2026-06-21.

Sources used:

- `AGENTS.md`
- `docs/internal/agent-guide.md`
- `docs/internal/design/1900-hosted-governance-policy-model.md`
- `docs/internal/design/1902-tenant-workspace-isolation.md`
- `docs/public/operate/hosted-governance.md`
- `docs/public/operate/hosted-security-posture.md`
- `apps/console/README.md`
- `apps/console/src/config/environment.ts`
- `apps/console/src/api/client.ts`
- `go/internal/query/auth.go`
- `go/internal/query/auth_scoped_routes.go`
- `go/internal/scopedtoken/README.md`
- OpenID Connect Core 1.0, `https://openid.net/specs/openid-connect-core-1_0.html`
- Okta redirect authentication, `https://developer.okta.com/docs/guides/sign-into-web-app-redirect/main/`
- Okta SAML concepts, `https://developer.okta.com/docs/concepts/saml/`
- Amazon Cognito user-pool identity federation, `https://docs.aws.amazon.com/cognito/latest/developerguide/cognito-user-pools-identity-federation.html`
- GitHub Actions OIDC, `https://docs.github.com/en/actions/concepts/security/openid-connect`
- GitHub OAuth app authorization, `https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps`
- OWASP Session Management Cheat Sheet, `https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html`
- NIST SP 800-63B, `https://pages.nist.gov/800-63-4/sp800-63b.html`
- OASIS SAML metadata interoperability profile, `https://docs.oasis-open.org/security/saml/Post2.0/sstc-metadata-iop.html`
