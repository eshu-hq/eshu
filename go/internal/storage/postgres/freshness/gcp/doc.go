// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gcpfreshnessstore persists GCP asset-inventory event-driven
// refresh triggers for later workflow handoff.
//
// GCPFreshnessStore coalesces same-FreshnessKey events into one row
// (StoreTrigger), lets the workflow coordinator claim queued triggers under
// a lease (ClaimQueuedTriggers), reclaims a lease that expired before
// handoff completed (ReapExpiredTriggerClaims, #4576), and completes a claim
// only when the caller presents the claim_fencing_token it was issued
// (MarkTriggersHandedOff/MarkTriggersFailed), so a stale claimant whose
// lease was reaped and re-claimed by another owner cannot complete a claim
// it no longer holds.
//
// This is one of the three `freshness/{aws,gcp,incident}/` trigger stores
// that share this StoreTrigger/ClaimQueuedTriggers/
// ReapExpiredTriggerClaims/MarkTriggersHandedOff/MarkTriggersFailed
// lifecycle (#6693 decision N4); `vulnerability/`'s source-state store has
// none of it and did not follow them into this split.
//
// This package must not import the parent postgres package.
package gcpfreshnessstore
