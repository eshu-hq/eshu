// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awsfreshnessplanner plans targeted AWS collector workflow rows from
// claimed AWS freshness triggers without contacting AWS.
//
// WorkPlanner validates one enabled, claim-capable AWS collector instance and
// its coalesced trigger batch, coalesces the triggers into unique
// (account_id, region, service_kind) targets sorted by freshness key, rejects
// any target the instance's configured target scopes do not authorize, and
// returns one deterministic webhook-triggered run plus one claimable work item
// per target.
//
// ParseTargetScopes and TargetAuthorized are exported for root coordinator
// code that shares this decision: service_aws_freshness.go routes a claimed
// trigger to the instance allowed to collect it, and the unextracted scheduled
// AWS planner (aws_scheduled_scheduler.go) plans from the same target_scopes
// array. The parent coordinator owns scheduling order, the plan-key clock,
// trigger claim leases, durable open-target admission, retries, and telemetry;
// this package resolves no credentials and makes no AWS API call.
package awsfreshnessplanner
