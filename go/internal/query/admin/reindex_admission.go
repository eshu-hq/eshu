// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

// ReindexAdmissionContractVersion versions the #9669 D7
// admission-contract shape for the operator reindex mutation the admin
// reindex endpoint admits through its fleet and repository-scoped ports
// (POST /api/v0/admin/reindex). Canonical mutations persist through
// explicit core-owned admission contracts: eshu-api keeps the HTTP shape
// and calls this versioned port, whose members the core-owned handlers
// satisfy. A breaking change to the mutation shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
const ReindexAdmissionContractVersion = "v0"

// ReindexAdmission is the versioned admission-contract port the API handler
// depends on instead of direct port handles. It embeds exactly the narrow
// ports the reindex endpoint admits through, so the boundary is
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
// In scope (POST /api/v0/admin/reindex admits through exactly these,
// branch by scope):
//
//   - RequestReindex records the fleet-wide reindex watermark for the
//     default workspace scope. The core-owned runtime status-request
//     handler satisfies it.
//   - MatchRepositories resolves each repository-scoped selector to its
//     exact catalog match. The query ContentReader implements it; the
//     endpoint admits through it only to resolve selectors before
//     recording anything.
//   - RequestRepositoryReindex records one watermark per distinct
//     resolved scope. The core-owned Postgres maintenance store
//     satisfies it.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - The reindex-progress read on the sibling path (GET
//     /api/v0/admin/reindex/progress via ReindexProgressReader) stays
//     out: it is a read-only progress view, not this mutation. A later
//     read wave may pin it deliberately; this lane does not.
//   - Store-as-a-whole and ReadStore stay out: the operation never
//     touches fact_work_items — no queue read, no queue write.
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
// lacks — arbiter Decision 5, which is why C-c2 excluded it): the reindex
// endpoint claims no idempotency key and holds no ledger row, appends no
// audit event, and runs no bounded finalize. A re-run is watermark-safe —
// the fleet upsert never lowers the watermark and the repository-scoped
// path records nothing unless every selector resolves — but concurrent
// duplicate requests both execute with no ledger to serialize them and no
// audit event naming what was requested. Dest termination MUST, before it
// stops relaying: mint a per-request idempotency key claimed core-side
// before any watermark commits (so a retried POST replays the recorded
// outcome instead of re-executing), append the allow/deny audit event via
// audit.Appender, and bound the finalize/audit work the way
// Handler.FinalizeTimeout bounds the replay/reopen finalize (default 2s
// via defaultFinalizeTimeout). Until that lands and the arbiter reviews
// it, the route stays relay-only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type ReindexAdmission interface {
	ReindexRequester
	RepositoryCatalogMatcher
	RepositoryReindexRequester
}
