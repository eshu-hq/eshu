// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package crossplane materializes the crossplane_satisfied_by_materialization
// reducer domain: Crossplane Claim -> XRD classification decisions projected
// into canonical SATISFIED_BY graph edges between a K8sResource node (the
// Claim — never parser-labeled, see issue #5347) and the CrossplaneXRD node
// it resolved against.
//
// [CrossplaneSatisfiedByMaterializationHandler] loads the intent's own scope
// generation's content_entity facts (Claim candidates) plus the cross-scope
// active CrossplaneXRD facts, resolves each candidate's (group, kind)
// against exactly one XRD's (spec.group, spec.claimNames.kind) through
// [ExtractCrossplaneSatisfiedByEdgeRows], and hands the resulting batch to
// the [CrossplaneSatisfiedByEdgeWriter]. A zero-match candidate is an
// ordinary Kubernetes object and an ambiguous (2+ match) candidate stays
// provenance-only; neither fabricates an edge.
//
// After a successful write, the handler confirms which resolved rows
// actually committed a SATISFIED_BY edge (issue #5476 P1-b: the writer's
// MATCH-MATCH-MERGE deliberately no-ops, without an error, when either
// endpoint node is absent) and records only the confirmed subset into the
// [CrossplaneRedriveTargetLedgerWriter], so the cross-scope redrive sweep
// never fences a target this handler did not actually satisfy.
package crossplane
