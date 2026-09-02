// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/incident"
)

// This file holds the DomainIncidentRepositoryCorrelation registration gate
// tests. They moved out of the incident package's own test suite in issue
// #6061 because this file, unlike that package, still needs loader/resolver/
// writer doubles to prove the registration gate rather than their own
// behavior (see defaults_config_state_drift_writer_gate_test.go for the same
// pattern applied to DomainConfigStateDrift).

// stubIncidentAppliedRoutingLoader is a no-op AppliedPagerDutyServiceRoutingLoader
// used only to satisfy the non-nil gate in implementedDefaultDomainDefinitions.
type stubIncidentAppliedRoutingLoader struct{}

func (stubIncidentAppliedRoutingLoader) LoadAppliedPagerDutyServiceRouting(
	context.Context, string, string,
) ([]incident.AppliedPagerDutyServiceRouting, error) {
	return nil, nil
}

// stubIncidentBackendRepositoryResolver is a no-op BackendRepositoryResolver
// used only to satisfy the non-nil gate in implementedDefaultDomainDefinitions.
type stubIncidentBackendRepositoryResolver struct{}

func (stubIncidentBackendRepositoryResolver) ResolveBackendRepository(
	context.Context, string, string,
) (incident.BackendRepositoryResolution, error) {
	return incident.BackendRepositoryResolution{}, nil
}

// stubIncidentRepositoryCorrelationWriter is a no-op
// IncidentRepositoryCorrelationWriter used only to satisfy the non-nil gate in
// implementedDefaultDomainDefinitions.
type stubIncidentRepositoryCorrelationWriter struct{}

func (stubIncidentRepositoryCorrelationWriter) WriteIncidentRepositoryCorrelations(
	context.Context, incident.IncidentRepositoryCorrelationWrite,
) (incident.IncidentRepositoryCorrelationWriteResult, error) {
	return incident.IncidentRepositoryCorrelationWriteResult{}, nil
}

// TestImplementedDefaultDomainDefinitionsOmitsIncidentRepositoryCorrelationWithoutWriter
// proves the additive domain stays unregistered when only the loader is wired,
// so a half-wired deployment never silently drops correlation intents.
func TestImplementedDefaultDomainDefinitionsOmitsIncidentRepositoryCorrelationWithoutWriter(t *testing.T) {
	t.Parallel()
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		IncidentRoutingHandlers: IncidentRoutingHandlers{
			AppliedPagerDutyServiceRoutingLoader: stubIncidentAppliedRoutingLoader{},
		},
	})
	for _, def := range definitions {
		if def.Domain == DomainIncidentRepositoryCorrelation {
			t.Fatalf("incident_repository_correlation registered without writer; want omitted")
		}
	}
}

// TestImplementedDefaultDomainDefinitionsIncludesIncidentRepositoryCorrelationWhenWired
// proves the domain registers with a fully-wired handler and canonical-write
// ownership once loader and writer are present.
func TestImplementedDefaultDomainDefinitionsIncludesIncidentRepositoryCorrelationWhenWired(t *testing.T) {
	t.Parallel()
	loader := stubIncidentAppliedRoutingLoader{}
	resolver := stubIncidentBackendRepositoryResolver{}
	writer := stubIncidentRepositoryCorrelationWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		IncidentRoutingHandlers: IncidentRoutingHandlers{
			AppliedPagerDutyServiceRoutingLoader: loader,
			BackendRepositoryResolver:            resolver,
			IncidentRepositoryCorrelationWriter:  writer,
		},
	})
	found := false
	for _, def := range definitions {
		if def.Domain != DomainIncidentRepositoryCorrelation {
			continue
		}
		found = true
		handler, ok := def.Handler.(incident.IncidentRepositoryCorrelationHandler)
		if !ok {
			t.Fatalf("handler type = %T, want incident.IncidentRepositoryCorrelationHandler", def.Handler)
		}
		if handler.Loader == nil || handler.Resolver == nil || handler.Writer == nil {
			t.Fatal("incident_repository_correlation handler not fully wired")
		}
		if !def.Ownership.CanonicalWrite {
			t.Fatal("incident_repository_correlation must declare CanonicalWrite ownership")
		}
	}
	if !found {
		t.Fatal("incident_repository_correlation not registered after wiring loader+writer")
	}
}
