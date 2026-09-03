# 6450: all-scope browser sessions on the scoped-route allowlist

Issue #6450. Branch `claude/6450-allscope-grant-split`.

## What was wrong

`browserSessionRouteAllowed` (since renamed `browserSessionRouteDenialReason`)
asked one question and answered a different one.
It returned `true` as soon as `scopedHTTPRouteSupportsTenantFilter(r)` did,
before `policy.AllowTenantBoundAllScopes && tenantBoundAllScopesBrowserSession(auth)`
ever ran. Membership of that allowlist means "a tenant-filtered caller may
enter", and the code read it as "any browser session may enter".

For a restricted session that is fine: the handler binds the session's real
repository or scope grant. For an all-scope session there is no grant to
bind. `querycontract.RepositoryAccessFilterFromContext` hands an all-scope
caller a filter with `AllScopes: true`, whose `Scoped()` is `false`, so the
handler's own predicate drops out, and no data-plane table carries a tenant
column to fall back on. In `hosted_multi_tenant`, where `cmd/api` deliberately
leaves `BrowserSessionRoutePolicy` at its fail-closed zero value, an all-scope
console session therefore read across tenants on every grant-filtered route on
the allowlist.

The naive gate does not work. Refusing all-scope sessions on the whole
allowlist breaks the admin console, because the `/api/v0/auth/` routes are on
that allowlist precisely so an admin cookie session can reach them at all.

Root-Cause Evidence: the early return at `auth_browser_session_route_policy.go:69`
(pre-fix), `if scopedHTTPRouteSupportsTenantFilter(r) { return true }`, sits
ahead of the policy check, and `RepositoryAccessFilterFromContext` returns
`AllScopes: true` for these callers so the handler's grant predicate is inert.
Observed directly: an all-scope tenant-bound browser session under the zero
policy reached the stub handler on `GET /api/v0/repositories` and received its
body, `status = 200, body = {"secret_cross_tenant_data":true}`, where the fix
returns `403`. That is the pre-fix probe failure recorded in
`TestAuthMiddlewareAllScopesBrowserSessionRefusedOnGrantBoundRouteUnderFailClosedPolicy`
and reproduced mechanically by the BITES neuter below.

## The fix

`scopedTokenAdvertisedRoutes` changed from `map[string]struct{}` to
`map[string]scopedRouteClass`. Every entry now records why the route is on the
allowlist, and admission splits on that class instead of on membership alone.

| Class | Count | All-scope session admitted without the policy check |
| --- | --- | --- |
| `grant_bound` | 113 | no |
| `identity_bound` | 36 | yes |
| `tenant_data_free` | 12 | yes |
| `deployment_scoped` | 13 | no |
| `transitive` | 1 | no |
| total | 175 | |

`scopedRouteGrantBound` is the zero value, so a route added without a class
fails closed. `deployment_scoped` and `transitive` are grouped with
`grant_bound` on purpose: Mode-based redaction in `status_scoped.go` and the
transitive re-dispatch behind `POST /api/v0/ask` are not caller-grant binding.

`GET /api/v0/auth/browser-session` was reclassified from `tenant_data_free`
to `identity_bound` in review, because it returns the caller's own session
identity (tenant, workspace, all-scopes flag, role ids, subject hashes)
rather than a static in-binary artifact. Both classes are admitted without
the policy check, so admission behavior is unchanged.

`scopedRouteNeedsNoCallerGrant` is the runtime half, written as a closed union
of the allowlist's existing matchers rather than an `/api/v0/auth/` prefix
test, so a future auth route that does read tenant data cannot admit itself.
The two allowlisted routes with no ledger entry, `GET /sse` and
`POST /mcp/message`, take the `grant_bound` default and stay policy-gated.

`TestScopedRouteClassLedgerAgreesWithPredicate` is the lockstep gate between
the data and the predicate, and fails in either drift direction.

One deliberate tightening rides along: a malformed tenantless all-scope
session, previously admitted to every allowlisted route, is now refused on the
grant-bound ones too, which is what `tenantBoundAllScopesBrowserSession`
already promised everywhere off the allowlist.

## BITES: break it to prove the tests guard it

Script: `bites-6450.sh` (session scratchpad; not committed, it hardcodes local
absolute paths). It seds one marked literal, `return false // bites-6450-neuter-anchor`
in `scopedRouteNeedsNoCallerGrant`, to `true`, which reopens the pre-fix
admission on every allowlisted route. Every exit code is captured directly off
the command as `cmd > log 2>&1; ec=$?`, never after a pipe.

The command run at each of the three steps:

```
cd go && go test ./internal/query -count=1 \
  -run 'TestAuthMiddlewareAllScopesBrowserSessionRefusedOnGrantBoundRouteUnderFailClosedPolicy|TestScopedRouteClassLedgerAgreesWithPredicate|TestAuthMiddlewareWithBrowserSessions|TestAuthMiddlewareAllScopesBrowserSessionSplitAcrossLedger'
```

| Step | State | Exit code |
| --- | --- | --- |
| 1 | committed tree | 0 |
| 2 | `scopedRouteNeedsNoCallerGrant` neutered | 1 |
| 3 | restored with `git checkout --`, `git status --short` clean | 0 |

Step 2 produced 387 `--- FAIL` lines. The named regression test is among them:

```
--- FAIL: TestAuthMiddlewareAllScopesBrowserSessionRefusedOnGrantBoundRouteUnderFailClosedPolicy
    --- FAIL: .../all_scopes_under_fail-closed_policy_is_refused
        handler called = true, want false; status = 200, body = {"secret_cross_tenant_data":true}
```

`TestScopedRouteClassLedgerAgreesWithPredicate` and
`TestAuthMiddlewareAllScopesBrowserSessionSplitAcrossLedger` also went red.
The 12 `TestAuthMiddlewareWithBrowserSessions*` admin subtests stayed green
under the neuter, which is the right discrimination: they are `identity_bound`
and are admitted either way, so they cannot mask the leak.

The lockstep test was separately proven to fail in both drift directions
before the suite was accepted: reclassifying `GET /api/v0/repositories` as
`identity_bound` failed with `scopedRouteNeedsNoCallerGrant = false, but the
ledger classifies it identity_bound`, and adding a matcher for that path
without reclassifying it failed with the mirror message. Both were restored.

## Focused verification

Run after the last edit, exit codes captured directly.

| Command | Exit |
| --- | --- |
| `go vet ./internal/query ./cmd/api ./internal/mcp` | 0 |
| `go test ./internal/query -count=1` | 0 |
| `go test ./cmd/api ./internal/mcp -count=1` | 0 |
| `scripts/dev/precommit-go.sh fmt <the 11 changed .go files>` | 0 |
| `scripts/dev/precommit-go.sh lint <the 11 changed .go files>` | 0 (run reported `1 package(s) from 11 path(s)`, 0 issues) |
| `scripts/dev/precommit-go.sh filecap <the 11 changed .go files>` | 0 |
| `scripts/verify-package-docs.sh` | 0 |
| `scripts/verify-root-cause-evidence.sh` | 0 |
| `scripts/verify-markdown-line-cap.sh --all` | 0 |
| `git diff --check` | 0 |
| `mkdocs build --strict --clean` | 0 |

No-Regression Evidence: admission stays a constant-time predicate over path
matchers with no I/O and no allocation, and an all-scope session pays at most
one extra matcher walk. `go test ./internal/query -count=1` wall time,
three runs each: baseline at `origin/main` 4.891s, 4.273s, 4.321s; this branch
4.780s, 4.818s, 4.290s. The two ranges overlap, so run-to-run noise dominates,
and the branch number additionally carries roughly 1050 new subtests. That
bounds the cost rather than isolating it, which is all a test-wall-time proxy
can honestly claim here; the structural argument above is the substantive one.

Observability Evidence: the new refusal gets its own governance-audit reason
code, `scoped_route_all_scope_grant_required`, on the existing
`read_authorization` event type with `decision = denied`. It is emitted by
`recordScopedRouteAuthorizationDeniedWithReason` (`auth_audit.go`) from
`tryBrowserSessionAuth`; the 403 body is unchanged and still written by
`scopedRouteDeniedResponse`.

Reusing the old `scoped_route_not_enabled` code was the first draft and was
wrong. That code means the route has no scoped authorization at all, which
was true by construction before this change. It is false for the new refusal:
the route IS enabled, and a restricted session still enters it and gets
grant-bound results. An operator who saw `scoped_route_not_enabled` here would
go looking for a missing allowlist entry that is present, when the actual
remedy is a narrower credential or an explicit `BrowserSessionRoutePolicy`
opt-in. The two codes are distinct so a denial can be triaged from the audit
row alone.

A third code, `scoped_route_denied_unspecified`, exists only as a defensive
fallback: `recordScopedRouteAuthorizationDeniedWithReason` substitutes it when
a caller hands it a blank or whitespace-only reason. It is unreachable from
the production path, because `browserSessionRouteDenialReason` returns the
empty string only for an ADMITTED request and an admitted request records no
denial, so an operator should never see it in
`governance_audit_events`. If one ever does, it means a new caller passed an
empty code, which is exactly why the fallback is its own value rather than a
reuse of `scoped_route_not_enabled`: a caller's bug filed under a real code is
the same triage failure this change fixed one level up.
`TestRecordScopedRouteAuthorizationDeniedBlankReasonFallsBackToUnspecified`
pins that identity by calling the helper directly, so a refactor that collapses
the fallback into one of the real codes fails.

The scoped-bearer branch of `authMiddlewareWithRoutePolicy` keeps the old
helper and the old code, which stays true there by construction: a scoped
bearer is refused only when the route is off the allowlist.

`TestAuthMiddlewareAllScopesBrowserSessionRefusedOnGrantBoundRouteUnderFailClosedPolicy`
asserts the reachable codes: the grant-bound refusal emits
`scoped_route_all_scope_grant_required`; a never-allowlisted route
(`GET /api/v0/graph/entities`) and a shared-key-only route
(`POST /api/v0/supply-chain/impact/suppressions`) under the same session both
still emit `scoped_route_not_enabled`; admitted requests emit no event at all;
and every emitted event carries the `ActorClass`, `ActorIDHash` and
`PolicyRevisionHash` the helper sets and passes
`governanceaudit.NormalizeEvent`.

The denial event also carries the caller's tenant and workspace. Review by
Codex on #6457 caught that it did not: a tenant admin reads
`governance_audit_events` through a `tenant_id` filter
(`governance_audit_store_helpers.go`), so a denial recorded without one was a
denial only the shared operator could ever see, even for a caller that plainly
belonged to a tenant. The helper now sets `TenantID` and `WorkspaceID` from the
normalized `AuthContext`, the way `recordScopedReadAuthorized` already did, and
both regression tests assert the two fields. The scoped-bearer path shares the
helper and gains the same fields for the same reason. (It shared it through a
fixed-code wrapper, `recordScopedRouteAuthorizationDenied`, which lost its last
caller when item 1 closed and was removed.)

That fix was written red first. Both assertions went in ahead of the helper
change, and this run failed in both tests with
`event.TenantID = "", want "tenant-a"`, exit 1:

```
cd go && go test ./internal/query -count=1 \
  -run 'TestAuthMiddlewareAllScopesBrowserSessionRefusedOnGrantBoundRouteUnderFailClosedPolicy|TestRecordScopedRouteAuthorizationDeniedBlankReasonFallsBackToUnspecified'
```

After the helper set `TenantID` and `WorkspaceID`, the same command exited 0.
`TestAuthMiddlewareWithScopedTokensAuditsUnsupportedScopedRoute` now asserts
the same two fields on the scoped-bearer path, so the shared helper cannot drop
the attribution on either caller:

```
cd go && go test ./internal/query -count=1 \
  -run 'TestAuthMiddlewareWithScopedTokensAuditsUnsupportedScopedRoute'
```

exit 0.

`governanceaudit.validReasonCode` (`audit.go`) is a FORMAT check --
lowercase, digits and underscore, 64 chars max -- not a closed vocabulary, so
the new code needed no registry entry. Nothing else in `go/` or `docs/`
enumerates governance-audit reason codes; `auditableBearerDenialReasons`
(`auth_audit.go`) is a closed set, but it governs bearer-resolver denial
outcomes, not scoped-route refusals.

## Scope

This change fixes one thing: an all-scope browser session no longer gets a
whole-graph read on a grant-bound allowlisted route. It is not a claim that
the identity-bound population is airtight for such a session. Six residuals
were named here; five stay open and are tracked separately, none of them fixed
in the original change. Item numbers are kept as written, because other
documents and issues cite them.

1. **All-scope OIDC bearer tokens. CLOSED**, on `claude/5167-ledger-drain`
   (#6472), by the commit that added `scopedBearerRouteDenialReason`. As
   originally written: the scoped-bearer branch of
   `authMiddlewareWithRoutePolicy` took no equivalent class split, so an
   all-scope bearer cleared the allowlist on membership alone. See
   [All-scope bearers](#all-scope-bearers-6450-item-1) below.
2. **Ask's shared-key fallback for cookie callers** (`internal/askwiring`,
   `internal/ask/engine`), which can re-enter inner routes on a credential
   the cookie caller did not present.
3. **Status-family Mode-only redaction.** `deployment_scoped` routes redact by
   auth `Mode` in `status_scoped.go` and take no grant at all. They are
   grouped with `grant_bound` here so the policy check still applies, but the
   redaction itself is untouched.
4. **Tenant switch via `PATCH /api/v0/auth/browser-session/context`.** The
   route is `identity_bound`, so an all-scope session reaches it without the
   policy check, and it takes the target tenant and workspace from the request
   body. `switchBrowserSessionWorkspaceQuery`
   (`storage/postgres/browser_sessions_schema.go`) gates on
   `sess.all_scopes = true` and on the target tenant and workspace being
   active, but binds nothing about the session's subject to that tenant. An
   all-scope session can therefore rebind itself to another tenant and read
   the new one. Tracked as #6450 item 4.
5. **Tenantless token-scope fallback.** `localIdentityAPITokenScope`
   (`local_identity_api_tokens.go`) falls back to a body-supplied tenant and
   workspace when `AuthContext` carries neither, and `selfServiceTokenOwner`
   (`local_identity_api_tokens_selfservice.go`) returns an empty owner hash
   for any all-scope caller, dropping the ownership predicate. The
   demonstrated exposure is token minting for any tenant-less credential, a
   shared key being the example the auth-slice finding names. It is **not**
   established that a tenantless all-scope *browser session* exists: the OIDC
   upgrade path rejects a blank tenant or workspace
   (`browser_session_handler.go`, "tenant_id and workspace_id are required to
   create a browser session"), SAML's `createSession` does the same
   (`saml_handler.go`), but `issueLocalSessionCookies`
   (`local_identity_handler_helpers.go`), shared by local login, break-glass
   and the setup wizard, copies `auth.TenantID` and `auth.WorkspaceID`
   through with no non-blank guard, and the `CreateBrowserSession` choke
   point validates neither. Caller shape (f) in the split table is carried as
   a **defensive** shape, pinning what admission does with a malformed
   session if one can exist, not as a known-live production shape. Resolving
   that reachability question is out of scope here. Tracked as #6450 item 2
   of the auth-slice findings.
6. **`actor_class` on the new audit code**, narrowed by item 1's closure.
   `recordScopedRouteAuthorizationDeniedWithReason` (`auth_audit.go`) stamps
   `actor_class = scoped_token` on a
   `scoped_route_all_scope_grant_required` row, because that is the closest
   member of the closed `governanceaudit.ActorClass` enum that
   `NormalizeEvent` validates against, and widening a validated enum is
   outside this change. When the row came only from a browser session that was
   a compromise; now that an all-scope bearer produces the same code, it is
   literally right for that half of the population and remains a compromise
   for the other. An operator filtering by `actor_class` should read
   `scoped_token` as "identity-resolved caller", not "bearer token". The
   mapping is marked at the assignment in `auth_audit.go` so it is not
   "corrected" by a later reader, and adding a browser-session member to the
   enum is tracked in #6459.

Residual 4 is why the `scopedRouteClass` doc comment says an identity-bound
handler answers from the tenant the session is *currently* bound to, rather
than the stronger and false claim that such a session can never reach another
tenant's data. Residual 5 is why that comment does not go further and treat a
tenantless session as a live browser-session shape.

Layout note for review: the class model lives in
`auth_browser_session_route_policy.go` beside the admission function rather
than in a file of its own. `internal/query` is over the dirgate cap, and
`scripts/lib/dirgate-grandfather.tsv` refuses a bump that would absorb a file
the same change adds.

## All-scope bearers (#6450 item 1)

Residual item 1 above is closed on `claude/5167-ledger-drain` (#6472). This
section is the account; the freshness-family branch that closed it carries the
route-specific half in
[5167-freshness-family-allowlist.md](5167-freshness-family-allowlist.md#all-scope-bearers-6450-item-1-closed-here).

### Why it was closed there and not on its own

#6472 promotes `GET /api/v0/freshness/changed-since` and
`GET /api/v0/freshness/generations` onto the scoped-token allowlist. Before
that promotion those two routes answered an all-scope bearer with a middleware
403; after it, with the residual open, they answered with a read across every
tenant's rows. The promotion is what opens the hole on those routes, so the
branch that promotes them is where it gets closed, rather than shipping a
widened cross-tenant read behind a follow-up label.

### The two shapes were never symmetric

`browserSessionRouteDenialReason` was reached only from the
`auth.Mode == AuthModeBrowserSession` branch of `tryBrowserSessionAuth`. The
scoped-bearer branch of `authMiddlewareWithRoutePolicy` tested one thing --
`scopedHTTPRouteSupportsTenantFilter`, allowlist membership -- and never read
`AllScopes`, the route class, or the policy. So on a grant-bound allowlisted
route an all-scope cookie session was refused under a fail-closed policy while
an all-scope bearer was admitted unconditionally, in `hosted_multi_tenant` as
readily as on a laptop, with the same inert grant predicate behind it.

Both minters produce that shape, and both make it tenant-bound:
`oidcbearer.Resolver.ResolveScopedToken` copies `TenantID`/`WorkspaceID` from
the provider config and takes `AllScopes` from the grant resolver, and
`scopedtoken.Entry.normalize` rejects a registry entry with no `tenant_id` or
`workspace_id`. So a tenantless all-scope bearer is not a shape an operator can
mint; the split table carries it (shape `i`) defensively, as it carries the
tenantless session (shape `f`).

### What changed

`scopedBearerRouteDenialReason` (`auth_browser_session_route_policy.go`) is
`browserSessionRouteDenialReason`'s sibling for a bearer, and the mode-neutral
`tenantBoundAllScopes` is the tenant-boundness test both now share. The two
differ in one deliberate way: the bearer function has no policy escape off the
allowlist. A route with no tenant filtering at all refuses a bearer whatever
the policy says, which is the pre-existing bearer behaviour and keeps the
routes still on `pendingRowFilteringRoutes` -- `GET
/api/v0/freshness/services/changed-since` among them -- refusing every bearer
in every deployment. The console session's off-allowlist opening is a dashboard
affordance, not a token one.

The refusal reuses `scopedRouteAllScopeGrantRequiredReason`. Its meaning was
never browser-specific: the route IS enabled, and a restricted caller enters it
and gets grant-bound results, but this caller's grant is inert here.

### The wiring trap

`cmd/mcp-server` reaches the middleware only through
`authMiddlewareWithAllowedReadAudit`, which hardcoded the fail-closed
`BrowserSessionRoutePolicy{}`. Nothing depended on that value, so nothing
noticed. The moment the bearer branch started reading it, a naive fix would
have refused every all-scope token on every grant-bound MCP route in every
profile, `local_no_policy` included -- a real regression for local CLI and MCP
workflows, shipped as a security fix.

So the policy became a parameter, `cmd/api`'s private mode table moved to
`query.ScopedRoutePolicyForGovernanceMode` where both commands call it, and
`buildTransportAuthMiddleware` threads the `governanceStatus` `wireAPI` already
computes. `TestTransportAuthMiddlewareHonoursTheGovernanceRoutePolicy`
(`cmd/mcp-server`) drives that composition per mode.

### Per-mode behaviour

| Caller | unset / `local_no_policy` / `hosted_single_tenant` | `hosted_multi_tenant` or unrecognized |
| --- | --- | --- |
| restricted bearer, grant-bound route | admitted, grant-bound | admitted, grant-bound |
| tenant-bound all-scope bearer, grant-bound route | admitted, whole corpus | 403 `scoped_route_all_scope_grant_required` |
| tenantless all-scope bearer, grant-bound route | 403 | 403 |
| any all-scope bearer, identity-bound or tenant-data-free route | admitted | admitted |
| any bearer, route off the allowlist | 403 `scoped_route_not_enabled` | 403 `scoped_route_not_enabled` |

`TestAuthMiddlewareAllScopesBrowserSessionSplitAcrossLedger` drives shapes `g`,
`h` and `i` -- the bearer mirror of the session shapes `b`, `c` and `f` -- over
every entry in `scopedTokenAdvertisedRoutes`, so every ledger class is asserted
for the bearer as well as the session.

### The consequence worth naming: the MCP handshake

`GET /sse` and `POST /mcp/message` are on the allowlist with no ledger entry,
so they take the grant-bound default. #6450 already refused an all-scope
console session there under a fail-closed policy; now an all-scope bearer is
refused too, which under `hosted_multi_tenant` means such a token cannot
complete `initialize` or `tools/list`, not merely that its reads come back
empty.

That is the right answer and it is not a comfortable one. It is right because
`tools/call` re-dispatches through this same middleware against the specific
`/api/v0/...` route, so every grant-bound read the token could have made was
already going to be refused: the handshake was the only part still working, and
a working handshake in front of a wall of 403s is a worse operator experience
than an honest refusal at the door. It is uncomfortable because the failure
arrives earlier and reads as "MCP is broken" rather than "this credential is
too broad". The governance-audit row is what distinguishes them:
`scoped_route_all_scope_grant_required` on `GET /sse` says exactly which of the
two it is. An operator who wants the handshake back narrows the credential, or
runs the deployment in the mode whose isolation model the token matches.

### The neutered run

The break-it-to-prove-it run lives with the branch that closed this
([5167-freshness-family-allowlist.md](5167-freshness-family-allowlist.md#all-scope-bearer-bites):
baseline 0, neutered 1, restored 0). These are the failures the neuter
produces, which is the part worth keeping next to the change itself:

```
--- FAIL: TestAllScopeBearerOnFreshnessDeltaRoutesPerGovernanceMode
    both hosted_multi_tenant rows, the unrecognized-mode row, and both
    tenantless rows: status = 200, want 403
--- FAIL: TestAllScopeBearerTwoTenantBoundary
    changed-since and generations, hosted_multi_tenant_never_runs_the_read:
    200 with scope-b / gen-b in the body -- the other tenant's row, which is
    the defect itself rather than a status code
--- FAIL: TestAuthMiddlewareAllScopesBrowserSessionSplitAcrossLedger
    shapes g (fail-closed bearer) and i (tenantless bearer)
--- FAIL: TestAuthMiddlewareAllScopesBrowserSessionSplitOnUnledgeredTransportRoutes
    g on GET /sse and POST /mcp/message: handler called = true, want false;
    body = {"secret_cross_tenant_data":true}
```

That last body is worth reading twice. The split table's stub handler writes a
sentinel payload, so a refusal that let the handler run shows up as data in the
403 rather than only as a status code -- and under the neuter it does run, on
the MCP transport paths as well as across the ledger.

The pending service route's rows do NOT move under the neuter, which is
correct: it is off the scoped-token allowlist, so the first check refuses it
with `scoped_route_not_enabled` and this predicate never runs for it.

### Why refusal, and not expanding `all_scopes` into a grant

`tenant_scope_grants` and `tenant_repository_grants`
(`schema/data-plane/postgres/006c_tenant_workspace_grants.sql`) could in
principle resolve `all_scopes` into the caller's real scope set instead of
refusing. That was considered and rejected for this change, on three grounds.

No reader exists: the only non-test use of `tenant_scope_grants` is a
per-item correlated `EXISTS` subquery in workflow admission
(`storage/postgres/workflow_control_sql.go`), not a list-grants-for-a-tenant
query, so a store method, its interface, its wiring into `internal/query`, and
a cache with an invalidation story would all be new.

It puts a database on the admission path. The middleware touches no datastore
today. Adding a round trip -- or a cache -- to every all-scope bearer read is a
performance-contract change, and a tenant with thousands of scopes produces an
unbounded `ANY($4)` array per request. That is an unproven theory, and
Prove-The-Theory-First says it needs its own `EXPLAIN ANALYZE` against a
realistic tenant before anyone builds it.

It changes what the credential means. An operator who wrote `all_scopes` asked
for admin-equivalent reach and would silently get less. Refusal keeps the
meaning and makes the deployment posture the thing that decides.

Refusal is the right first move. Grant expansion stays a legitimate follow-up
if operators ask for it.

### Who loses access

Nobody, as far as the committed proofs go.
`scripts/run-two-team-governance-proof.sh` and
`scripts/run-k8s-two-team-governance-proof.sh` both seed an admin `all_scopes`
registry entry and run under `ESHU_GOVERNANCE_MODE: hosted_multi_tenant`, which
looks fatal to the committed claim that an admin token enumerates every
ingested repository. It is not. That assertion runs before the registry is
mounted, where the admin token IS `ESHU_API_KEY` and resolves to
`AuthModeShared`, which the scoped branch never touches. After the mount, only
the two team tokens are used. The admin entry is registered and never read
with. The hosted-governance page's bullet has been corrected to say the shared
operator key, which is what it always meant.
