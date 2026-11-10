# SignInPolicyReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. D13 auth-read core pin, per the arbiter
W7 ruling on #9669 (APPROVED for the 1 remaining read). Minted
core-side in eshu-hq/eshu#10432 (O6 pin 1); the satellite recorded
it byte-identical in eshu-hq/eshu-api#84. A satellite PR consuming
an unminted pin is invalid. Arbiter review of this pin precedes
satellite termination of the sign-in-policy read legs.

## Shape

Version: `SignInPolicyReadAdmissionContractVersion = "v0"`
(go/internal/query/auth/read_admission.go).

Exactly 1 re-declared (never embedded) method, sealed:

| # | Method | Mirrors (auth) | Serves |
|---|--------|----------------|--------|
| 1 | `GetSignInPolicy(context.Context, string) (auth.SignInPolicy, error)` | `auth.SignInPolicyReadStore.GetSignInPolicy` | `GET /api/v0/auth/sign-in-policy`, `GET /api/v0/auth/providers` (require_sso half), `GET /api/v0/auth/admin/sign-in-policy` |

Enforcement (all in go/internal/query/auth, plus route proof in
go/internal/query):

- `read_admission_contract_test.go` — frozen method-set test plus
  version pin (`TestSignInPolicyReadAdmissionContractIsVersioned`).
- `go/internal/query/sign_in_policy_admin_wire_shapes_test.go` —
  admin-leg wire proof: the admin GET served through `Mount` answers
  the byte-exact projection of the `SignInPolicy` value the pinned
  read returns (frozen admin key set, no credential field ever
  present), and the store-error path fails closed with the fixed
  `signInPolicyReadFailedMessage`. The shared test double satisfies
  the admission port by compile-time assertion, so the proof rides
  the pinned shape, not a parallel signature.
- Compile-time mirror assertion in the pin file:
  `var _ SignInPolicyReadAdmission = SignInPolicyReadStore(nil)`.

## Admin-leg verbatim coverage

Both sign-in-policy GETs (public and admin) and the providers
require_sso half admit through the single pinned read: each handler
calls `GetSignInPolicy` and nothing else on the store. The public
projection (`require_sso` only) and the admin projection (full
detail JSON via `signInPolicyDetailJSON`) are pure functions of the
pinned return value. No admin wire key exists outside the pinned
read's return shape: the wire-shapes test above fails on any added,
removed, or renamed admin key until this record moves with it.

## Satellite consumption

The terminated reader forwards the keyed call to the versioned core
HTTP read API with the caller credential intact (Wave-1 pattern:
shared-key gate, family authorizer, `forwardToCore` on the native
lane); core bytes come back unmodified, so the wire stays verbatim
by construction. Dest synthesizes nothing on this leg and holds no
policy state. No `WithReadStore` constructor exists on this pin: the
`IdentitySubjectStore` behind the port takes the read-write
`db.ExecQueryer` (the mutation leg shares the store), so satellite
consumption stays on the HTTP read API, never on a direct handle.

## Explicitly OUT (not widened by this pin)

- The sign-in-policy mutation (`UpsertSignInPolicy`) — covered by
  `SignInPolicyAdmission` v0 (#8872 seam), untouched here; there is
  no write method set to freeze.
- Grant checks, fail-open read conventions, and fixed failure
  messages — handler behavior, no method set to freeze.
- The providers list read (provider-config enumeration behind
  `GET /api/v0/auth/providers`) — the pin covers only the
  `require_sso` half dest needs from this port.

## Invariants held by this pin (arbiter conditions)

- Zero edits to `querycontract/ports.go`.
- Zero drift in the frozen sets (`api_boundary`,
  `read_store_contract`); no `CoreReadContractsVersion` bump.
- Unwired seam: no handler, route, or caller depends on the pin
  itself yet. Rollback = revert the pin files; rollback moves no
  traffic and changes no data.
