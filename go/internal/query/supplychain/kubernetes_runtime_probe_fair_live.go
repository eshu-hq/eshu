// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package supplychain

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

// Live-test seam for the Kubernetes runtime fair planner. This file is
// compiled only under the `integration` build tag, so the default build's
// API surface and its `unused` gate never see it. The two live suites that
// wire the planner to the production Postgres store and a real graph reader
// live in the parent query package (they name root production types, which
// this package cannot import without a cycle), and they reach the planner
// internals exclusively through these forwards. Every symbol here mirrors
// its unexported twin one-to-one; keep them behavior-identical.

// KubernetesRuntimeProbePlan is one digest's planned query. See
// kubernetesRuntimeProbePlan.
type KubernetesRuntimeProbePlan = kubernetesRuntimeProbePlan

// KubernetesRuntimeProbeFanout is the fanned-out candidate read. See
// kubernetesRuntimeProbeFanout.
type KubernetesRuntimeProbeFanout = kubernetesRuntimeProbeFanout

// PlanKubernetesRuntimeProbeQueries plans the per-digest queries. See
// planKubernetesRuntimeProbeQueries.
func PlanKubernetesRuntimeProbeQueries(digests []string, allScopes bool) []KubernetesRuntimeProbePlan {
	return planKubernetesRuntimeProbeQueries(digests, allScopes)
}

// QueryKubernetesRuntimeCandidates fans the plans out over the graph. See
// queryKubernetesRuntimeCandidates.
func QueryKubernetesRuntimeCandidates(
	ctx context.Context,
	graph querycontract.GraphQuery,
	plans []KubernetesRuntimeProbePlan,
) (KubernetesRuntimeProbeFanout, error) {
	return queryKubernetesRuntimeCandidates(ctx, graph, plans)
}

// Candidates returns the deduplicated candidates the fanout collected.
func (f kubernetesRuntimeProbeFanout) Candidates() []KubernetesRuntimeCandidate {
	return f.candidates
}

// MaxConcurrency returns the peak worker count the fanout reached.
func (f kubernetesRuntimeProbeFanout) MaxConcurrency() int {
	return f.maxConcurrency
}

// PlannedCandidateLimit returns the summed per-plan query budget.
func (f kubernetesRuntimeProbeFanout) PlannedCandidateLimit() int {
	return f.plannedCandidateLimit
}

// KubernetesRuntimeCandidates converts raw graph rows to candidates. See
// kubernetesRuntimeCandidates.
func KubernetesRuntimeCandidates(rows []map[string]any) []KubernetesRuntimeCandidate {
	return kubernetesRuntimeCandidates(rows)
}

// ApplySupplyChainKubernetesRuntimeEvidenceLive applies the runtime evidence
// probe to findings rows. See applySupplyChainKubernetesRuntimeEvidence.
func (h *SupplyChainHandler) ApplySupplyChainKubernetesRuntimeEvidenceLive(
	ctx context.Context,
	access querycontract.RepositoryAccessFilter,
	rows []impact.SupplyChainImpactFindingRow,
) error {
	return h.applySupplyChainKubernetesRuntimeEvidence(ctx, access, rows)
}
