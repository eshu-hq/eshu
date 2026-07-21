# AGENTS.md — cmd/mock-oidc-idp guidance for LLM assistants

## Read first

1. `go/cmd/mock-oidc-idp/README.md` — purpose, configuration, invariants
2. `go/cmd/mock-oidc-idp/server.go` — `Server`, the four HTTP handlers, the
   one-time code store
3. `go/internal/oidclogin/connector.go` — `NewOIDCConnector`, the production
   client this mock IdP must stay compatible with
4. `go/internal/oidclogin/db_provider_config.go` — the DB-backed
   `ProviderConfig` shape (issuer, client_id, group_claim, redirect_url) a
   compose-deployed provider config pointed at this mock IdP must satisfy

## Invariants this package enforces

- **Single configured identity, no login UI** — `handleAuthorize` never
  prompts; it redirects to `redirect_uri` immediately with a fresh code.
  Any change that adds a credential form or a client registry violates the
  "mock, not a real IdP" contract this binary exists for.
- **Static signing key** — `staticPrivateKeyPEM` in `keys.go` is fixed and
  committed. Do not regenerate it per build or per process start: JWKS
  output and the derived `kid` must stay stable across container restarts so
  a captured token or cassette stays reproducible. It is not a secret (see
  the doc comment on `staticPrivateKeyPEM`) and must never be treated as
  one or reused outside this binary.
- **Issuer/discovery/token agreement** — `s.issuer` (from
  `MOCK_OIDC_ISSUER_URL`) must appear identically in the discovery
  document's `issuer` field, the ID token's `iss` claim, and the URL the
  OIDC client used for discovery. `coreos/go-oidc` rejects a mismatch.
  Enforced at `server.go` (`handleDiscovery`, `signIDToken`).
- **One-time codes** — `issueCode`/`consumeCode` in `server.go` delete a
  code on first redemption. Do not make codes reusable; that would let a
  test replay a stale authorization code, hiding a real client bug.
- **No OTEL bootstrap** — matches `go/cmd/admin-status`'s precedent: a
  synthetic test fixture with no database does not need the runtime
  telemetry framework. Do not add `internal/telemetry` wiring here.
- **`access_token` stays the fixed opaque string by default** — `mintAccessToken`
  (`server.go`) only mints a JWT when the flow carries an RFC 8707 `resource`
  parameter or `MOCK_OIDC_ACCESS_TOKEN_JWT=true` is set. Do not make JWT
  minting unconditional: the #4971 browser-auth suite depends on
  `access_token` staying byte-stable (`"mock-access-token"`) since it never
  reads that field.

## Common changes and how to scope them

- **Add a second synthetic identity/persona** → today `ServerConfig.Identity`
  is one value read once at startup (`configFromEnv` in `main.go`). Adding a
  second persona means either a second `MOCK_OIDC_*` env-configured
  container (matching how the existing Compose pattern runs one identity per
  container) or extending `/authorize` to select by a request parameter —
  the latter changes the "no login UI, one process one identity" invariant
  above, so confirm with the phase 2 browser-runner design before doing it.
- **Support PKCE** → add `code_challenge`/`code_challenge_method` handling
  to `handleAuthorize` (store on `authorizeRequest`) and verification to
  `handleToken`; only add this if the phase 2 browser client actually sends
  a PKCE challenge — `oauth2.Config.Exchange` does not by default.
- **Change the group claim shape** → `MOCK_OIDC_GROUP_CLAIM` already
  controls the claim name; the claim value is always a JSON string array
  (`signIDToken` in `server.go`). If a consumer needs a comma-joined string
  instead, check `stringSliceClaim` in `go/internal/oidclogin/connector.go`
  first — it already accepts both shapes, so a shape change here is likely
  unnecessary.

## Failure modes and how to debug

- Symptom: `MOCK_OIDC_ISSUER_URL is required` at startup → cause: the env
  var is unset → set it to the exact URL other containers/the browser
  reach this service at (the compose service hostname:port, not
  `localhost`, when called from inside the compose network).
- Symptom: `oidc: issuer did not match` from a client → cause: discovery
  was fetched from one URL but `MOCK_OIDC_ISSUER_URL` is set to a
  different one → they must be byte-identical; check for a trailing slash
  or a scheme mismatch.
- Symptom: `unknown or already-used code` from `/token` → cause: either the
  code was already redeemed once, or the process restarted and the
  in-memory `codes` map was cleared → codes do not survive a restart by
  design.
- Symptom: `redirect_uri mismatch` from `/token` → cause: the `redirect_uri`
  posted to `/token` differs from the one used at `/authorize` → this is
  intentional RFC 6749 behavior, not a bug in this mock.

## Anti-patterns specific to this package

- **Persisting codes or sessions to Postgres** — this binary is
  deliberately in-memory and stateless across restarts; it is not a
  production IdP and should never grow a storage dependency.
- **Validating `client_secret`** — there is no client registry; checking
  the secret value would only create false confidence without a real
  registry backing it.
- **Adding a real login form** — defeats the point of a scripted,
  deterministic E2E fixture.

## What NOT to change without discussion

- The four endpoint paths (`/.well-known/openid-configuration`,
  `/authorize`, `/token`, `/jwks`) — a compose-configured DB-backed
  `ProviderConfig.IssuerURL` for this IdP assumes standard OIDC discovery
  at that fixed path relative to the issuer.
- The `staticPrivateKeyPEM` value — see the invariant above.
