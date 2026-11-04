// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// manifestRuntimePort is the registered fan-out port plus the #9874 reader,
// counting the reader calls a marker pass makes.
type manifestRuntimePort struct {
	registrationFanOut
	reads int
}

func (p *manifestRuntimePort) ReadManifestDependencyGeneration(context.Context, string, string) (supplychaincore.ManifestDependencyGeneration, error) {
	p.reads++
	return supplychaincore.ManifestDependencyGeneration{Activation: supplychaincore.ManifestGenerationActive}, nil
}

// manifestRuntime registers the production supply_chain_impact definition
// (appendSupplyChainCorrelationAdditiveDomains) behind a runtime whose
// GenerationCheck answers with check.
func manifestRuntime(t *testing.T, port *manifestRuntimePort, check GenerationFreshnessCheck) *Runtime {
	t.Helper()
	definitions := appendSupplyChainCorrelationAdditiveDomains(nil, DefaultHandlers{
		FactLoader: &stubSupplyChainImpactFactLoader{},
		SupplyChainSecurityHandlers: SupplyChainSecurityHandlers{
			SupplyChainImpactWriter:             &recordingSupplyChainImpactWriter{},
			SupplyChainImpactFencingTokenIssuer: fixedSupplyChainImpactFencingTokenIssuer{token: 1},
			SupplyChainImpactFanOut:             port,
		},
	})
	registry := NewRegistry()
	for _, definition := range definitions {
		if definition.Domain != DomainSupplyChainImpact {
			continue
		}
		if err := registry.Register(definition); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
	}
	rt, err := NewRuntime(registry)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	rt.GenerationCheck = check
	return rt
}

func manifestRuntimeIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID: "intent-manifest", ScopeID: "git-repo-checkout", GenerationID: "gen-2",
		SourceSystem: "git", Domain: DomainSupplyChainImpact, Cause: "manifest dependency evidence observed",
		EntityKeys: []string{"supply_chain_impact:git-repo-checkout"}, Status: IntentStatusPending,
		Payload: map[string]any{supplychaincore.PayloadKeyTrigger: supplychaincore.TriggerManifestDependency},
	}
}

// TestRuntimeDefersAManifestMarkerBeforeTheHandler proves the real deferral
// path for a #9874 marker item: the default runtime's GenerationCheck defers a
// pending generation with the shared, non-counting
// generation_activation_not_ready class and skips a superseded one, both
// before the handler runs, so the marker branch's own gate never sees them.
// An active generation reaches the marker branch.
func TestRuntimeDefersAManifestMarkerBeforeTheHandler(t *testing.T) {
	t.Parallel()

	pending := &manifestRuntimePort{}
	_, err := manifestRuntime(t, pending, func(_ context.Context, scopeID, generationID string) (bool, error) {
		return false, reducercontract.GenerationNotYetActiveError{ScopeID: scopeID, GenerationID: generationID, ActiveGenerationID: "gen-1"}
	}).Execute(context.Background(), manifestRuntimeIntent())
	var classed interface{ FailureClass() string }
	if !IsRetryable(err) || !errors.As(err, &classed) ||
		classed.FailureClass() != reducercontract.GenerationActivationNotReadyFailureClass {
		t.Fatalf("pending Execute() error = %v, want retryable %s", err, reducercontract.GenerationActivationNotReadyFailureClass)
	}
	if pending.reads != 0 {
		t.Fatalf("pending generation reached the handler: reads = %d", pending.reads)
	}

	superseded := &manifestRuntimePort{}
	result, err := manifestRuntime(t, superseded, func(context.Context, string, string) (bool, error) {
		return false, nil
	}).Execute(context.Background(), manifestRuntimeIntent())
	if err != nil || result.Status != ResultStatusSuperseded || superseded.reads != 0 {
		t.Fatalf("superseded Execute() = %+v, %v, reads = %d; want superseded before the handler", result, err, superseded.reads)
	}

	active := &manifestRuntimePort{}
	result, err = manifestRuntime(t, active, func(context.Context, string, string) (bool, error) {
		return true, nil
	}).Execute(context.Background(), manifestRuntimeIntent())
	if err != nil || result.Status != ResultStatusSucceeded || active.reads != 1 {
		t.Fatalf("active Execute() = %+v, %v, reads = %d; want the marker branch to run once", result, err, active.reads)
	}
}
