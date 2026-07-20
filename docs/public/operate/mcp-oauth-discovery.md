# MCP OAuth 2.1 Discovery

Eshu's MCP server publishes an [RFC 9728](https://www.rfc-editor.org/rfc/rfc9728.html)
OAuth 2.0 Protected Resource Metadata document so an OAuth-capable MCP client
(Claude Code, claude.ai) can discover where to obtain an access token for the
server without hand-configuration (issue #5163). This page covers enabling the
discovery document and pre-registering an OAuth client with your identity
provider. See [MCP Client Authentication](mcp-client-auth.md) for how
`eshu mcp setup` detects this document automatically and for the per-client
snippet shapes it emits.

## What the server publishes

When enabled, `cmd/mcp-server` serves the metadata document at
`/.well-known/oauth-protected-resource` (and, when the resource identifier
carries a path such as `/mcp`, at the RFC 9728 section 3 path-suffixed URL
`/.well-known/oauth-protected-resource/mcp`). The route is **unauthenticated** —
an anonymous client must be able to read it to learn where to authenticate.

The document reports:

- `resource` — the canonical resource identifier, verbatim from
  `ESHU_AUTH_RESOURCE_URI`. This is the exact value an access token's `aud`
  claim must carry to be accepted.
- `authorization_servers` — the issuer URL(s) currently enabled for bearer-token
  validation. Sourced from the live bearer-resolver snapshot, so the document
  never names an issuer the deployment could not actually validate a token
  against.
- `bearer_methods_supported` — always `["header"]`; Eshu reads a token only from
  `Authorization: Bearer`.
- `scopes_supported` — `openid profile email groups`. The `groups` scope is
  load-bearing, not decorative: the resolver denies a token that resolves to no
  group-derived grant, so a client must request `groups` to obtain a usable
  token.
- `resource_name` — `Eshu MCP Server`.
- `resource_documentation` — optional, from `ESHU_AUTH_RESOURCE_DOCUMENTATION`.
- `eshu_preregistered_client_id` — optional extension member, from
  `ESHU_AUTH_PREREGISTERED_CLIENT_ID` (see below).

A `401` from a credentialed MCP route (`POST /mcp/message`, `GET /sse`, `/api/`)
adds a [RFC 9728 section 5.1](https://www.rfc-editor.org/rfc/rfc9728.html#section-5.1)
`WWW-Authenticate: Bearer resource_metadata="…", scope="…"` challenge pointing
the client at this document — but only for a credential-less or unrecognized
request. A request bearing a valid token is served normally with no challenge,
and a request bearing a recognized-but-rejected token (expired, wrong audience,
bad signature) gets the bare `Bearer` challenge.

## Enabling discovery

Discovery is derived from configuration you already set for bearer-token
validation — there is no separate on/off switch:

1. Set `ESHU_AUTH_RESOURCE_URI` to your canonical resource identifier (for
   example `https://eshu.example.com/mcp`). It must be an `https` URL — or an
   `http` loopback URL for local development — with no query string or fragment.
2. Configure at least one OIDC bearer provider (see
   [Environment Variable Registry](../reference/env-registry.md) for
   `ESHU_AUTH_OIDC_CONFIG_FILE` and the DB-backed provider path).

The document answers `404` — indistinguishable from a token-only deployment —
whenever `ESHU_AUTH_RESOURCE_URI` is unset or invalid, no provider is
configured, or no bearer issuer is currently active.

## Pre-registering an OAuth client (Okta)

An Okta custom authorization server offers no anonymous dynamic client
registration, so MCP clients need a pre-registered client to start an
Authorization Code + PKCE flow. Create a **native / public** OIDC application
(no client secret) and configure:

- **Grant type:** Authorization Code with PKCE.
- **Redirect URIs:** the loopback callback Claude Code uses
  (`http://localhost:<port>/callback`, with the port the client picks per run)
  and the claude.ai callback (`https://claude.ai/api/mcp/auth_callback`).
- **Scopes:** `openid`, `profile`, `email`, and `groups` — the `groups` scope
  and a matching groups claim are required for the resolver to map the token to
  Eshu roles.
- **Audience:** the access token's `aud` must equal your
  `ESHU_AUTH_RESOURCE_URI`. Configure the Okta authorization server / resource
  indicator accordingly.

Then set `ESHU_AUTH_PREREGISTERED_CLIENT_ID` to the application's client_id. The
server advertises it in the metadata document's `eshu_preregistered_client_id`
field; a client that cannot self-register copies it into its own client
configuration.

A broker that performs dynamic client registration on the client's behalf is an
alternative to pre-registration, but Eshu does not ship one.
