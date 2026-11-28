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
  `audit_actor_class_test.go`).

## Related docs

- [docs/public/reference/http-api.md](../../../../docs/public/reference/http-api.md)
- [go/internal/query/README.md](../README.md)
