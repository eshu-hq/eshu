// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/value/affected"
)

// TestAdditiveDomainsRegisterValueFlowRefresh pins refresh registration: the
// code_value_flow_refresh domain registers exactly when the fixpoint
// projector is wired, carrying a handler that re-runs the global fixpoint.
func TestAdditiveDomainsRegisterValueFlowRefresh(t *testing.T) {
	t.Parallel()

	withProjector := appendAdditiveDomainDefinitions(nil, DefaultHandlers{
		CodeEvidenceHandlers: CodeEvidenceHandlers{
			ValueFlowFixpointProjector: &replayRecordingFixpointProjector{},
		},
	})
	var found *DomainDefinition
	for i, def := range withProjector {
		if def.Domain == DomainCodeValueFlowRefresh {
			found = &withProjector[i]
		}
	}
	if found == nil {
		t.Fatal("DomainCodeValueFlowRefresh not registered with fixpoint projector wired")
	}
	if found.Handler == nil {
		t.Fatal("DomainCodeValueFlowRefresh registered with nil handler")
	}

	withoutProjector := appendAdditiveDomainDefinitions(nil, DefaultHandlers{})
	for _, def := range withoutProjector {
		if def.Domain == DomainCodeValueFlowRefresh {
			t.Fatal("DomainCodeValueFlowRefresh registered without fixpoint projector")
		}
	}
}

// wiringProbeRunner is a fake affected.Runner proving the production
// definitions path wires the graph runner into the four producer handlers:
// every gate call lands here, and canned rows drive suppression/emission.
type wiringProbeRunner struct {
	rows  []map[string]any
	calls int
}

func (r *wiringProbeRunner) Run(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	r.calls++
	return r.rows, nil
}

type wiringProbeNodeWriter struct{}

func (wiringProbeNodeWriter) WriteCloudResourceNodes(_ context.Context, _ []map[string]any, _ string) error {
	return nil
}

type wiringProbeRelationshipWriter struct{}

func (wiringProbeRelationshipWriter) WriteWorkloadCloudRelationshipEdges(_ context.Context, _ []map[string]any, _, _, _ string) error {
	return nil
}

func (wiringProbeRelationshipWriter) RetractWorkloadCloudRelationshipEdges(_ context.Context, _ []string, _, _ string) error {
	return nil
}

type wiringProbePerformWriter struct{}

func (wiringProbePerformWriter) WriteIAMCanPerformEdges(_ context.Context, _ []map[string]any, _, _, _ string) error {
	return nil
}

func (wiringProbePerformWriter) RetractIAMCanPerformEdges(_ context.Context, _ []string, _, _ string) error {
	return nil
}

// TestAdditiveDomainsWireRefreshAffectedGraph pins P1-1: the production
// definitions path (not test stubs) carries the graph runner into all four
// producer handlers, so a gate-passing run consults the graph and an empty
// read suppresses through the wired path.
func TestAdditiveDomainsWireRefreshAffectedGraph(t *testing.T) {
	t.Parallel()

	probe := &wiringProbeRunner{}
	defs := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                          &stubFactLoader{},
		CloudResourceNodeWriter:             wiringProbeNodeWriter{},
		WorkloadCloudRelationshipEdgeWriter: wiringProbeRelationshipWriter{},
		IAMCanPerformEdgeWriter:             wiringProbePerformWriter{},
		WorkloadMaterializer:                &WorkloadMaterializer{},
		RefreshAffectedGraph:                probe,
	})
	byDomain := make(map[Domain]DomainDefinition, len(defs))
	for _, def := range defs {
		byDomain[def.Domain] = def
	}
	ctx := context.Background()
	intent := Intent{ScopeID: "wiring-proof", GenerationID: "genesis"}

	workload, ok := byDomain[DomainWorkloadMaterialization].Handler.(WorkloadMaterializationHandler)
	if !ok {
		t.Fatal("DomainWorkloadMaterialization handler missing or wrong type")
	}
	if workload.AffectedGraph == nil {
		t.Fatal("workload handler AffectedGraph not wired")
	}
	uses, ok := byDomain[DomainWorkloadCloudRelationshipMaterialization].Handler.(WorkloadCloudRelationshipMaterializationHandler)
	if !ok {
		t.Fatal("DomainWorkloadCloudRelationshipMaterialization handler missing or wrong type")
	}
	if uses.AffectedGraph == nil {
		t.Fatal("USES handler AffectedGraph not wired")
	}
	perform, ok := byDomain[DomainIAMCanPerformMaterialization].Handler.(IAMCanPerformMaterializationHandler)
	if !ok {
		t.Fatal("DomainIAMCanPerformMaterialization handler missing or wrong type")
	}
	if perform.AffectedGraph == nil {
		t.Fatal("CAN_PERFORM handler AffectedGraph not wired")
	}
	resources, ok := byDomain[DomainAWSResourceMaterialization].Handler.(AWSResourceMaterializationHandler)
	if !ok {
		t.Fatal("DomainAWSResourceMaterialization handler missing or wrong type")
	}
	if resources.AffectedGraph == nil {
		t.Fatal("aws_resource handler AffectedGraph not wired")
	}

	// Empty graph reads suppress through the wired path on all three
	// same-package handlers (the iamcan package proves its own stub path;
	// its wiring is pinned above).
	if got := workload.refreshResultSignals(ctx, intent, nil, 2, []string{"repo-wired"})[affected.RefreshAffectedReposSignal]; got != 0 {
		t.Errorf("wired workload signal = %v, want 0 on empty graph read", got)
	}
	rows := []map[string]any{{"workload_id": "wl-wired"}}
	if got := uses.refreshResultSignals(ctx, intent, nil, 1, rows)[affected.RefreshAffectedReposSignal]; got != 0 {
		t.Errorf("wired USES signal = %v, want 0 on empty graph read", got)
	}
	resRows := []map[string]any{{"uid": "res-wired"}}
	if got := resources.refreshResultSignals(ctx, intent, nil, 1, resRows)[affected.RefreshAffectedReposSignal]; got != 0 {
		t.Errorf("wired aws_resource signal = %v, want 0 on empty graph read", got)
	}
	if probe.calls != 3 {
		t.Errorf("wired gate graph reads = %d, want 3 (one per handler)", probe.calls)
	}

	// Without the runner the production path still fails open.
	unwired := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                          &stubFactLoader{},
		CloudResourceNodeWriter:             wiringProbeNodeWriter{},
		WorkloadCloudRelationshipEdgeWriter: wiringProbeRelationshipWriter{},
		IAMCanPerformEdgeWriter:             wiringProbePerformWriter{},
		WorkloadMaterializer:                &WorkloadMaterializer{},
	})
	unwiredWorkload, ok := unwiredByDomain(unwired)[DomainWorkloadMaterialization].Handler.(WorkloadMaterializationHandler)
	if !ok {
		t.Fatal("unwired DomainWorkloadMaterialization handler missing")
	}
	if unwiredWorkload.AffectedGraph != nil {
		t.Fatal("unwired workload handler AffectedGraph non-nil")
	}
	if got := unwiredWorkload.refreshResultSignals(ctx, intent, nil, 2, []string{"repo-wired"})[affected.RefreshAffectedReposSignal]; got != 1 {
		t.Errorf("unwired workload signal = %v, want 1 fail-open", got)
	}
}

func unwiredByDomain(defs []DomainDefinition) map[Domain]DomainDefinition {
	byDomain := make(map[Domain]DomainDefinition, len(defs))
	for _, def := range defs {
		byDomain[def.Domain] = def
	}
	return byDomain
}
