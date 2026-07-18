# AGENTS — internal/oidcbearer

IdP bearer-token resolver (issue #5162). Read `README.md` and `doc.go` before
editing.

## Invariants (do not regress)

- Never log, wrap into an error message, or otherwise expose the raw bearer
  token. Only `iss` (issuer), a `SubjectIDHash`-shaped hash, and the bounded
  `outcome` enum may appear in logs or errors. `resolver_test.go` has a test
  that fails if this regresses.
- Once a credential is JWT-shaped AND at least one bearer IdP is enabled,
  `ResolveScopedToken` must return `(zero, false, err)` — never
  `(zero, false, nil)` — on every denial path. A `(false, nil)` return past
  that point would let the composite resolver chain fall through to a
  resolver that cannot understand a JWT anyway, which is harmless
  functionally but breaks the "this resolver owns the verdict" contract and
  the distinct-denied-reason acceptance criterion.
- `AuthContext.Mode` must always be `query.AuthModeScoped` on a successful
  resolve. This is deliberate, not an oversight: it is what makes a
  bearer-resolved context inherit the F-6 scoped-route allowlist enforced in
  `internal/query/auth.go`. Changing it would silently bypass that
  allowlist — do not "fix" it without understanding this.
- The zero-provider path (`NewResolver` with a `ProviderSource` returning no
  providers) must allocate nothing and must never call `VerifierFactory`.
  `benchmark_test.go` asserts this; do not add work to the hot path that
  breaks it.
- `cache.go`'s rebuild path must never block a concurrent
  `ResolveScopedToken` call. Reads are a lock-free `atomic.Pointer[snapshot]`
  load; only `rebuild` (invoked from at most one in-flight goroutine at a
  time, guarded by `rebuildMu`/`rebuilding`) may construct and swap a new
  snapshot.
- A rebuild must reuse a provider's existing verifier when its
  `(IssuerURL, RevisionID)` is unchanged from the previous snapshot. Do not
  rebuild every verifier on every TTL tick — that would generate needless
  JWKS/discovery traffic for a stable provider set.
- This package's dependency contract is `oidclogin`, `go-oidc/v3`, and
  `internal/query` — nothing else. In particular it must never import
  `internal/storage/postgres`; the DB-backed `ProviderSource` and grant
  resolver adapters live in `cmd/api` and `cmd/mcp-server` instead (see
  README's "Provider sources" section for why). Adding a
  `storage/postgres` import here is a layering regression.

## Boundaries

- This package produces `query.AuthContext`; it does not enforce routes or
  mount HTTP handlers. It is a `query.ScopedTokenResolver` slotted into
  `scopedtoken.ChainResolvers` by `cmd/api` and `cmd/mcp-server` wiring,
  positioned between the identity-token resolver and the file-registry
  resolver.
- Grant resolution must go through the caller-supplied
  `oidclogin.GrantResolver`, never reimplemented here — AC #3 (grant
  equivalence with interactive login) requires the actual same composition,
  not a lookalike.
- Workspace resolution for a DB-backed provider is out of scope for this
  package (see README). A `ProviderSource` implementation must arrive with a
  concrete, already-resolved `WorkspaceID` or must omit the provider.

## Tests

`resolver_test.go`, `cache_test.go`, and `benchmark_test.go` cover the
algorithm, the cache, and the AC #4 performance contract respectively. Add a
case to the matching file for any new claim, outcome, or cache behavior;
tests drive the real `go-oidc` `Verify` path via `oidc.StaticKeySet` and
`golang-jwt/jwt/v5` signing, never a hand-rolled reimplementation of JWT
verification.
