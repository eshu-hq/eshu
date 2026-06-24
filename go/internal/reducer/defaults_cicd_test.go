// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"
)

func TestImplementedDefaultDomainDefinitionsOmitsCICDRunCorrelationWithoutAdapters(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{})
	for _, def := range definitions {
		if def.Domain == DomainCICDRunCorrelation {
			t.Fatalf("ci_cd_run_correlation registered without adapters; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesCICDRunCorrelationWhenAdaptersPresent(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &recordingCICDRunCorrelationWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:               loader,
		CICDRunCorrelationWriter: writer,
	})
	found := false
	for _, def := range definitions {
		if def.Domain == DomainCICDRunCorrelation {
			found = true
			handler, ok := def.Handler.(CICDRunCorrelationHandler)
			if !ok {
				t.Fatalf("ci_cd_run_correlation handler type = %T, want CICDRunCorrelationHandler", def.Handler)
			}
			if handler.FactLoader != loader {
				t.Fatal("ci_cd_run_correlation handler FactLoader was not wired")
			}
			if handler.Writer != writer {
				t.Fatal("ci_cd_run_correlation handler Writer was not wired")
			}
		}
	}
	if !found {
		t.Fatal("ci_cd_run_correlation not registered after wiring loader+writer")
	}
}

func TestImplementedDefaultDomainDefinitionsOmitsServiceCatalogCorrelationWithoutAdapters(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{})
	for _, def := range definitions {
		if def.Domain == DomainServiceCatalogCorrelation {
			t.Fatalf("service_catalog_correlation registered without adapters; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesServiceCatalogCorrelationWhenAdaptersPresent(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &recordingServiceCatalogCorrelationWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                      loader,
		ServiceCatalogCorrelationWriter: writer,
	})
	found := false
	for _, def := range definitions {
		if def.Domain == DomainServiceCatalogCorrelation {
			found = true
			handler, ok := def.Handler.(ServiceCatalogCorrelationHandler)
			if !ok {
				t.Fatalf("service_catalog_correlation handler type = %T, want ServiceCatalogCorrelationHandler", def.Handler)
			}
			if handler.FactLoader != loader {
				t.Fatal("service_catalog_correlation handler FactLoader was not wired")
			}
			if handler.Writer != writer {
				t.Fatal("service_catalog_correlation handler Writer was not wired")
			}
		}
	}
	if !found {
		t.Fatal("service_catalog_correlation not registered after wiring loader+writer")
	}
}

func TestImplementedDefaultDomainDefinitionsWiresServiceIncidentEvidenceLoader(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &recordingServiceCatalogCorrelationWriter{}
	incidentLoader := &fakeServiceScopedIncidentLoader{}
	materialization := PostgresServiceMaterializationWriter{DB: newFakeServiceMaterializationStore(), Now: time.Now}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                      loader,
		ServiceCatalogCorrelationWriter: writer,
		ServiceMaterializationWriter:    materialization,
		ServiceIncidentEvidenceLoader:   incidentLoader,
	})

	for _, def := range definitions {
		if def.Domain != DomainServiceCatalogCorrelation {
			continue
		}
		handler, ok := def.Handler.(ServiceCatalogCorrelationHandler)
		if !ok {
			t.Fatalf("service_catalog_correlation handler type = %T, want ServiceCatalogCorrelationHandler", def.Handler)
		}
		if handler.IncidentEvidenceLoader != incidentLoader {
			t.Fatal("service_catalog_correlation handler IncidentEvidenceLoader was not wired")
		}
		return
	}
	t.Fatal("service_catalog_correlation not registered after wiring loader+writer")
}
