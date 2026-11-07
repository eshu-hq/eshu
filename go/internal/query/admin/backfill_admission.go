// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import "context"

// BackfillAdmissionContractVersion versions the #9669 D7
// admission-contract shape for the operator backfill mutation the admin
// backfill endpoint admits through Store.RequestBackfill (POST
// /api/v0/admin/backfill). Canonical mutations persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and calls
// this versioned port, which the core-owned Postgres store satisfies. A
// breaking change to the mutation shape fails the frozen method-set test
// until that list is deliberately updated; bump this version alongside
// (manually — the bump is convention, the frozen list is the enforcement)
// so dest consumers can pin the shape.
const BackfillAdmissionContractVersion = "v0"

// BackfillAdmission is the versioned admission-contract port the API
// handler depends on instead of a direct store handle. It narrows Store to
// the single mutation the backfill endpoint admits through, so the
// boundary is identity-verified by the frozen contract test rather than a
// parallel hand-maintained shape; it will back the eshu-api extraction
// handoff for this operation.
//
// D-wave consumer: D7 (admin four, per the arbiter D-wave ruling on #9669).
// The satellite records this pin in a follow-up; a satellite PR consuming
// an unminted pin is invalid. Arbiter review of the idempotency/audit
// semantics below precedes tier-C dispatch clones; termination comes last
// among mutations, only on D0-proven transport.
//
// In scope (POST /api/v0/admin/backfill admits through exactly this):
//
//   - RequestBackfill records one durable operator backfill request. The
//     core-owned Postgres store satisfies it.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - There is no sibling read route on this path, so no read port is
//     frozen here.
//   - Store-as-a-whole stays out: pinning it would grant the replay,
//     reopen, dead-letter, and skip mutations this operation never calls
//     — a blanket pin, which the arbiter ruling forbids.
//   - ReadStore stays out: the operation performs no bounded-list read.
//   - RecoveryService stays out: refinalize and recover-generations are
//     different operations with their own pin.
//   - audit.Appender stays out: it is the shared cross-family audit port
//     (replay, reopen, recover-generations, identity and provider
//     mutations all append through it). Pinning it here would be a blanket
//     pin; the audit obligation below names it instead of embedding it.
//   - Clock and FinalizeTimeout are handler fields, not ports, so there is
//     no method set to freeze; the finalize bound they imply is stated as
//     an obligation below.
//
// Idempotency/audit obligation (the replay/ledger protection this operation
// lacks — arbiter Decision 5, which is why C-c2 excluded it): the backfill
// endpoint claims no idempotency key and holds no ledger row, appends no
// audit event, and runs no bounded finalize. A re-run records a second
// backfill request row — the mutation is not even state-idempotent. Dest
// termination MUST, before it stops relaying: mint a per-request
// idempotency key claimed core-side before the insert commits (so a
// retried POST replays the recorded outcome instead of inserting again),
// append the allow/deny audit event via audit.Appender, and bound the
// finalize/audit work the way Handler.FinalizeTimeout bounds the
// replay/reopen finalize (default 2s via defaultFinalizeTimeout). Until
// that lands and the arbiter reviews it, the route stays relay-only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type BackfillAdmission interface {
	RequestBackfill(ctx context.Context, input BackfillInput) (*BackfillRequest, error)
}

var _ BackfillAdmission = Store(nil)
