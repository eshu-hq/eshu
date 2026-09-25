// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package incidentfreshnessstore persists PagerDuty and Jira webhook
// wake-up triggers for later workflow handoff.
//
// IncidentFreshnessStore coalesces same-FreshnessKey events into one row
// (StoreIncidentFreshnessTrigger), lets the workflow coordinator claim
// queued triggers under a lease (ClaimQueuedTriggers), and completes a
// claim only when the caller presents the claim_fencing_token it was issued
// (MarkTriggersHandedOff/MarkTriggersFailed), so a stale claimant whose
// lease was reaped and re-claimed by another owner cannot complete a claim
// it no longer holds.
//
// This is one of the three `freshness/{aws,gcp,incident}/` trigger stores
// that share this StoreTrigger/ClaimQueuedTriggers/
// MarkTriggersHandedOff/MarkTriggersFailed lifecycle (#6693 decision N4);
// `vulnerability/`'s source-state store has none of it and did not follow
// them into this split.
//
// This package must not import the parent postgres package.
package incidentfreshnessstore
