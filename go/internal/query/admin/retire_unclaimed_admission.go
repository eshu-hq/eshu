// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import "context"

// RetireUnclaimedAdmissionContractVersion versions the #9669 writer
// admission-contract shape for the never-claimed workflow retirement the
// admin workflow-retire-unclaimed endpoint admits through
// Store.RetireUnclaimedWorkflowItems (POST
// /api/v0/admin/workflow-retire-unclaimed). Canonical mutations persist
// through explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned Postgres store
// satisfies. A breaking change to the mutation shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
const RetireUnclaimedAdmissionContractVersion = "v0"

// RetireUnclaimedAdmission is the versioned admission-contract port the API
// handler depends on instead of a direct store handle. It narrows Store to
// the single mutation the workflow-retire-unclaimed endpoint admits
// through, so the boundary is identity-verified by the frozen contract test
// rather than a parallel hand-maintained shape; it will back the eshu-api
// extraction handoff for this operation.
//
// Satellite consumer: Wave-1 record-then-terminate on
// POST /api/v0/admin/workflow-retire-unclaimed, per the arbiter ruling on
// #9669 (APPROVED-WITH-CONDITIONS). The core pin PR merges before any
// satellite consumption; a satellite PR consuming an unminted pin is
// invalid. Arbiter review of the audit/concurrency semantics below
// precedes termination; termination comes last among mutations, only on
// D0-proven transport.
//
// In scope (POST /api/v0/admin/workflow-retire-unclaimed admits through
// exactly this):
//
//   - RetireUnclaimedWorkflowItems retires never-claimed pending workflow
//     work items for one deconfigured collector instance: rows with status
//     pending, attempt_count 0, and no current claim move to
//     failed_terminal with the operator-retired failure class. The
//     response count includes only rows this call transitioned, in
//     work_item_id order, at most 100 per call; an empty result is a safe
//     no-op. The core-owned Postgres store satisfies it.
//
// Wire freeze (method-set freeze alone is insufficient here because
// sentinel text reaches the wire through WriteError err.Error()):
//
//   - Request JSON: collector_kind and collector_instance_id (required,
//     trimmed, 400 when either is empty) plus operator_note (optional,
//     trimmed; truncated to 500 runes in storage, never split mid-rune).
//   - Response 200: {count, items[{work_item_id, scope_id}]} in
//     work_item_id order, counting only rows this call transitioned.
//   - Sentinels: ErrRetireUnclaimedUnknownInstance and
//     ErrRetireUnclaimedInstanceActive, wrapped with "%w: kind/instance"
//     so errors.Is still matches while the 404/409 body names the pair.
//   - Status map: 404 unknown instance, 409 still-configured instance,
//     400 validation, 500 unexpected store fault through the fixed
//     retireUnclaimedFailedMessage via writeMutationFailure (499 on
//     cancel), 503 "admin store not configured" on a nil store.
//   - Eligibility: pending + attempt_count 0 + no current claim for a
//     known deconfigured (disabled or deactivated) instance, with the
//     gate probe plus the in-snapshot deconfiguration recheck, so a
//     reactivation between gate and retire retires nothing.
//   - Concurrency: per-row lock plus predicate recheck gives every row
//     exactly one winner versus a concurrent claim; the mutation is
//     state-idempotent, not exactly-once.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - Store-as-a-whole stays out: pinning it would grant the replay,
//     reopen, dead-letter, backfill, and skip mutations this operation
//     never calls — a blanket pin, which the arbiter ruling forbids.
//   - ReadStore stays out: the mutation performs no bounded-list read.
//   - RecoveryService stays out: refinalize and recover-generations are
//     different operations with their own pin.
//   - audit.Appender stays out: it is the shared cross-family audit port
//     (replay, reopen, recover-generations, identity and provider
//     mutations all append through it). Pinning it here would be a blanket
//     pin; the audit freeze below names it instead of embedding it.
//   - Clock and FinalizeTimeout are handler fields, not ports, so there
//     is no method set to freeze; the audit append below runs inline on
//     the request context with audit.AppendTimeout, like the skip's deny
//     path.
//   - storage/postgres WorkflowControlStore and its sentinels stay out:
//     they are core-internal. The admin wrapper maps them onto the admin
//     sentinels above so the handler stays on the admin port.
//   - All sibling admin reads and mutations stay out: each has its own
//     pin.
//
// Idempotency obligation (the replay/ledger protection this operation does
// NOT get — deliberately, per the arbiter ruling): the endpoint claims no
// idempotency key and holds no ledger row, and runs no detached bounded
// finalize, because the mutation is already state-idempotent: a re-run
// touches only rows still pending with no claim, so concurrent duplicate
// requests both execute against disjoint row sets and a duplicate submit
// returns the residue or a zero count, never a replayed outcome. Copying a
// ledger obligation onto this writer would change core semantics (the
// first count would replay instead of draining). NO new idempotency key,
// ledger, or detached bounded finalize is required or permitted by this
// pin. The pin states "state-idempotent, not exactly-once"; concurrent
// duplicates both execute.
//
// Audit freeze: logged is exactly 3 governance events, each appended via
// recordRecoveryAction inline on the request context with
// audit.SafeCorrelationID — allowed retire_unclaimed_accepted on success,
// denied retire_unclaimed_refused_unknown_instance on 404, denied
// retire_unclaimed_refused_instance_active on 409. Discarded (no event)
// is exactly 3 paths: 400 validation, 503 nil-store, and 500/499 store
// failure emit NO governance audit today, and the satellite must preserve
// that set, not add audits. Single-author rule: the forwarded termination
// leaves core as the sole audit author.
//
// Telemetry freeze: the eshu_dp_workflow_unclaimed_retired_total counter
// increments only on a nonzero count, labeled by (collector_kind,
// collector_instance_id) — scope ids never become labels.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type RetireUnclaimedAdmission interface {
	RetireUnclaimedWorkflowItems(ctx context.Context, kind, instanceID, note string) ([]RetiredUnclaimedItem, error)
}

var _ RetireUnclaimedAdmission = Store(nil)
