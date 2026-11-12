# WorkflowRetireUnclaimedWriterAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Core mint per the arbiter ruling on #9669
(APPROVED-WITH-CONDITIONS): a single-method RetireUnclaimedAdmission v0
pin, minted first as an unwired seam with a frozen contract test.
Zero runtime behavior change: no handler, route, port, store, SQL,
OpenAPI, or wiring edit ships with this record. A satellite PR
consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the workflow-retire-unclaimed leg
(eshu-hq/eshu-api#103, tracker eshu-hq/eshu#10491).

## Shape

Version: `RetireUnclaimedAdmissionContractVersion = "v0"`
(go/internal/query/admin/retire_unclaimed_admission.go).

Exactly 1 re-declared (never embedded) method, sealed:

| # | Method | Mirrors (store) | Serves |
|---|--------|-----------------|--------|
| 1 | `RetireUnclaimedWorkflowItems(context.Context, string, string, string) ([]RetiredUnclaimedItem, error)` | `Store.RetireUnclaimedWorkflowItems` | `POST /api/v0/admin/workflow-retire-unclaimed` |

Enforcement (all in go/internal/query/admin):

- `retire_unclaimed_admission_contract_test.go` — frozen method-set
  test plus version pin
  (`TestRetireUnclaimedAdmissionContractIsVersioned`).
- `retire_unclaimed_admission_wire_shapes_test.go` — wire proof: the
  shared test double satisfies the admission port by compile-time
  assertion (`var _ RetireUnclaimedAdmission = (*stubAdminStore)(nil)`),
  so the proof rides the pinned shape, not a parallel signature. It
  freezes the 200 order/count/keys, the empty no-op, the 404 pair
  naming, the fixed 500 message without error leakage, and the no-audit
  set (400, 503, 500), alongside the pre-existing
  `retire_unclaimed_test.go` allow/deny audit pins.
- Compile-time mirror assertion in the pin file
  (`RetireUnclaimedAdmission = Store`).

## Wire verbatim coverage

The admin leg admits through the single pinned write: the handler calls
`RetireUnclaimedWorkflowItems` and nothing else on the store, trims
both identity fields, and serves `{count, items}` in the port's
`work_item_id` order with exactly the `work_item_id`/`scope_id` keys.
Sentinels arrive wrapped as `"%w: kind/instance"`, so `errors.Is`
still matches while the 404/409 body names the refused pair. The
response count includes only rows this call transitioned, at most 100
per call; a repeat call retires the residue or nothing. The operator
note is trimmed and truncated to 500 runes in storage without
splitting a rune. Eligibility is pending + `attempt_count = 0` + no
current claim for a known deconfigured (disabled or deactivated)
instance, with the gate probe plus the in-snapshot deconfiguration
recheck. The mutation is state-idempotent, not exactly-once:
concurrent duplicates both execute against disjoint row sets, and a
duplicate submit returns the residue or a zero count — never a replayed
outcome. No idempotency key, ledger, or detached bounded finalize is
required or permitted by this pin.

## Audit reason-code matrix (logged vs discarded)

Logged — one governance audit event per attempt, allowed or denied,
appended via `recordRecoveryAction` inline on the request context with
`SafeCorrelationID` (single author: core):

| Path | Decision | Reason |
|------|----------|--------|
| Success (200) | allowed | `retire_unclaimed_accepted` |
| Unknown instance (404) | denied | `retire_unclaimed_refused_unknown_instance` |
| Still-configured instance (409) | denied | `retire_unclaimed_refused_instance_active` |

Discarded — no governance audit event: the 400 validation path, the 503
nil-store path, and the 500/499 store-failure path. The satellite must
preserve this set, not add audits.

## Explicitly OUT (not widened by this pin)

- `Store`-as-a-whole, `ReadStore`, `RecoveryService`,
  `audit.Appender` (named, not embedded), `Clock`/`FinalizeTimeout`
  (handler fields, not ports).
- `storage/postgres` `WorkflowControlStore` and its sentinels —
  core-internal; the admin wrapper maps them onto the admin sentinels.
- All sibling admin reads and mutations — each keeps its own pin.
- Any traffic flip, deploy promotion, or in-tree removal — none is
  authorized by this ruling.

## Invariants held by this pin (arbiter conditions)

- Zero edits to handler, store wrapper, `WorkflowControlStore`/SQL,
  OpenAPI fragment, versions, or wiring.
- Zero drift in the frozen sets; no other contract version bumped.
- Unwired seam: no handler, route, or caller depends on the pin yet.
  Rollback = revert the pin files; rollback moves no traffic and
  changes no data.

## Satellite consumption (after merge, satellite-side only)

The terminated writer forwards the keyed POST to the versioned core
HTTP mutation API with the caller credential and correlation intact
(Wave-1 pattern: shared-key gate, family authorizer, `forwardToCore`
on the native lane); core bytes come back unmodified, so the wire stays
verbatim by construction. Dest synthesizes nothing on this leg and
holds no workflow state (never a direct DB/table/SQL touch). Exactly
one forward attempt per POST — including on 5xx/timeout/cancel, since
a retry double-audits and recounts. Duplicate submit stays
no-worse-than-core (sequential dup returns residue/0, concurrent dups
return disjoint id sets whose union equals the core single-run total).
Byte-parity dest-vs-core holds for 200/400/404/409/500/503 including
bodies naming kind/instance and items order. The satellite labels the
leg honestly ("terminated writer via core-owned Postgres workflow
control store; state-idempotent, not exactly-once; ≤100 rows/call")
and rehearses revert-to-relay (disable termination → relay serves the
route, no data migration, no core change). Nothing satellite-side is
implemented by this pin.
