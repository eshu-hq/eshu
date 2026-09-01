// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codeintel computes the materialized code-reachability read model
// and the #5376 code-root verdicts that gate it, for one repository
// generation at a time.
//
// [BuildCodeReachabilityRows] (and its stats-returning sibling
// [BuildCodeReachabilityRowsWithStats]) walks a bounded, in-memory snapshot of
// entities and edges ([CodeReachabilityProjectionInput]) outward from a root
// set and classifies every reached entity as [CodeReachabilityStateReachable]
// or [CodeReachabilityStateAmbiguous]. [CodeReachabilityProjectionRunner]
// drains pending snapshots through a [CodeReachabilityInputLoader], partitions
// them by (scope, generation, repository) — a provably disjoint conflict
// domain — and replaces each partition's rows through a
// [CodeReachabilityRowWriter] in one transaction.
//
// [BuildCodeRootVerdicts] is the repo-wide decision that keeps that
// reachable-set honest for Ruby-on-Rails controller actions. The parser roots
// a controller action whenever its declared base class is unresolved in that
// file alone, which is deliberately over-inclusive per file. This function
// re-walks the base-class chain against the repository's full class registry
// ([RubyClassEntity]) and either CONFIRMS the root (a real, unreachable
// framework action) or DOWNGRADES it (the base resolves onward to a
// non-controller reject branch, so the parser's per-file root was a false
// positive). Absence of a verdict is not a claim — the dead-code query treats
// an unverdicted root as kept, which is the safe direction when evidence is
// incomplete. [CodeRootVerdictRow] carries a [CodeRootVerdictBasis] recording
// the exact chain and terminal event a verdict rests on, for the audit trail
// and the query surface.
//
// The route-liveness layer in code_root_verdicts_routes.go (#5494) is a
// second, independent downgrade path over the same CONFIRMED set: a
// controller action with a real, resolvable class ancestry can still be dead
// if the repository's parsed Rails routes never dispatch to it. This path
// only downgrades when the repo's route surface is provably exact and
// complete ([RouteEvidenceRouted] / [RouteEvidenceUnrouted]); any unmodeled or
// ambiguous route registration ([RouteEvidenceAmbiguous]) keeps every action
// in the repo, and no observed route data at all ([RouteEvidenceNoData])
// keeps as well. Both downgrade paths write into the same
// [CodeRootVerdictRow] slice and are replaced atomically with the
// reachability rows they gate, so the two can never disagree about a given
// entity.
package codeintel
