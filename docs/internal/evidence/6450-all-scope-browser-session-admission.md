# 6450: all-scope browser sessions on the scoped-route allowlist

Issue #6450. Branch `claude/6450-allscope-grant-split`.

## What was wrong

`browserSessionRouteAllowed` asked one question and answered a different one.
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
| `identity_bound` | 35 | yes |
| `tenant_data_free` | 13 | yes |
| `deployment_scoped` | 13 | no |
| `transitive` | 1 | no |
| total | 175 | |

`scopedRouteGrantBound` is the zero value, so a route added without a class
fails closed. `deployment_scoped` and `transitive` are grouped with
`grant_bound` on purpose: Mode-based redaction in `status_scoped.go` and the
transitive re-dispatch behind `POST /api/v0/ask` are not caller-grant binding.

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
cd /Users/linuxdynasty/repos/eshu-wt-6450/go && go test ./internal/query -count=1 \
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
| `scripts/dev/precommit-go.sh fmt <8 changed .go files>` | 0 |
| `scripts/dev/precommit-go.sh lint <8 changed .go files>` | 0 (1 package, 0 issues) |
| `scripts/dev/precommit-go.sh filecap <8 changed .go files>` | 0 |
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

No-Observability-Change: the new denial path emits nothing new. It reuses the
existing `recordScopedRouteAuthorizationDenied` governance-audit event and the
existing `scopedRouteDeniedResponse` 403 writer, both already called from
`tryBrowserSessionAuth` for the off-allowlist refusal. A route that now denies
where it previously admitted produces the same audit event and the same
response body it would have produced had it never been allowlisted, so no
dashboard, alert, or log parser sees a new shape.

## Scope

Out of scope and tracked separately: the scoped-bearer branch of
`authMiddlewareWithRoutePolicy` for all-scope OIDC bearer tokens, and the Ask
runner's shared-key fallback for cookie callers.

Layout note for review: the class model lives in
`auth_browser_session_route_policy.go` beside the admission function rather
than in a file of its own. `internal/query` is over the dirgate cap, and
`scripts/lib/dirgate-grandfather.tsv` refuses a bump that would absorb a file
the same change adds.
