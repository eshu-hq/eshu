// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package fence is the cross-service commit fence for facts committed under a
// claim held by the extracted claim plane (eshu-workflow, issue #9376).
//
// The in-tree fence is one transaction: the ingestion commit heartbeats the
// core claim row on the same transaction that persists facts. A claim held in
// another database cannot share that transaction, so the plane path instead:
// pins the claim on the plane before BEGIN (Pin), keeps it pinned while the
// transaction runs (Keeper), records a per (generation, work item) monotonic
// fence row inside the transaction (Write), and refuses to COMMIT once the
// latest pin deadline is within the margin of the Go clock (CheckDeadline).
// A writer that was reaped and reclaimed at a higher token is refused by the
// pin, the re-pin, the fence row, or the deadline, whichever sees it first.
//
// Fences of different work items on one generation are incomparable: tokens
// are per work item, and terraform-state can commit one resolved generation
// from two items. Activation is unchanged; nothing here runs at projector Ack.
// The package holds no telemetry; the ingestion store records the rejection
// counter, the slack histogram, and the re-pin outcomes around these calls.
package fence
