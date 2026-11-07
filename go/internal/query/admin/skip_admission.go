// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import "context"

// SkipAdmissionContractVersion versions the #9669 D7 admission-contract
// shape for the operator skip mutation the admin skip endpoint admits
// through Store.SkipRepositoryWorkItems (POST /api/v0/admin/skip).
// Canonical mutations persist through explicit core-owned admission
// contracts: eshu-api keeps the HTTP shape and calls this versioned port,
// which the core-owned Postgres store satisfies. A breaking change to the
// mutation shape fails the frozen method-set test until that list is
// deliberately updated; bump this version alongside (manually — the bump is
// convention, the frozen list is the enforcement) so dest consumers can pin
// the shape.
const SkipAdmissionContractVersion = "v0"

// SkipAdmission is the versioned admission-contract port the API handler
// depends on instead of a direct store handle. It narrows Store to the
// single mutation the skip endpoint admits through, so the boundary is
// identity-verified by the frozen contract test rather than a parallel
// hand-maintained shape; it will back the eshu-api extraction handoff for
// this operation.
//
// D-wave consumer: D7 (admin four, per the arbiter D-wave ruling on #9669).
// The satellite records this pin in a follow-up; a satellite PR consuming
// an unminted pin is invalid. Arbiter review of the idempotency/audit
// semantics below precedes tier-C dispatch clones; termination comes last
// among mutations, only on D0-proven transport.
//
// In scope (POST /api/v0/admin/skip admits through exactly this):
//
//   - SkipRepositoryWorkItems dead-letters one repository's pending,
//     retrying, or failed work items. The core-owned Postgres store
//     satisfies it.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - There is no sibling read route on this path, so no read port is
//     frozen here.
//   - Store-as-a-whole stays out: pinning it would grant the replay,
//     reopen, dead-letter, and backfill mutations this operation never
//     calls — a blanket pin, which the arbiter ruling forbids.
//   - ReadStore stays out: the mutation performs no bounded-list read.
//   - RecoveryService stays out: refinalize and recover-generations are
//     different operations with their own pin.
//   - audit.Appender stays out: it is the shared cross-family audit port
//     (replay, reopen, recover-generations, identity and provider
//     mutations all append through it). Pinning it here would be a blanket
//     pin; the audit behavior below names it instead of embedding it.
//   - Clock and FinalizeTimeout are handler fields, not ports, so there is
//     no method set to freeze; the bounds they imply are stated as
//     obligations below.
//
// Idempotency/audit obligation (the replay/ledger protection this operation
// lacks — arbiter Decision 5, which is why C-c2 excluded it): the skip
// endpoint claims no idempotency key and holds no ledger row, and runs no
// bounded finalize. A re-run touches only rows still pending, retrying, or
// failed, so the mutation is state-idempotent, but concurrent duplicate
// requests both execute with no ledger to serialize them. The only audit
// this operation emits today is the deny path: an ambiguous scope selector
// appends skip_refused_ambiguous_scope via audit.Appender with OccurredAt
// from the handler Clock and the append bounded by audit.AppendTimeout —
// there is no allow-path audit event naming what was skipped. Dest
// termination MUST, before it stops relaying: mint a per-request
// idempotency key claimed core-side before the update commits (so a
// retried POST replays the recorded outcome instead of re-executing),
// append the allow-path audit event via audit.Appender alongside the
// existing deny-path event, and bound the finalize/audit work the way
// Handler.FinalizeTimeout bounds the replay/reopen finalize (default 2s
// via defaultFinalizeTimeout). Until that lands and the arbiter reviews
// it, the route stays relay-only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type SkipAdmission interface {
	SkipRepositoryWorkItems(ctx context.Context, repoID string, note string) ([]WorkItem, error)
}

var _ SkipAdmission = Store(nil)
