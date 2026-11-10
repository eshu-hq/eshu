# SetupWriterAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. W7 writers core record, per the arbiter
ruling on #9669 (APPROVED-WITH-CONDITIONS): NO new pin — this
record covers the ONE already-wired v0 writer pin the first-run
setup wizard legs travel through. Zero runtime behavior change:
no handler, route, port, or store edit ships with this record. A
satellite PR consuming an unminted pin is invalid. Arbiter review
of this pin precedes satellite termination of the setup legs.

## Shape

Version: `SetupAdmissionContractVersion = "v0"`
(go/internal/query/setup_types.go:27).

Exactly 5 re-declared (never embedded) methods, sealed:

| # | Method | Mirrors (store) | Serves |
|---|--------|-----------------|--------|
| 1 | `SetupNeeded(context.Context) (bool, error)` | `SetupStore.SetupNeeded` | `GET /api/v0/auth/setup-state` plus the re-check every mutating leg runs |
| 2 | `VerifyBootstrapCredential(context.Context, string, string) (bool, error)` | `SetupStore.VerifyBootstrapCredential` | `POST /api/v0/auth/setup/claim`, `/setup/admin`, `/setup/mfa` (every leg reproves) |
| 3 | `ResolveSetupOwner(context.Context) (SetupOwner, error)` | `SetupStore.ResolveSetupOwner` | `POST /api/v0/auth/setup/admin`, `POST /api/v0/auth/setup/mfa` |
| 4 | `RotateSetupPassword(context.Context, LocalIdentityPasswordReset) error` | `SetupStore.RotateSetupPassword` | `POST /api/v0/auth/setup/admin` |
| 5 | `CompleteSetupMFA(context.Context, CompleteSetupMFAInput) (bool, error)` | `SetupStore.CompleteSetupMFA` | `POST /api/v0/auth/setup/mfa` (seal) |

Enforcement (all in go/internal/query):

- `setup_admission_contract_test.go` — frozen method-set test plus
  version pin (`TestSetupAdmissionContractIsVersioned`).
- `setup_writer_wire_shapes_test.go` — claim/admin/mfa wire proof:
  the wizard served through `SetupHandler.Mount` fails closed with
  410 once sealed, answers the indistinguishable 401 on every wrong
  guess, answers 409 for the concurrent loser with no completion
  claim and no codes, emits the winner's recovery codes exactly
  once while persisting hashes only, never seals when session
  issuance fails, issues the session before the seal, and emits the
  audit reason per path. The shared test double satisfies the
  admission port by compile-time assertion, so the proof rides the
  pinned shape, not a parallel signature.
- Compile-time mirror assertion in the pin file
  (`SetupAdmission = SetupStore`).

## Claim/admin/mfa-leg verbatim coverage

Every mutating leg re-checks `SetupNeeded` and reproves the
submitted bootstrap credential against the still-unconsumed sealed
envelope on every call — the envelope itself is the continuity
proof across the wizard's three steps, so no session, ticket, or
token state exists to forward. A dest consumer forwards the three
POST bodies and returns core bytes unmodified.

## Satellite consumption

The terminated writer forwards the keyed claim/admin/mfa calls to
the versioned core HTTP setup API with the caller credential
intact (Wave-1 pattern: shared-key gate, family authorizer,
`forwardToCore` on the native lane); core bytes come back
unmodified, so the wire stays verbatim by construction. Dest
synthesizes nothing on these legs and holds no bootstrap state.
No `WithReadStore` constructor exists on this pin: the
`IdentitySubjectStore` behind the port takes the read-write
`db.ExecQueryer`, so satellite consumption stays on the HTTP setup
API, never on a direct handle.

## Exactly-once matrix (cross-linked with signin-policy-writer-admission)

Verbatim for both writer records:

| Leg | Replay semantic |
|-----|-----------------|
| PATCH sign-in-policy upsert | Atomic upsert: guardrail plus timeout ordering enforced inside the store transaction; concurrent partial PATCHes serialize on the row lock. |
| Setup claim (`/setup/claim`) | Replayable: reproves the unconsumed bootstrap credential, persists nothing. |
| Setup admin (`/setup/admin`) | Repeatable pre-seal: rotates the password on every call while the wizard stays open. |
| Setup MFA seal (`/setup/mfa`) | Exactly once ONLY here: `CompleteSetupMFA` atomically rotates MFA and consumes the bootstrap credential; a concurrent loser answers 409 and its codes are discarded. |

## Audit reason-code matrix (logged vs discarded)

Logged — one governance audit event per credential-verified
attempt, allowed or denied:

| Path | Decision | Reason |
|------|----------|--------|
| Claim success | allowed | `setup_claimed` |
| Claim wrong credential (401) | denied | `setup_claim_invalid` |
| Claim store error (500) | denied | `setup_claim_error` |
| Admin success | allowed | `setup_admin_password_set` |
| Admin/MFA wrong credential (401) | denied | `setup_admin_invalid` / `setup_mfa_invalid` |
| Admin/MFA store error (500) | denied | `setup_admin_error` / `setup_mfa_error` |
| Admin/MFA owner unresolvable (500) | denied | `setup_admin_owner_unresolved` / `setup_mfa_owner_unresolved` |
| MFA concurrent loser (409) | denied | `setup_mfa_concurrent_completion` |
| MFA winner | allowed | `setup_completed` |

Discarded — no audit event (logged-vs-discarded asymmetry): the
shape-only password-floor rejection (400, never even reproves the
credential), the sealed 410 path on every mutating leg (steady
state, not an operator action), and the read-only state check.

## Explicitly OUT (not widened by this pin)

- The `/setup/mfa` session issuance (`BrowserSessionStore`): the
  wizard issues the browser session BEFORE the irreversible seal
  through a separate port (`Sessions`, built by
  `newBrowserSessionStore`), so a session failure leaves the wizard
  reachable and the seal never runs. Session lifecycle is not part
  of the admission shape.
- Grant checks, fail-open read conventions, and fixed failure
  messages — handler behavior, no method set to freeze.
- The sign-in-policy mutation (`UpsertSignInPolicy`) — covered by
  `SignInPolicyAdmission` v0
  ([signin-policy-writer-admission](signin-policy-writer-admission.md)),
  untouched here.

## Invariants held by this pin (arbiter conditions)

- Zero edits to `nativeNowWave`/`ReadTransportContractsVersion`.
- Zero edits to `querycontract` ports and `auth/read_admission.go`.
- Zero edits to method sets, versions, handler behavior, storage, or
  `wiring_router.go`.
- Zero drift in the frozen sets; no `CoreReadContractsVersion` bump.
- Already wired seam: the handler already carries this port via
  `newSetupHandler`; the record moves no traffic and changes no
  data. Rollback = revert the record files.
