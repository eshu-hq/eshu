# #6566: one credential, one governance-audit actor class

Follow-up to `6459-browser-session-actor-class.md`. After #6459 a dashboard
cookie session was `browser_session` on a route denial but `operator` on the
identity-mutation rows the same session caused (`localIdentityActorClass`)
and `shared_token` on its admin recovery actions (`adminRecoveryActor`).
Three mappings for one credential meant an operator filtering the audit sink
by `actor_class` could not follow one session across event types.

## What changed

`actorClassForAuth` in `go/internal/query/auth_audit.go` is now the single
mapping: `AuthModeBrowserSession` is `browser_session`, `AuthModeScoped` is
`scoped_token`, `AuthModeShared` is `shared_token`, and a blank mode (an
`AuthContext` nobody authenticated) is `anonymous`. The switch names every
`AuthMode` member with no default so the `exhaustive` linter fails a fourth
mode instead of misfiling it. The route-denial helper, the allowed-read
helper, the four identity-mutation emitters (local identity, admin identity,
provider config, sign-in policy), and `adminRecoveryActor` all call it;
`localIdentityActorClass` is deleted. `operator` stays reserved for the SSO
login path (`sso_login_audit.go`), which carries no resolved credential.

Subject-hash rules are unchanged: route denials, allowed reads, and recovery
by a scoped or cookie caller downgrade to `anonymous` without a hash; the
shared bearer keeps its stable synthetic identity on identity mutations and
recovery actions.

One more row moves at the cut-over. In the open posture (auth enforcement
not configured, `auth.Mode == ""`, where every admin route is open) a request
carries no credential, and `adminRecoveryActor` now returns `anonymous` with
no hash where the base returned `shared_token` with the synthetic identity.
So `admin_recovery_action` rows in such a deployment move from `shared_token`
to `anonymous`; no credential was presented, so the mapping stays, but an
operator filtering recovery rows by `shared_token` must include `anonymous`
after the release. `scoped_token` rows and `shared_token` rows for a
presented shared bearer are unchanged.

Historical rows are not migrated. The runbook in
`docs/public/operate/hosted-governance.md` and the `governanceaudit` README
say that rows written before this change carry the old classes for a cookie
session or an open-posture request, so a filter by class spanning the
cut-over includes both values.

## Evidence

Observability Evidence: the audit row for a cookie session's identity
mutation or recovery action now carries `actor_class = browser_session`,
matching its route denials. Pinned by `TestActorClassForAuthMapsEveryAuthMode`,
`TestAdminRecoveryActorMapsEveryAuthMode`,
`TestIdentityMutationAuditsStampActorClassByAuthMode`, and
`TestRecordScopedReadAuthorizedActorClassFollowsAuthMode` in
`go/internal/query/audit_actor_class_test.go`, each table-driven over every
`AuthMode` member; the pre-existing
`TestRecordScopedRouteAuthorizationDeniedActorClassFollowsAuthMode` still
pins the route-denial path. Existing signals: `governance_audit_events` rows
and the `audit.actor_class_count` readback on `/api/v0/status/governance`.

No-Regression Evidence: a pure mapping change on a field every emitter
already holds; no query, statement, index, lease, worker, or migration
changes. The `actor_class` column is `TEXT` with no constraint (migration
006b), and `browser_session` was already accepted by `validActorClass`
(#6459), so no reader change is needed. `go test ./internal/query
./internal/governanceaudit -count=1` passes at the branch head.
