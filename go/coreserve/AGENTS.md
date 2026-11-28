# AGENTS.md — coreserve guidance for LLM assistants

## Read first

1. `doc.go` — package contract: alias discipline, v0.1.0 surface limits,
   module discipline, and the no-observability-change note.
2. `README.md` — ownership, tag sequence, and verification commands.
3. `coreserve.go` — the serving seam: `CoreServeDeps`, `CoreAuthWiring`,
   `NewCoreServeMux`.
4. `types.go` — auth-safe provider and field types: `AuthProviderItem`,
   `AuthProviderStore`, `SignInPolicyReadStore`, `GraphQuery`,
   `QueryProfile`, `ScopedTokenResolver`, `BrowserSessionResolver`,
   `GovernanceStatusConfig`, `GovernanceAuditAppender`, `GraphBackend`,
   `CookieSecureMode`, `MetricsTimeSeriesSource`.
5. `handlers.go` — auth-family handler aliases: `LocalIdentityHandler`,
   `BrowserSessionHandler`, `BrowserSessionListHandler`,
   `AdminIdentityReadHandler`, `AdminIdentityMutationHandler`,
   `ProfileHandler`, `SignInPolicyReadHandler`,
   `SignInPolicyMutationHandler`, `SetupHandler`, `OIDCLoginHandler`,
   `SAMLHandler`, `GitHubLoginHandler`, `AuthProviderListHandler`,
   `AdminProviderConfigReadHandler`,
   `AdminProviderConfigMutationHandler`.
6. `serve.go` in the parent `internal/query` package — the `CoreServeDeps`,
   `CoreAuthWiring`, and `NewCoreServeMux` definitions this package
   aliases. Behavior changes land there, not here.

## Invariants this package enforces

- **Aliases only** — every exported name is a `=` alias over
  `internal/query`. No structs, no constructors, no conversion functions,
  no copied handler logic. A store written against these names satisfies
  the underlying query interfaces method-for-method.
- **Pure delegation** — `NewCoreServeMux` forwards to the parent
  constructor and adds no middleware, routes, or runtime path. Do not add
  any.
- **Frozen v0.1.0 surface** — component policy, telemetry instruments,
  narration status, `APIRouter`, and `ExtraMounts` stay out. External
  callers leave those fields zero; widening needs a follow-up version,
  not a local fork.
- **Secret boundary** — the serving binary builds `CoreAuthWiring`
  itself. This package never imports `internal/secretcrypto`, and
  secret-sealed material must never cross it.
- **No replace directives** — `go.mod` carries none. The parent require
  pins a pseudo-version until the tag sequence in `README.md` completes;
  consumers pin the tag after that.
- **Docs travel with the contract** — `doc.go`, `README.md`, and this
  file stay aligned on every change; the package-docs gate fails the
  tree when any one is missing.

## Common changes and how to scope them

- **Widen the surface (new alias)** → add the `=` alias in `types.go` or
  `handlers.go`, record the version move in `doc.go` and `README.md`,
  then run the verification block below. Never copy the parent type or
  wrap it.
- **Parent seam moved** → update the alias target only; no logic follows
  it here. Confirm with `go build ./...` and `go vet ./...` from this
  directory plus `go test ./internal/query -count=1` from `go/`.
- **Version roll** → bump the parent require to the new pseudo-version
  (or tag, post-cutover), run `go mod tidy`, confirm no `replace` lines
  appear and the fork indirect line stays as documented.

## Verification

```bash
cd go/coreserve && go build ./... && go vet ./... && go test ./... -count=1
cd go && go test ./internal/runtime/ -run TestRepositoryDocumentationStandardsAreEnforced -count=1
bash scripts/verify-doc-claims.sh go/coreserve
```

## Failure modes and how to debug

- Symptom: `go mod tidy` wants to add unexpected requirements → likely
  cause: the parent require drifted from the pinned merge commit; re-pin
  before tidying.
- Symptom: a consumer build fails on a missing field → likely cause: the
  field is outside the frozen v0.1.0 surface; check the surface limits in
  `doc.go` before reaching for a fork.
- Symptom: the package-docs gate names this directory → likely cause:
  `doc.go`, `README.md`, or `AGENTS.md` is missing or renamed; restore
  the exact filenames.

## Anti-patterns specific to this package

- **Defining behavior here** — handler logic, middleware, or route
  registration belongs in `internal/query`, not in this module.
- **Constructing or wrapping parent values** — alias the type; do not
  build, wrap, or convert parent values.
- **Adding a `replace`** — local pins via `replace` break the
  tag-consumption model; re-pin the require instead.
- **Grounding prose in test files** — the doc-claims gate excludes
  `*_test.go` from its source check, so identifiers cited in `README.md`
  and here must appear in non-test files.
