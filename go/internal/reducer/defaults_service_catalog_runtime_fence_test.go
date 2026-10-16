// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"io"
	"log/slog"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
)

type stubRuntimeProducerReadiness struct{}

func (stubRuntimeProducerReadiness) RuntimeProducerStates(
	context.Context, []string, []reducercontract.Domain,
) (map[string]servicecatalog.RuntimeProducerState, error) {
	return nil, nil
}

type stubRuntimeInstanceLoader struct{}

func (stubRuntimeInstanceLoader) GetRuntimeInstancesForRepos(
	context.Context, []string,
) (map[string][]ServiceRuntimeInstance, error) {
	return nil, nil
}

// TestServiceCatalogRuntimeLoaderRequiresTheReadinessFence pins the #7816
// wiring: the default catalog never hands the service catalog handler a
// runtime loader without the producer readiness fence, so production cannot
// read runtime evidence while workload_materialization is in flight.
func TestServiceCatalogRuntimeLoaderRequiresTheReadinessFence(t *testing.T) {
	t.Parallel()

	base := DefaultHandlers{
		FactLoader:                      &stubFactLoader{},
		ServiceCatalogCorrelationWriter: PostgresServiceCatalogCorrelationWriter{},
		ServiceMaterializationWriter:    noopServiceMaterializationWriter{},
		ServiceRuntimeInstanceLoader:    stubRuntimeInstanceLoader{},
	}
	unfenced := serviceCatalogHandlerFromDefaults(t, base)
	if unfenced.RuntimeInstanceLoader != nil {
		t.Fatal("runtime loader wired without ServiceRuntimeProducerReadiness; the read would be unfenced")
	}

	fenced := base
	fenced.ServiceRuntimeProducerReadiness = stubRuntimeProducerReadiness{}
	fenced.CrossScopeReadinessLogger = slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := serviceCatalogHandlerFromDefaults(t, fenced)
	if handler.RuntimeInstanceLoader == nil || handler.RuntimeProducerReadiness == nil {
		t.Fatalf("runtime loader=%v readiness=%v, want both wired", handler.RuntimeInstanceLoader, handler.RuntimeProducerReadiness)
	}
	if handler.Logger != fenced.CrossScopeReadinessLogger {
		t.Fatal("runtime fence logger not wired to the reducer's structured readiness logger")
	}
}

func serviceCatalogHandlerFromDefaults(t *testing.T, handlers DefaultHandlers) ServiceCatalogCorrelationHandler {
	t.Helper()
	for _, def := range implementedDefaultDomainDefinitions(handlers) {
		if def.Domain != DomainServiceCatalogCorrelation {
			continue
		}
		handler, ok := def.Handler.(ServiceCatalogCorrelationHandler)
		if !ok {
			t.Fatalf("service catalog handler type = %T", def.Handler)
		}
		return handler
	}
	t.Fatal("service catalog correlation domain not registered")
	return ServiceCatalogCorrelationHandler{}
}
