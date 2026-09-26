// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// unfencedServiceCatalogRelationshipLoader exposes the scope and by-repos
// reads but not the fused corpus-fenced read.
type unfencedServiceCatalogRelationshipLoader struct{}

func (unfencedServiceCatalogRelationshipLoader) GetResolvedRelationships(
	context.Context, string,
) ([]relationships.ResolvedRelationship, error) {
	return nil, nil
}

func (unfencedServiceCatalogRelationshipLoader) GetResolvedRelationshipsForRepos(
	context.Context, []string,
) ([]relationships.ResolvedRelationship, error) {
	return nil, nil
}

// fencedServiceCatalogRelationshipLoader adds the fused read.
type fencedServiceCatalogRelationshipLoader struct {
	unfencedServiceCatalogRelationshipLoader
}

func (fencedServiceCatalogRelationshipLoader) GetResolvedRelationshipsForReposWithCorpusFence(
	context.Context, []string,
) ([]relationships.ResolvedRelationship, bool, error) {
	return nil, true, nil
}

type noopServiceMaterializationWriter struct{}

func (noopServiceMaterializationWriter) WriteServiceMaterialization(
	context.Context, servicecatalog.ServiceMaterializationWrite,
) (servicecatalog.ServiceMaterializationWriteResult, error) {
	return servicecatalog.ServiceMaterializationWriteResult{}, nil
}

// TestServiceCatalogDeploymentRelationshipLoaderRequiresTheFencedRead pins the
// #7258 wiring: the service deployment and dependency families are sourced
// only from a loader with the fused corpus-fenced read, never from one that
// has only the unfenced by-repos read, and never without the lineage writer.
func TestServiceCatalogDeploymentRelationshipLoaderRequiresTheFencedRead(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		handlers DefaultHandlers
		wantNil  bool
	}{
		{
			name: "fenced loader and writer",
			handlers: DefaultHandlers{
				ServiceMaterializationWriter: noopServiceMaterializationWriter{},
				ResolvedRelationshipLoader:   fencedServiceCatalogRelationshipLoader{},
			},
		},
		{
			name: "unfenced loader",
			handlers: DefaultHandlers{
				ServiceMaterializationWriter: noopServiceMaterializationWriter{},
				ResolvedRelationshipLoader:   unfencedServiceCatalogRelationshipLoader{},
			},
			wantNil: true,
		},
		{
			name: "no writer",
			handlers: DefaultHandlers{
				ResolvedRelationshipLoader: fencedServiceCatalogRelationshipLoader{},
			},
			wantNil: true,
		},
		{
			name: "no loader",
			handlers: DefaultHandlers{
				ServiceMaterializationWriter: noopServiceMaterializationWriter{},
			},
			wantNil: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := serviceCatalogDeploymentRelationshipLoader(tc.handlers)
			if (got == nil) != tc.wantNil {
				t.Fatalf("serviceCatalogDeploymentRelationshipLoader() = %v, want nil=%v", got, tc.wantNil)
			}
			if got == nil {
				return
			}
			// The value the root wiring returns must satisfy the handler's
			// field type, so the handler can never be wired unfenced.
			var field servicecatalog.CorpusFencedResolvedRelationshipLoader = got
			if field == nil {
				t.Fatal("fenced loader lost when assigned to the handler field")
			}
		})
	}
}
