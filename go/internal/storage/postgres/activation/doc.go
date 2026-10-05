// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package activation stores the durable exact-generation activation
// obligations of issue #7584 in the activation_obligations table.
//
// A quiet repository generation can be activated by ProjectorQueue.Ack after
// the ingester's last deferred-maintenance pass already ran. Nothing then
// publishes that generation's backward_evidence_committed phase, and every
// deployment_mapping row for it waits on the not-ready retry schedule. The
// obligation makes the gap durable: Insert runs inside the Ack transaction,
// so an obligation exists exactly when an activation commits, and a leased
// consumer in the resolution engine settles it.
//
// Store.Claim leases the oldest open obligation (FOR NO KEY UPDATE SKIP
// LOCKED, database clock, lease owner plus a claim_token that increases on
// every claim). Store.Finalize settles a claimed obligation in one
// transaction, taking the scope row before the obligation row (the Ack lock
// order): it retires the obligation as obsolete when the scope moved to
// another generation, refuses when the exact generation's own phase is
// absent, wakes up to WakeBatchLimit waiting deployment_mapping rows of that
// exact generation, keeps the obligation open while a handler for the
// generation is still claimed or running or more waiting rows remain, and
// completes it under the lease fence otherwise. Finalize never publishes a
// phase and never runs maintenance; the caller runs maintenance between
// Claim and Finalize through its own port.
//
// Store.CatchUp owes obligations, one bounded keyset page of scopes at a
// time, to active repository generations that have neither an obligation
// nor their phase. Store.Prune deletes finished obligations older than a
// retention window, oldest first, bounded per call. Store.Stats reads the
// per-state census and the oldest open obligation's age for gauges.
//
// The package imports only the db contracts, never the parent postgres
// package, so ProjectorQueue.Ack can call Insert.
package activation
