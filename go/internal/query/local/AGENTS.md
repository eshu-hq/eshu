# Local identity — Agent Instructions

Scope: `go/internal/query/local/` (package `local`).

## Ownership

This leaf owns `IdentityHandler` and every route it registers: session
lifecycle (`handler.go`, `helpers.go`), generated API tokens
(`api_tokens.go`, `api_tokens_self_service.go`), self-service TOTP
enrollment (`totp.go`), the storage ports and hash-only record types
(`types.go`, `profile.go`), request bodies and small pure helpers
(`requests.go`), and the require_sso/allow_local_user_creation sign-in-policy
gates for this family's own routes (`sign_in_policy_gate.go`). See README.md
for the full file layout and Move evidence.

## Invariants

- MUST NOT import root package `query` -- root would import this package
  back for the compatibility aliases in `local_identity_alias.go`, cycling.
  Reach root-only helpers through `queryauth` (auth context, browser-session
  types, sign-in-policy read port) or `querycontract` (envelope writers,
  `ReadJSON`/`WriteJSON`/`WriteError`/`PathParam`,
  `RequirePermissionFeature`/`WritePermissionDenied`/`WriteUnauthorized`); if
  neither has what you need, it does not belong here -- ask before adding a
  new shared home.
- This family registers no capability. Do not add a `capabilities.go` or
  `main_test.go` TestMain here on the freshness/codeowners precedent -- no
  route here is gated by `querycontract`'s capability matrix. The routes
  fall into five gate classes, and a change must be reviewed against the
  class it belongs to, not the admin threat model alone:
  - public credential routes, reachable without an auth context because the
    request body carries the credential the store verifies: `login`
    (password plus TOTP or recovery code), `invitations/accept` (invite
    code), `password/rotate` (current password, re-proving MFA with a TOTP
    or recovery code when the account has an active factor) and
    `break-glass/session` (break-glass code);
  - shared-operator routes (`requireSharedOperator`): `bootstrap` and
    `break-glass` enablement;
  - all-scope plus permission-catalog routes (`requireAllScopeAuth` then
    `requirePermissionFeature`): invitation creation and the per-user
    `password`, `mfa-reset` and `disable` administration routes;
  - subject-authenticated self-service routes that act only on the caller's
    own identity (`queryauth.AuthContextFromContext` with a non-empty
    `SubjectIDHash`): TOTP enrolment (`mfa/totp/begin`, `mfa/totp/confirm`)
    and `GET api-tokens`;
  - dual-mode token mutations (`POST api-tokens`,
    `api-tokens/{token_id}/revoke`, `api-tokens/{token_id}/rotate`):
    `authorizeTokenMutation` then `requirePermissionFeature`
    (`PermissionFeatureTokens`). An all-scope caller, including the
    subject-less shared operator, takes the admin path: create for any
    active member and revoke or rotate any token, always within the
    caller's own tenant/workspace per `localIdentityAPITokenScope` (only a
    tenant-less shared operator names the tenant/workspace in the request
    body). A non-all-scope caller (which must carry a `SubjectIDHash`, else
    401) is held to its own tokens through `selfServiceTokenOwner` with a
    non-disclosing 404, and its create is further limited by
    `enforceSelfServiceTokenCreateScope` in `api_tokens_self_service.go`.

  If a future route on this handler DOES need a capability, follow the
  freshness precedent then, including the root lockstep test.
- `IdentityHash` is the single hash-only identity-field implementation
  (`"sha256:<hex>"`) for `subject_id_hash`, `profile_handle_hash`,
  recovery-code hashes, the policy revision hash, and
  `password_parameters_hash`. Before the move (#6642) it was two functions --
  this package's own unexported `localIdentityHash` doing the work, and an
  exported `IdentityHash` forwarding to it for `go/cmd/api/seed_initial_admin.go`,
  `go/cmd/api/seed_initial_admin_helpers.go`, and
  `go/internal/cli/admin/credential.go`. Naming rule 4 would drop the
  package-word stutter from `localIdentityHash` too, colliding with the
  already-exported wrapper of the exact same computation. The two were
  merged into this one function rather than carrying a second exported name
  forward (`HashForMode`-style alternatives were considered and rejected: it
  never had a "mode," only ever one hash convention). Every internal call
  site in this package calls `IdentityHash` directly now; do not reintroduce
  a private wrapper around it.
- `IssueSessionCookies`, `SessionIssued`, `IdentityHashes`, and
  `PolicyRevision` are exported because a staying root caller needs them
  through `local_identity_alias.go`'s forwarders: `IssueSessionCookies` by
  the setup wizard's final step (`setup_mfa_handler.go`), `IdentityHashes` by
  the same file, `PolicyRevision` by `sign_in_policy_mutations.go`. Do not
  rename any of the four without updating that alias file in the same
  change. `SessionIssued` specifically had to be exported purely so root's
  `issueLocalSessionCookies` forwarder function can name its own return
  type in its signature -- an unexported cross-package type can be returned
  from a qualified call (root could have written `issued, ok :=
  local.IssueSessionCookies(...)` and used `issued.Auth` etc. without ever
  naming the type), but a *forwarder function's own declared signature*
  must name every parameter and return type, and an unexported identifier
  from another package cannot appear there at all. Naming rule 4 did not
  otherwise require exporting it.
- `auditLocalIdentity` stays an unexported method deliberately (method names
  keep their pre-move spelling per the move brief). A root test that needs
  to drive it white-box CANNOT reach it through any alias: a type alias
  carries the type, but an unexported method's visibility is decided by its
  DECLARING package, so `query.LocalIdentityHandler` (an alias for
  `local.IdentityHandler`) still cannot call `.auditLocalIdentity(...)` from
  package `query`. `audit_test.go`'s
  `TestAuditLocalIdentityStampsActorClassByAuthMode` is the home for that
  proof now; do not try to restore root's old direct call by exporting the
  method -- if a future change genuinely needs to call it from outside this
  package, that is a real API decision to raise, not a naming-rule
  side effect to smuggle in.
- `requirePermissionFeature` (this package's own method, distinct from
  `querycontract.RequirePermissionFeature`) calls
  `queryauth.AllowsPermissionFeature` and `querycontract.WritePermissionDenied`
  directly, then additionally emits a `permission_catalog_denied` governance
  audit event on denial through `auditLocalIdentity`. Do not replace it with
  a bare call to `querycontract.RequirePermissionFeature`: that function
  does not audit, and every call site in this package needs the audit trail.
- `requireSSODecision` (`sign_in_policy_gate.go`) is the SINGLE authoritative
  require_sso read for both `handleLogin` and `handleRotatePassword`; do not
  add a second read site for either route (see #5001/#4976 in that file's
  doc comments for why a TOCTOU-shaped second read is the specific bug this
  guards against).
- No route here calls `startQueryHandlerSpan` or any `tracing` helper
  today. Do not add a `handler_tracing.go` speculatively; if a future route
  genuinely needs a span, follow the freshness/language precedent
  (`tracing.HandlerTracer()` seeded into a package-local var) then, not
  before.

## Test fixtures (no querytestutil hoist)

Unlike freshness's two-tenant fixtures, none of this family's pre-move test
doubles were hoisted to `querytestutil`: every one of them
(`fakeGovernanceAuditAppender`, `sequenceSecrets`, `fakeBrowserSessionStore`,
`fakeSignInPolicyReadStore`, `fakeLocalIdentityListStore`, `bodyContains`,
`fixedNow`) is shared with OTHER still-root families (setup, status), so
hoisting any of them for this move alone would mean rewriting those other
families' root tests too -- outside this move's scope. This package
therefore declares its own `fakeStore` (`handler_fakes_test.go`, an
`IdentityProfileLister` double) and `fakeSessions`
(`session.BrowserSessionStore` double) rather than reaching for root's
private fakes it structurally cannot reach anyway. Do not try to import
`querytestutil.FakeGovernanceAuditAppender` here as a `GovernanceAuditAppender`
double AND keep duplicating `fakeStore`/`fakeSessions` locally -- the
`Append`-only appender fixture genuinely is the shared home
(`audit_test.go` already uses it); the storage/session doubles are not,
because no OTHER package's tests need this family's exact storage port
shape.

## Naming

`docs/internal/naming.md` is law: no `local_identity_` file prefixes, no
`local/local_identity_handler.go`, exported identifiers lose the `Local`
stutter (see README.md's Move evidence for the `Identity` second-stutter
discussion and why the literal rule-4 rename was applied instead of
collapsing further). The root `local_identity_alias.go` keeps every old
exported spelling for staying callers. Struct fields and method names are
out of scope for the rule-4 exported-identifier check and keep their exact
pre-move spelling throughout this package.
