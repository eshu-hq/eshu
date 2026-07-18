# internal/oidcbearer

IdP bearer-token resolver (issue #5162, epic #5161): validates an
IdP-issued OAuth2 access token presented as `Authorization: Bearer <token>`
and maps it to a `query.AuthContext`, reusing the same grant machinery as
interactive OIDC login.

## Why this exists

Before this package, an access token minted by an external IdP (Okta, or any
generic OIDC issuer) was worthless against the Eshu API/MCP surface: the
bearer path only understood Eshu's own opaque scoped tokens and the shared
API key. The OIDC building blocks already existed (`internal/oidclogin`
builds a go-oidc verifier during interactive login), but nothing verified an
*inbound* bearer token against them.

## Algorithm

`Resolver.ResolveScopedToken` (see `resolver.go`):

1. Trim the credential. If it is not JWT-shaped (exactly three non-empty,
   dot-separated segments — see `isJWTShaped` in `claims.go`), return
   `(zero, false, nil)`: Eshu's own tokens are opaque and never look like
   this, so the caller's resolver chain should keep trying the next
   resolver.
2. Load the current verifier-cache snapshot (`cache.go`), triggering a
   background rebuild if it is stale. If the snapshot has zero enabled
   providers, return `(zero, false, nil)` instantly — no verifier is ever
   built and no JWKS traffic is ever generated for a deployment that has not
   configured a bearer IdP (the zero-provider fast path, AC #4).
3. Base64url-decode the JWT's middle segment and read its `iss` claim
   *without verifying the signature* (`peekUnverifiedIssuer`). This is
   routing only — it selects which enabled provider's real verifier to call
   next. Nothing about this issuer is trusted until step 5 succeeds.
4. Look the issuer up in the snapshot. No match means an IdP bearer token
   was presented for an issuer Eshu does not have a matching enabled
   provider for. From this point on the resolver **owns the verdict**: every
   further failure returns `(zero, false, err)`, not `(zero, false, nil)`,
   so the composite resolver chain stops here and returns 401 rather than
   falling through to a resolver (identity tokens, the file registry, the
   shared key) that could never have understood a JWT anyway.
5. Call the matched provider's `*oidc.IDTokenVerifier.Verify` — real go-oidc
   JWKS signature verification, issuer check, expiry check, and audience
   check (`ClientID` is set to the configured resource URI). A failure is
   classified into a bounded outcome (`expired`, `wrong_audience`,
   `bad_signature`, `jwks_fetch_failure`, or `malformed` — see
   `classifyVerifyError` in `claims.go`, whose classification is derived
   from go-oidc v3's actual error text, not guessed).
6. On success, decode the verified claims, hash the external groups, and
   call the injected `oidclogin.GrantResolver.ResolveGroupGrants` — the same
   composition (`DB-backed resolver falling back to the static env-file
   resolver`) the interactive login handler builds, so a bearer token
   produces identical grants to an interactive login for the same user (AC
   #3). Empty groups or empty resolved roles both deny (`no_grants`).
7. Build the `AuthContext` with `Mode: AuthModeScoped` (deliberate: this
   inherits the F-6 scoped-route allowlist enforced in
   `internal/query/auth.go`; any other mode would bypass it),
   `SubjectClass: "external_oidc_user"`, and
   `SubjectIDHash = SHA256("<provider_config_id>:<sub>")`.

## Verifier cache (`cache.go`)

- Reads are a lock-free `atomic.Pointer[snapshot]` load. A `snapshot` is
  immutable once built: `{builtAt, byIssuer, byProviderConfigID}`.
- A stale snapshot (`now - builtAt > ttl`, default 30s) triggers exactly one
  background rebuild, guarded by a mutex-protected `rebuilding` bool — never
  a second concurrent rebuild, and never a blocked reader.
- A rebuild reuses a provider's prior verifier when its `(IssuerURL,
  RevisionID)` is unchanged; otherwise it calls `VerifierFactory`, which
  talks to the network in production (OIDC discovery + JWKS) but is a
  hermetic `oidc.StaticKeySet`-backed factory in tests.
- The very first snapshot is built **synchronously** inside `NewResolver`,
  not lazily on the first request: this is what makes the zero-provider fast
  path instant from request one, and it keeps the (possibly slow, network
  based) initial JWKS discovery off the request path entirely.
- A provider whose verifier cannot be built (unreachable issuer, JWKS fetch
  failure) is logged, counted (`outcome=jwks_fetch_failure`), and excluded
  from that snapshot — one bad IdP never takes every other enabled IdP down
  with it.

### Why a TTL, not a push notification

`cmd/api` and `cmd/mcp-server` are independent processes with no shared
event bus. A provider CRUD write in one process cannot signal the other
in-process. The TTL is therefore the only honest cross-process consistency
mechanism: both processes converge on the same provider set within one TTL
window of a write, with no false claim of instant propagation.

## Provider sources (`types.go`, `source_env.go`)

`ProviderSource` is the seam between "list of enabled bearer IdPs" and where
they come from. This package only implements the env-file source
(`NewEnvProviderSource`, reading `oidclogin.Config.Providers` — the same
config `ESHU_AUTH_OIDC_CONFIG_FILE` loads for interactive login). The
DB-backed source is intentionally **not** implemented here: it would require
importing `internal/storage/postgres`, which this package's dependency
contract (`oidclogin`, `go-oidc/v3`, `internal/query` — nothing else) rules
out, mirroring how `cmd/api`'s `oidcDBProviderResolver` (not `oidclogin`
itself) is the one place that bridges `internal/storage/postgres` reads into
an `oidclogin`-shaped interface. `cmd/api` and `cmd/mcp-server` each build
their own DB-backed `ProviderSource` and combine it with
`NewEnvProviderSource`'s output via `ComposeProviderSources`.

A DB-backed provider config (`identity_provider_configs`) is tenant-scoped
only — it has no `workspace_id` column. A `ProviderSource` implementation
that sources from the DB must resolve a concrete `WorkspaceID` itself
(mirroring `cmd/api`'s `oidcDBProviderResolver.resolveWorkspace`, which
defaults to the tenant's one active workspace and fails closed when the
tenant has more than one) before handing a `BearerProvider` to this package,
or must omit that provider entirely when no unambiguous workspace exists.
This package never guesses a workspace.

## Activation

`ESHU_AUTH_RESOURCE_URI` (the RFC 8707 canonical resource identifier) gates
the whole feature. When unset, wiring must not construct a `Resolver` at
all — `NewResolver` returns `ErrAudienceRequired` for an empty audience by
design, so wiring treats "unset" as "build nothing," not "build one that
always errors."

## Tests

- `resolver_test.go` (and siblings): valid-audience acceptance; wrong
  audience, unknown issuer, expired, and bad-signature denial with distinct
  outcomes; a non-JWT credential falling through; the zero-provider fast
  path with zero factory calls; group-to-role mapping via a fake
  `GrantResolver`; the raw token never appearing in captured `slog` output.
- `cache_test.go`: a provider-list change becomes visible after the TTL
  elapses; a verifier is reused (not rebuilt) when a provider's revision is
  unchanged.
- `benchmark_test.go`: the zero-provider path allocates nothing and never
  calls the verifier factory (AC #4).

All tests drive the real go-oidc `Verify` path via `oidc.NewVerifier` +
`oidc.StaticKeySet`, signing tokens with `golang-jwt/jwt/v5` — no network.
