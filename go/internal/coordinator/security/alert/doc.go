// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package alert plans workflow rows for configured provider security
// alert targets.
//
// WorkPlanner validates one enabled, claim-capable collector instance and
// returns deterministic workflow rows without resolving credentials or calling
// a provider. A repository target is one work item. An org target is one work
// item per shard of its partition (shard.Partition, #9019): the base scope_id
// when it holds at most 100 same-org repositories, otherwise
// "<base>#shard-<bits>" scopes of at most 100 each.
//
// Every scope of an org target that the partition no longer plans (the base
// scope after a split, the two leaves after a merge) and that still holds a
// non-retired active generation gets one retirement work item, marked by
// shard.RetirementAcceptanceUnitID. The candidates come from ShardScopeReader;
// a configuration with an org target requires one, and a read error fails the
// plan. At most MaxRetirementCandidates retire per scan. The parent
// coordinator package owns scheduling order, durable admission, persistence,
// retries, and telemetry.
package alert
