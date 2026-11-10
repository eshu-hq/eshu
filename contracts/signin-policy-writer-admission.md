# SignInPolicyWriterAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. W7 writers core record, per the arbiter
ruling on #9669 (APPROVED-WITH-CONDITIONS): NO new pin — this
record covers the ONE already-wired v0 writer pin the admin
sign-in-policy PATCH leg travels through. Zero runtime behavior
change: no handler, route, port, or store edit ships with this
record. A satellite PR consuming an unminted pin is invalid.
Arbiter review of this pin precedes satellite termination of the
sign-in-policy mutation leg.

## Shape

Version: `SignInPolicyAdmissionContractVersion = "v0"`
(go/internal/query/sign_in_policy_types.go:63).

Exactly 1 re-declared (never embedded) method, sealed:

| # | Method | Mirrors (store) | Serves |
|---|--------|-----------------|--------|
| 1 | `UpsertSignInPolicy(context.Context, string, SignInPolicyUpdateRequest, string, time.Time) (SignInPolicy, error)` | `SignInPolicyMutationStore.UpsertSignInPolicy` | `PATCH /api/v0/auth/admin/sign-in-policy` |

Enforcement (all in go/internal/query):

- `sign_in_policy_admission_contract_test.go` — frozen method-set
  test plus version pin
  (`TestSignInPolicyAdmissionContractIsVersioned`).
- `sign_in_policy_writer_wire_shapes_test.go` — PATCH-leg wire
  proof: the mutation served through `SignInPolicyMutationHandler.Mount`
  answers the byte-exact projection of the `SignInPolicy` value the
  pinned writer returns (shared `signInPolicyDetailJSON`, no key
  outside the pinned return shape), the store-side guardrail and
  timeout-ordering sentinels map to 400 with their own denied audit
  reasons, the permission catalog (403) and all-scope checks (403)
  run before the writer, the nil port fails closed with 503, and
  every path emits its audit decision plus reason. The shared test
  double satisfies the admission port by compile-time assertion, so
  the proof rides the pinned shape, not a parallel signature.
- Compile-time mirror assertion in the pin file
  (`SignInPolicyAdmission = SignInPolicyMutationStore`).

## PATCH-leg verbatim coverage

The admin PATCH admits through the single pinned write: the handler
calls `UpsertSignInPolicy` and nothing else on the store, and serves
the byte-exact `signInPolicyDetailJSON` projection of the returned
value. The served bytes equal the shared projection encoding, so a
dest consumer forwarding core bytes serves this leg verbatim by
construction. The upsert is atomic: the require_sso guardrail and
the merged idle/absolute timeout-ordering check run INSIDE the
store's row-locked transaction, never as a handler-side
pre-transaction read (see `SignInPolicyMutationStore`'s doc
comment) — two concurrent partial PATCHes cannot both pass on the
same stale value.

## Satellite consumption

The terminated writer forwards the keyed PATCH to the versioned
core HTTP mutation API with the caller credential intact (Wave-1
pattern: shared-key gate, family authorizer, `forwardToCore` on the
native lane); core bytes come back unmodified, so the wire stays
verbatim by construction. Dest synthesizes nothing on this leg and
holds no policy state. No `WithReadStore` constructor exists on
this pin: the `IdentitySubjectStore` behind the port takes the
read-write `db.ExecQueryer`, so satellite consumption stays on the
HTTP mutation API, never on a direct handle.

## Exactly-once matrix (cross-linked with setup-writer-admission)

Verbatim for both writer records:

| Leg | Replay semantic |
|-----|-----------------|
| PATCH sign-in-policy upsert | Atomic upsert: guardrail plus timeout ordering enforced inside the store transaction; concurrent partial PATCHes serialize on the row lock. |
| Setup claim (`/setup/claim`) | Replayable: reproves the unconsumed bootstrap credential, persists nothing. |
| Setup admin (`/setup/admin`) | Repeatable pre-seal: rotates the password on every call while the wizard stays open. |
| Setup MFA seal (`/setup/mfa`) | Exactly once ONLY here: `CompleteSetupMFA` atomically rotates MFA and consumes the bootstrap credential; a concurrent loser answers 409 and its codes are discarded. |

## Audit reason-code matrix (logged vs discarded)

Logged — one governance audit event per attempt, allowed or denied:

| Path | Decision | Reason |
|------|----------|--------|
| PATCH success | allowed | `sign_in_policy_updated` |
| PATCH nil port (503) | denied | `sign_in_policy_store_unavailable` |
| PATCH catalog denial (403) | denied | `permission_catalog_denied` |
| PATCH non-all-scope (403) | denied | `admin_scope_required` |
| PATCH tenantless (403) | denied | `admin_tenant_required` |
| PATCH malformed JSON (400) | denied | `sign_in_policy_invalid_request` |
| PATCH timeout shape (400) | denied | `sign_in_policy_invalid_timeout` |
| PATCH no proven provider (400) | denied | `sign_in_policy_guardrail_no_provider` |
| PATCH no SSO admin proof (400) | denied | `sign_in_policy_guardrail_no_sso_proof` |
| PATCH store timeout ordering (400) | denied | `sign_in_policy_invalid_timeout` |
| PATCH unexpected store error (500) | denied | `sign_in_policy_update_failed` |

Discarded — no audit event: none on this leg. Every PATCH attempt,
including shape-only rejections, is logged (contrast the setup
record, where shape-only and sealed-path rejections are discarded).

## Explicitly OUT (not widened by this pin)

- The sign-in-policy reads (`GetSignInPolicy`) — covered by
  `SignInPolicyReadAdmission` v0
  ([signin-policy-read-admission](signin-policy-read-admission.md)),
  untouched here.
- Grant checks, fail-open read conventions, and fixed failure
  messages — handler behavior, no method set to freeze.
- The `/setup/mfa` session issuance (`BrowserSessionStore`):
  sessions are issued BEFORE the seal through a separate port and
  are explicitly out of the admission shape (see the setup record).

## Invariants held by this pin (arbiter conditions)

- Zero edits to `nativeNowWave`/`ReadTransportContractsVersion`.
- Zero edits to `querycontract` ports and `auth/read_admission.go`.
- Zero edits to method sets, versions, handler behavior, storage, or
  `wiring_router.go`.
- Zero drift in the frozen sets; no `CoreReadContractsVersion` bump.
- Already wired seam: the handler already carries this port via
  `newSignInPolicyMutationHandler`; the record moves no traffic and
  changes no data. Rollback = revert the record files.
