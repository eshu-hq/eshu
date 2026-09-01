// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import (
	"context"
	"fmt"
	"strings"
)

// Phase identifies one durable readiness milestone for a graph projection
// keyspace.
type Phase string

const (
	// PhaseCanonicalNodesCommitted is published after canonical projector
	// node writes commit successfully.
	PhaseCanonicalNodesCommitted Phase = "canonical_nodes_committed"
	// PhaseDeployableUnitCorrelation is published after the deployable-unit
	// correlation pass finishes one bounded slice, including slices that
	// intentionally admit zero candidates.
	PhaseDeployableUnitCorrelation Phase = "deployable_unit_correlation"
	// PhaseSemanticNodesCommitted is published after semantic entity reducer
	// node writes commit successfully.
	PhaseSemanticNodesCommitted Phase = "semantic_nodes_committed"
	// PhaseBackwardEvidenceCommitted is published after deferred backward
	// relationship evidence is durably committed for one scope-generation
	// slice.
	PhaseBackwardEvidenceCommitted Phase = "backward_evidence_committed"
	// PhaseDeploymentMapping is published after the deployment_mapping
	// reducer domain finishes one bounded slice.
	PhaseDeploymentMapping Phase = "deployment_mapping"
	// PhaseWorkloadMaterialization is published after the
	// workload_materialization reducer domain finishes one bounded slice.
	PhaseWorkloadMaterialization Phase = "workload_materialization"
	// PhaseCrossSourceAnchorReady is reserved for the future DSL evaluator
	// publication that proves cross-source joins are fully converged.
	PhaseCrossSourceAnchorReady Phase = "cross_source_anchor_ready"
)

// PhaseKey identifies one bounded graph-write readiness slice.
type PhaseKey struct {
	ScopeID          string
	AcceptanceUnitID string
	SourceRunID      string
	GenerationID     string
	Keyspace         Keyspace
}

// Validate checks the bounded readiness identity contract.
func (k PhaseKey) Validate() error {
	if strings.TrimSpace(k.ScopeID) == "" {
		return fmt.Errorf("scope_id must not be blank")
	}
	if strings.TrimSpace(k.AcceptanceUnitID) == "" {
		return fmt.Errorf("acceptance_unit_id must not be blank")
	}
	if strings.TrimSpace(k.SourceRunID) == "" {
		return fmt.Errorf("source_run_id must not be blank")
	}
	if strings.TrimSpace(k.GenerationID) == "" {
		return fmt.Errorf("generation_id must not be blank")
	}
	if strings.TrimSpace(string(k.Keyspace)) == "" {
		return fmt.Errorf("keyspace must not be blank")
	}
	return nil
}

// ReadinessLookup reports whether a bounded readiness slice has reached the
// requested phase. It returns (ready, found).
type ReadinessLookup func(key PhaseKey, phase Phase) (bool, bool)

// ReadinessPrefetch resolves readiness for a bounded set of keys and returns
// an in-memory lookup closure for the current cycle.
type ReadinessPrefetch func(ctx context.Context, keys []PhaseKey, phase Phase) (ReadinessLookup, error)
