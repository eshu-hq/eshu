# Findings Detail — Eshu run-1 (medium+ records)

Full schema-shaped records for all 11 fingerprints live in `findings.json`; the three medium records carry their complete trace and reproduction here. All commands run against the reviewed tree at commit `5766b3592d7975860efcc2b01078b51378bb6ace` with `GOTOOLCHAIN=local ~/.local/go/bin/go` (go1.27.3).

## 1. `eshu.query.auth.headerless-open-with-db-only-credentials` — medium

**Ordered repository-relative trace**
1. `entrypoint` — `go/internal/query/auth.go:162` (`authMiddlewareWithRoutePolicy`): single HTTP auth gate for the API plane.
2. `propagation` — `go/cmd/api/auth_enforcement.go:42` (`authEnforcementConfigured`): counts only `apiKey`, `fileResolver`, `oidcResolver`; the identity resolver is invisible to the predicate.
3. `propagation` — `go/internal/query/auth.go:187` (`authMiddlewareWithRoutePolicy`): with enforcement "not configured", a headerless request proceeds without an `AuthContext`.
4. `sink` — `go/internal/query/querycontract/repository_authz_context.go:26` (`RepositoryAccessFilterFromContext`): missing `AuthContext` → `AllScopes: true`, unfiltered repository reads.

**Evidence**
- `go/cmd/api/auth_enforcement.go:42` — three-knob predicate definition.
- `go/cmd/api/wiring.go:155` — identity resolver wired unconditionally, first in the chain (`:211`).
- `go/cmd/api/auth_enforcement_wiring_test.go` — `TestAPIAuthBootstrapIdentityOnlyServesHeaderlessOpen` pins headerless 200 under identity-only wiring.

**Principal / resource**: any client on the API listener network (lower-trust, no credential); the data-plane read surface of the identity-only deployment.

**Native input and bounded instructions**
1. `go test ./cmd/api -run "AuthEnforcement|Headerless" -count=1`
2. `go test ./internal/query -run "Headerless|EnforcementConstructor" -count=1`
3. Inputs: headerless `GET /api/v1/...`; same path with the seeded identity token; same path with a wrong token.

**Observed output and invariant proved**: 5 PASS + 7 PASS, exit 0. Headerless → 200 with no `AuthContext`; matching token → 200; wrong token → 401. The plane's enforcement posture is decided by a predicate that omits a live credential source, so "any credential present ⇒ enforced" does not hold for headerless requests. Artifact: `agents/hunter-authn2/artifacts/cmdapi-headerless-open.txt`.

**Conditions / containment**: identity-token-store-only wiring; headerless request. Each credential-carrying request still receives its scoped filter, so containment is per-request.

**Remediation / regression**: count the identity resolver in `authEnforcementConfigured` when `identity_token_metadata` holds rows. Regression pair: `TestAPIAuthBootstrapIdentityOnlyServesHeaderlessOpen` flips to enforced 401 while `TestAPIAuthDemoConfigServesHeaderlessOpen` (empty stub store) keeps its 200.

## 2. `eshu.query.browser-session.context-switch-unbound-tenant` — medium

**Ordered repository-relative trace**
1. `entrypoint` — `go/internal/query/browser_session_handler.go:264` (`handleSwitch`): parses the body pair `{tenant_id, workspace_id}` and forwards it verbatim to the store.
2. `sink` — `go/internal/storage/postgres/browser_sessions_schema.go:369` (`switchBrowserSessionWorkspaceQuery`): single UPDATE keyed on `session_hash` with catalog validation only; no subject/membership predicate.

**Evidence**
- `go/internal/storage/postgres/browser_sessions_schema.go:369` — query text binds only `session_hash`; `ws.tenant_id`/`ws.workspace_id` come from the request pair; checks `ws.status='active'`, `ten.status='active'`, both `tombstoned_at IS NULL`, `revoked_at IS NULL`, `all_scopes=true`, both expiries.
- `go/internal/storage/postgres/browser_sessions_test.go:372` — `TestBrowserSessionStoreSwitchesActiveWorkspaceByHash` pins the single-UPDATE shape via a recording fake.
- `go/internal/query/auth_browser_session_route_policy.go:297` — in-tree note records the unbound-pair residual (#6450 item 4).

**Principal / resource**: an authenticated browser-session principal holding the session id + CSRF token; the session row's active-workspace anchor and every later identity-bound read that re-anchors on it (resolve projection, schema `:220-249`).

**Native input and bounded instructions**
1. `go test ./internal/storage/postgres/ -run TestBrowserSessionStoreSwitchesActiveWorkspaceByHash -count=1`
2. Input: body `{tenant_id, workspace_id}` pair differing from the row's stored anchor, with the correct session hash and CSRF token.

**Observed output and invariant proved**: PASS, exit 0. The recording fake shows the UPDATE binds solely on `session_hash`; no term ties the pair to the row's stored tenant/workspace or `subject_id_hash`. The invariant "the active anchor equals the session row's own membership" is not enforced at the switch point.

**Conditions / containment**: valid session id + CSRF pair; target pair exists in the active non-tombstoned catalog. Contained to the catalog-validated pair set.

**Remediation / regression**: add `sess.tenant_id = $2` and an `EXISTS` over `tenant_scope_grants` (`g.tenant_id = $2 AND g.workspace_id = $3 AND g.subject_class = sess.subject_class AND g.tombstoned_at IS NULL`) to the switch WHERE. Regression: extend the recording fake to pin the new terms — an unbound pair must yield zero matched rows.

## 3. `reducer.bootstrap.handler-timeout-not-enforced` — medium

**Ordered repository-relative trace**
1. `entrypoint` — `go/cmd/reducer/main.go:218` (`run`): loads/validates `ESHU_REDUCER_HANDLER_TIMEOUT` and `_OVERRIDES`, logs them as recorded but not enforced (`:228`), stores them (`:455-456`).
2. `propagation` — `go/internal/reducer/service_heartbeat.go:245` (`HandlerTimeoutFor`): the only resolver; callers are tests only (`service_handler_abandon_test.go:541-548`).
3. `sink` — `go/internal/reducer/service_heartbeat.go:315` (`executeHandler`): select with exactly two branches (`done`, `shutdownCtx.Done()`), non-blocking re-check `:324-328`; no timeout case. The batch caller (`service_batch.go:489`) adds no bound.

**Evidence**
- `go/cmd/reducer/main.go:228` — startup WARN: bounds recorded but not enforced.
- `go/internal/reducer/service_handler_abandon_test.go:541` — the only `HandlerTimeoutFor` callers; enforcement itself is ungraded.
- `go/cmd/reducer/README.md:150-151` — documented rows match the non-enforced posture.

**Principal / resource**: the reducer process and its queue; per stuck handler one worker goroutine, one pooled Postgres/Neo4j connection, and one fenced conflict key until process shutdown.

**Native input and bounded instructions**
1. `go test ./internal/reducer -run "TestServiceRun|TestHandlerTimeoutFor" -count=1 -v`
2. Caller census: `rg -n "HandlerTimeoutFor" --glob '!*_test.go' go/` → definition only.
3. Configuration under test: `ESHU_REDUCER_HANDLER_TIMEOUT=2s` with a handler that blocks.

**Observed output and invariant proved**: exit 0 — `TestServiceRunAbandonsStuckHandlerAtShutdown`, `TestServiceRunBatchAbandonsStuckHandlerAtShutdown`, `TestHandlerTimeoutForResolvesOverrideThenDefault` all PASS, yet none applies a bound mid-run: the select has no timeout branch and the heartbeat (`service_heartbeat.go:91-128`) keeps `claim_until` fresh so the expired-lease reclaim cannot free the key. The declared bound is operator-visible but not executed. Artifact: `agents/hunter-w5a/artifacts/local-proof.txt`.

**Conditions / containment**: a handler that blocks past the intended bound (wedged backend round trip, long scan); deployments that set the env vars. Other domains keep progressing — containment is per stuck handler.

**Remediation / regression**: add a third select case on `time.After(HandlerTimeoutFor(item.Domain))` in `executeHandler`, abandoning the item per the existing shutdown-abandon path. Regression: the two existing abandon tests gain a timer-branch twin proving the item is released at the bound rather than at shutdown.
