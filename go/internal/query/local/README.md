# Local identity

## Purpose

The production local-identity routes: bootstrap (`POST /api/v0/auth/local/bootstrap`),
login (`POST /api/v0/auth/local/login`), invitations (`POST
/api/v0/auth/local/invitations`, `.../accept`), admin password reset (`POST
/api/v0/auth/local/users/{user_id}/password`), self-service password rotation
(`POST /api/v0/auth/local/password/rotate`), MFA reset (`POST
/api/v0/auth/local/users/{user_id}/mfa-reset`), user disablement (`POST
/api/v0/auth/local/users/{user_id}/disable`), break-glass recovery (`POST
/api/v0/auth/local/break-glass`, `.../session`), generated API-token
create/list/revoke/rotate (`/api/v0/auth/local/api-tokens*`), and self-service
TOTP enrollment (`/api/v0/auth/local/mfa/totp/*`).

## Ownership boundary

Owns `IdentityHandler`, its route dispatch, the `IdentityStore`/
`IdentityProfileLister` storage ports, every local-identity request/response
type, the require_sso and allow_local_user_creation sign-in-policy gates for
this family's routes, and `IssueSessionCookies` (the session-issuance body
this family and the setup wizard's final step both call). Does not own the
sign-in-policy read/write store itself (`auth.SignInPolicyReadStore`,
root's `sign_in_policy_reads.go`/`sign_in_policy_mutations.go`), the
browser-session store or cookie primitives (`auth`), the governance
audit sink (`auth.GovernanceAuditAppender`), or the permission catalog
(`auth.AllowsPermissionFeature`) -- those are separate homes this
package calls into. Does not own `SetupHandler` (root's `setup_handler.go`
family, moving in a later #6642 lane): it shares this package's
`IdentityHash`/`IdentityHashes`/`IssueSessionCookies`/`PolicyRevision`
helpers through root's `local_identity_alias.go` forwarders, but its own
struct, routes, and tests stay in root until that lane lands.

## Layout

- `handler.go` -- `IdentityHandler`, `Mount`, and the session-lifecycle route
  bodies: `handleBootstrap`, `handleLogin`, `handleCreateInvitation`,
  `handleAcceptInvitation`, `handleResetPassword`, `handleRotatePassword`,
  `handleResetMFA`, `handleDisableUser`.
- `helpers.go` -- the break-glass route bodies (`handleEnableBreakGlass`,
  `handleBreakGlassSession`), `issueLocalIdentitySession`, `SessionIssued`,
  `IssueSessionCookies`, the auth-gate helpers (`ready`,
  `requireSharedOperator`, `requireAllScopeAuth`, `requirePermissionFeature`),
  the clock/secret/cookie-mode accessors, `auditLocalIdentity`, and
  `IdentityHash`/`IdentityHashes`.
- `api_tokens.go` -- `mountAPITokenRoutes` and the four generated API-token
  route bodies, their request/response types, and
  `resolveSelfServiceAPITokenUserID`/`buildAPITokenCreateRecord`.
- `api_tokens_self_service.go` -- the self-service authorization helpers for
  API-token create/revoke/rotate (issue #5164):
  `authorizeTokenMutation`, `selfServiceTokenOwner`,
  `writeSelfServiceTokenNotFound`, `enforceSelfServiceTokenCreateScope`.
- `totp.go` -- `mountTOTPRoutes` and the two self-service TOTP enrollment
  route bodies (issue #4986), their request/response types.
- `profile.go` -- `IdentityAPITokenListItem`, `IdentityMFAStatus`, and the
  `IdentityProfileLister` interface `ProfileHandler` and `SetupHandler` read
  through.
- `requests.go` -- every route's unexported request-body struct, plus the
  small pure helpers (`localIdentityDefault`, `localIdentityExpiry`,
  `PolicyRevision`, `localIdentityOptionalID`, `authTenantID`/
  `authWorkspaceID`/`authSubjectIDHash`).
- `sign_in_policy_gate.go` -- `requireSSODecision`, `allowLocalUserCreation`,
  and `recordRequireSSOLoginGate` (issue #4968, epic #4962).
- `types.go` -- `IdentityStore`, every hash-only storage record type, the
  closed `IdentityAuthStatus` enumeration, and `IdentityAuthContext`.
- Test files -- this package's own tests: `helpers_test.go` (moved, one
  obsolete case replaced; `IdentityHash` format proof),
  `handler_fakes_test.go` (`fakeStore`,
  `fakeSessions`, `sequenceSecrets`), `handler_test.go`/`api_tokens_test.go`/
  `totp_test.go` (new route-coverage tests, one success plus one meaningful
  failure path per route), and `audit_test.go`
  (`TestAuditLocalIdentityStampsActorClassByAuthMode`, moved from root's
  `audit_actor_class_test.go`). See Move evidence below for why these tests
  are new rather than carried over verbatim.

## Move evidence

The nine production files moved here from the query root
(`local_identity_api_tokens.go`, `local_identity_api_tokens_selfservice.go`,
`local_identity_handler.go`, `local_identity_handler_helpers.go`,
`local_identity_profile.go`, `local_identity_requests.go`,
`local_identity_sign_in_policy_gate.go`, `local_identity_totp.go`,
`local_identity_types.go`, #6642), destuttered under rule 2
(`local_identity_api_tokens_selfservice.go` -> `api_tokens_self_service.go`,
etc.) with `LocalIdentityHandler` renamed to `IdentityHandler` at its
declaration and every other `LocalIdentity`-prefixed exported identifier
losing the `Local` stutter per naming rule 4 (`LocalIdentityStore` ->
`IdentityStore`, `LocalIdentityAuthContext` -> `IdentityAuthContext`,
`ErrLocalIdentityAPITokenNotFound` -> `ErrIdentityAPITokenNotFound`, and so
on). `IdentityHash` keeps its pre-move exported name -- see AGENTS.md for why
the pre-move unexported `localIdentityHash` was merged into it rather than
carrying two exported spellings of the same computation forward.
`issueLocalSessionCookies` -> `IssueSessionCookies` and
`localIdentityPolicyRevision` -> `PolicyRevision` export the two other
unexported helpers root's staying setup family and
`sign_in_policy_mutations.go` still call; `localSessionIssued` ->
`SessionIssued` also had to be exported, purely so root's
`issueLocalSessionCookies` forwarder can name its own return type -- naming
rule 4 did not otherwise require it (see AGENTS.md). Root keeps every
pre-move exported spelling in `local_identity_alias.go`: the twenty-four type
aliases, the six `LocalIdentityAuth*` status-value aliases, the
`ErrLocalIdentityAPITokenNotFound` var alias, and the `IdentityHash`/
`localIdentityHash`/`localIdentityHashes`/`issueLocalSessionCookies`/
`localIdentityPolicyRevision` forwarders the setup family and
`sign_in_policy_mutations.go` need. `handler.go`'s `LocalIdentity
*LocalIdentityHandler` field, `profile_handler.go`'s `LocalIdentityStore
LocalIdentityProfileLister` field, `setup_types.go`'s
`RotateSetupPassword(ctx, reset LocalIdentityPasswordReset)`, and every
`cmd/api`/`cmd/mcp-server` construction of `query.LocalIdentityHandler{...}`
needed no edit at all: the type aliases make every pre-move spelling still
resolve.

Helper replacements: every call the family made to a root-private
auth/session/permission helper now calls the seam spelling the #6642
auth-seam handoff assigned it --
`auth.{GovernanceAuditAppender,AuthContext,AuthContextFromContext,
AuthModeBrowserSession,AuthModeShared,BrowserSessionAuthResponse,
BrowserSessionCreateRecord,BrowserSessionStore,CreateBrowserSession,
BrowserSessionSecretHash,CookieSecureMode,
DefaultBrowserSessionAbsoluteTimeout,DefaultBrowserSessionIdleTimeout,
ParseCookieSecureMode,SignInPolicyReadStore,ActorClassForAuth,
NormalizeAuthContext,PermissionFeatureIdentityAdmin,PermissionFeatureTokens,
BrowserSessionAuthResponseFor,ResolveSessionTimeouts,
WriteBrowserSessionCookies,AllowsPermissionFeature}` and
`querycontract.{PathParam,ReadJSON,WriteError,WriteJSON,WriteUnauthorized,
WritePermissionDenied}`. `go list -deps ./internal/query/local` names no
`internal/query` root dependency.

The `Identity` stutter, considered and rejected: after dropping `Local`,
every remaining exported identifier in this package still leads with
`Identity` (`IdentityHandler`, `IdentityStore`, `IdentityAuthContext`,
`IdentityAPITokenCreate`, and so on) -- a second, package-word-adjacent
stutter naming rule 4 asks the mover to flag. Collapsing `LocalIdentity` to
one dropped unit (`local.Handler`, `local.Store`, `local.AuthContext`) was
considered against two costs: `local.AuthContext` would collide in read (not
name, since Go scoping resolves it, but in a reviewer's eye) with
`auth.AuthContext`, a completely different type this same file already
imports and uses side by side in `IssueSessionCookies` -- a package that
reads `AuthContext` and `auth.AuthContext` on adjacent lines invites
mistaking one for the other far more than `IdentityAuthContext` and
`auth.AuthContext` do. `local.Store` would also read one step removed
from what it stores (a local **identity**, not a local anything-else) in a
package whose own name (`local`) says nothing about identity at all -- unlike
`freshness.Cause` or `secrets.IAMFinding`, where the package name alone
carries the missing noun. The literal rule 4 rename (drop only `Local`) was
applied; the orchestrator may still ask for the alternative.

Capabilities: this family registers no capability. Its routes are
session/credential lifecycle endpoints gated by shared-operator or
all-scope-admin authentication and, where applicable, the permission
catalog -- never by `querycontract`'s capability matrix or
`BuildTruthEnvelope`. `rg -n 'local_identity|localIdentity'
go/internal/query/contract_*.go` returns nothing, so there is no leaf
`capabilities.go`/`main_test.go` and no root
`capability_lockstep_local_test.go` to add; this move brief's step 3 is
inapplicable to this family.

Tests: almost every pre-move family test file depended on root-only test
doubles this package cannot reach (`fakeGovernanceAuditAppender`,
`sequenceSecrets`, `fakeBrowserSessionStore`, `fakeSignInPolicyReadStore`,
`fakeLocalIdentityListStore`, `bodyContains`, `fixedNow`), most of which are
also shared with other still-root families (setup, status) rather than
being local-identity-specific hoist candidates under the #6608 rule (that
rule hoists a fixture BOTH the leaf and root need identically; these are
shared across MORE than this one family's split, and hoisting them would
mean rewriting every other family's root tests that call them too, well
outside this move's scope). Only `local_identity_handler_helpers_test.go`
(no out-fixtures, `IdentityHash` format proof) moved, as
`helpers_test.go`; its one obsolete case
(`TestIdentityHashMatchesUnexportedImplementation`, proving two functions
that no longer exist separately produce equal output) was replaced with
`TestIdentityHashUsesTheSha256Prefix`, pinning the merged function's wire
format instead.

`local_identity_handler_fakes_test.go`'s `fakeLocalIdentityStore` looked
movable in isolation (no out-fixtures of its own) but is used by seven other
family test files that stay in root because THEY reach family-unexported or
root-unexported symbols the move cannot carry across the package boundary
(`local_identity_api_tokens_test.go` needs the leaf-private
`localIdentityAPITokenResponse` type and the pre-merge `localIdentityHash`
name; `local_identity_handler_test.go` needs root's `publicHTTPPaths`;
`local_identity_totp_test.go` needs root's `minQueryTreeNonTestFiles`/
`sweepGoFiles`). Moving the fake alone would have broken all seven, so it
stayed in root too, renamed `identity_handler_fakes_test.go` clear of the
new `local/` directory's naming-rule sibling-prefix check (see below). Two
root CONFLICT test files needed a small in-file fix rather than a stay-as-is:
`identity_api_tokens_test.go` and `identity_totp_test.go` each gained a
package-local `identity*ResponseWireShape` decode struct copying the exact
JSON shape the leaf's now-private response type writes, since a root test
can no longer name a `package local`-private type to `json.Unmarshal` into
it; `identity_totp_test.go`'s static secret-leakage sweep
(`TestOnlyTOTPBeginResponseCarriesSecretJSONField`) also had its exempted
basename repointed from `local_identity_totp.go` to `totp.go` to match the
file's new location under the (already-recursive) `go/internal/query` walk.

One test could not stay in root at all: `audit_actor_class_test.go`'s
`identityMutationEmitters()` table called `LocalIdentityHandler`'s
now-leaf-private `auditLocalIdentity` method directly -- an alias forwards a
TYPE, but an unexported METHOD's visibility is decided by its declaring
package, so no alias stanza can restore that specific call site. Its "local
identity" emitter row moved to this package's own `audit_test.go` as
`TestAuditLocalIdentityStampsActorClassByAuthMode`, driven with the same
`AuthMode` table root's shared `actorClassModeCases()` used; root's table
keeps only its own staying emitter (sign-in policy mutation).

Since every pre-move family test stayed in root or needed root-private
fixtures, this leaf's own directory had zero tests naming any of its sixteen
routes by handler method -- `scripts/verify-route-coverage.sh`, which scopes
its test-existence search to a route's own directory (never a sibling
package, #6055), reported all sixteen `UNCOVERED`. `handler_test.go`,
`api_tokens_test.go`, and `totp_test.go` are new tests (not carried over)
built for exactly that gate, using a fresh `fakeStore`/`fakeSessions` pair
(`handler_fakes_test.go`) rather than root's private fakes: one success case
and one meaningful failure case per route (auth-required, ownership-denied,
wrong-code, and similar), not the full behavioral matrix root's pre-move
tests still cover for the same handlers.

Staying root files that started with `local_identity_` tripped the new
`local/` directory's dirgate sibling-prefix naming rule (any qualifying file
whose stem is `local` or starts with `local_`) and were renamed clear of it:
`local_identity_api_tokens_list_handler_test.go` ->
`identity_api_tokens_list_handler_test.go`,
`local_identity_api_tokens_selfservice_test.go` ->
`identity_api_tokens_self_service_test.go` (rule 1: no glued `selfservice`),
`local_identity_api_tokens_test.go` -> `identity_api_tokens_test.go`,
`local_identity_handler_fakes_test.go` -> `identity_handler_fakes_test.go`,
`local_identity_handler_test.go` -> `identity_handler_test.go`,
`local_identity_password_rotation_test.go` ->
`identity_password_rotation_test.go`,
`local_identity_sign_in_policy_gate_test.go` ->
`identity_sign_in_policy_gate_test.go`, `local_identity_totp_test.go` ->
`identity_totp_test.go`. `identity_*_test.go` is fine because no `identity/`
leaf exists.

No-Regression Evidence: the `go test ./internal/query -list '.*'` and
`go test ./internal/query/local -list '.*'` name union equals this move's own
base (`81fc4f317`, main after the console test-timeout fix #6685 merged -- 2623 root names, measured directly on that
commit, not derived) minus
`TestIdentityHashMatchesUnexportedImplementation` (replaced, see above) plus
the 27 tests this move added: fifteen `handler_test.go` route tests
(`TestBootstrap`, `TestBootstrapRequiresSharedOperator`, `TestLogin`,
`TestLoginInvalidCredentialReturnsUnauthorized`,
`TestLoginMalformedBodyReturnsBadRequest`, `TestCreateInvitation`,
`TestCreateInvitationRequiresAllScopeAuth`, `TestAcceptInvitation`,
`TestResetPassword`, `TestRotatePassword`, `TestResetMFA`, `TestDisableUser`,
`TestEnableBreakGlass`, `TestBreakGlassSession`,
`TestBreakGlassSessionUnavailableReturnsUnauthorized`), six
`api_tokens_test.go` route tests (`TestListAPITokens`,
`TestListAPITokensRequiresAuthentication`, `TestCreateAPIToken`,
`TestRevokeAPIToken`, `TestRevokeAPITokenNotOwnedReturnsNotFound`,
`TestRotateAPIToken`), four `totp_test.go` route tests
(`TestBeginTOTPEnrollment`, `TestBeginTOTPEnrollmentRequiresAuthenticatedSession`,
`TestConfirmTOTPEnrollment`, `TestConfirmTOTPEnrollmentWrongCodeReturnsBadRequest`),
one `helpers_test.go` replacement test (`TestIdentityHashUsesTheSha256Prefix`),
and `TestAuditLocalIdentityStampsActorClassByAuthMode`. That is
2623 - 1 + 27 = 2649, matching the measured union of the head root list (2621
names) and this leaf's own list (28 names). No name was dropped or duplicated
beyond that one documented replacement. `go test ./internal/query/...
./cmd/api ./cmd/mcp-server ./internal/mcp ./cmd/golden-corpus-gate/...
-count=1` is green.

The `internal/query` dirgate ledger row
(`scripts/lib/dirgate-grandfather.tsv`) moved from 326 non-test files to 318
(nine files left root, one alias file arrived); `bash
scripts/verify-dirgate.sh --all` passes with no exemption row for this new
leaf directory (the alias file's own `//nolint:dirgate` marker on its
package line suppresses its own sibling-prefix hit, the same pattern every
other `*_alias.go` file in root uses).

No-Observability-Change: this move adds no span, metric, log line, worker,
queue, or runtime knob. The family emits none of its own today (no
`startQueryHandlerSpan` call in any of the nine moved files), so there is no
handler-tracing.go seam to add either, unlike the freshness/language
precedents. `docs/public/observability/telemetry-coverage.md`'s
require-SSO-login-gate row is repointed to this package's file (no line
number, following the established bare-file convention this doc already
uses for other behavior-preserving package moves) rather than the vacated
root path.

## Related docs

- [docs/public/reference/http-api.md](../../../../docs/public/reference/http-api.md)
- [go/internal/query/README.md](../README.md)
