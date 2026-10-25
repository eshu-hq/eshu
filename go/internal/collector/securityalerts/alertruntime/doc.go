// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package alertruntime runs claim-driven hosted provider security-alert
// collection and returns source-truth repository alert facts.
//
// NewClaimedSource validates the configured targets and expands each org
// target through shard.Partition (#9019): a target of more than 100 same-org
// repositories is collected as "<scope_id>#shard-<bits>" scopes of at most 100,
// each with ParentScopeID set to the base scope_id. NextClaimed collects the
// scope a work item names. A retirement item (shard.IsRetirement) is answered
// with an empty generation carrying TrustedDeletion and
// RetirementFreshnessHint and a read state with repositories_total = 0; a
// claim of a sharded target's base scope without the marker fails retryable
// with ErrShardedTargetNeedsCoordinatorUpgrade.
package alertruntime
