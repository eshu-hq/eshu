// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package refresh

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/value"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// FixpointProjector re-runs the global value-flow fixpoint. It is satisfied
// by value.FixpointEvidenceProjector; declared here so the handler does not
// depend on the summary handler's identical declaration.
type FixpointProjector interface {
	ProjectValueFlowFixpointEvidence(ctx context.Context, scopeID, generationID string) (value.FixpointProjectionResult, error)
}

// Handler re-runs the global value-flow fixpoint for one refresh intent. It
// persists nothing itself: summaries, sources, and graph ids are unchanged by
// the producers whose completion enqueues the refresh (workload, USES,
// CAN_PERFORM, and aws_resource materialization), and the fixpoint reloads
// them globally before solving. The solve is idempotent (global retract then
// MERGE on evidence_uid), so re-runs converge; concurrent per-repo summary
// fixpoint runs are a pre-existing race class filed separately (issue #6785
// design note).
type Handler struct {
	Fixpoint FixpointProjector
}

// Handle executes one value-flow refresh intent.
func (h Handler) Handle(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	if intent.Domain != reducercontract.DomainCodeValueFlowRefresh {
		return reducercontract.Result{}, fmt.Errorf("value-flow refresh handler does not accept domain %q", intent.Domain)
	}
	if h.Fixpoint == nil {
		return reducercontract.Result{}, fmt.Errorf("value-flow refresh fixpoint projector is required")
	}
	fixpoint, err := h.Fixpoint.ProjectValueFlowFixpointEvidence(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("project value-flow fixpoint evidence: %w", err)
	}
	slog.Info(
		"value-flow refresh completed",
		"scope_id", intent.ScopeID,
		"generation_id", intent.GenerationID,
		"fixpoint_finding_count", fixpoint.FindingCount,
		"fixpoint_graph_rows", fixpoint.GraphRows,
		"fixpoint_unresolved_endpoint_count", fixpoint.UnresolvedEndpointCount,
	)
	return reducercontract.Result{
		IntentID: intent.IntentID,
		Domain:   reducercontract.DomainCodeValueFlowRefresh,
		Status:   reducercontract.ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"refreshed value-flow fixpoint, projected %d fixpoint edge(s)",
			fixpoint.GraphRows,
		),
		CanonicalWrites: fixpoint.GraphRows,
	}, nil
}

// Definition returns the additive domain definition for the value-flow
// refresh: it re-runs the global fixpoint (rewriting cloud-sink edges) after
// late producers land, reading the cross-scope chain the fixpoint loads.
func Definition() reducercontract.DomainDefinition {
	return reducercontract.DomainDefinition{
		Domain:  reducercontract.DomainCodeValueFlowRefresh,
		Summary: "re-run the global value-flow fixpoint after late producers land",
		Ownership: reducercontract.OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "code_value_flow_refresh",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
			},
		},
	}
}
