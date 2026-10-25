// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package retirement reads the security-alert scopes the coordinator's
// planner must retire (#9019).
//
// An org target with more than 100 same-org repositories is collected through
// shard scopes whose ingestion_scopes.parent_scope_id is the target's base
// scope_id. When the partition changes, a scope it no longer plans (the base
// after a split, two leaves after a merge) keeps its last active generation,
// which every alert read joins. Reader.RetirementCandidates returns those
// scopes: the base scope or a shard of a listed base, with an active
// generation, outside the planned set, and whose active generation's read
// state does not have repositories_total = 0: a retirement commits 0, and a
// collection with 0 holds no alert fact, so skipping it leaves no readable
// row. The retirement marker itself is the freshness hint
// shard.RetirementFreshnessHint, not the zero count. The
// planner turns each into a retirement work item; the collector commits an
// empty generation for it.
//
// CandidatesQuery is the exact statement. A query or row error is returned,
// never answered with an empty list. The package imports the shared value
// types only, never the parent postgres package.
package retirement
