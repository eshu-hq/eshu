// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInfraResourceAggregateRejectsUnknownCategory(t *testing.T) {
	t.Parallel()

	store := &stubInfraResourceAggregateStore{}
	handler := &InfraHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// `kubernetes` is a typo for `k8s`; the closed enum is k8s / terraform /
	// argocd / crossplane / helm / cloud. Both aggregate endpoints must surface
	// out-of-contract categories as 400.
	for _, target := range []string{
		"/api/v0/infra/resources/count?category=kubernetes",
		"/api/v0/infra/resources/inventory?category=kubernetes",
	} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
	if store.countCalls != 0 || store.invCalls != 0 {
		t.Fatalf("store called for unknown category (countCalls=%d invCalls=%d)",
			store.countCalls, store.invCalls)
	}
}

func TestInfraResourceAggregateAcceptsCloudCategory(t *testing.T) {
	t.Parallel()

	store := &stubInfraResourceAggregateStore{
		count: InfraResourceAggregateCount{
			TotalResources: 3,
			ByProvider:     map[string]int{"aws": 3},
			ByEnvironment:  map[string]int{"unknown": 3},
			ByLabel:        map[string]int{"CloudResource": 3},
		},
	}
	handler := &InfraHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/infra/resources/count?category=cloud&provider=aws", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.Category, "cloud"; got != want {
		t.Fatalf("Category = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Provider, "aws"; got != want {
		t.Fatalf("Provider = %q, want %q", got, want)
	}
}

func TestGraphInfraResourceAggregateCloudCategoryUsesCloudFields(t *testing.T) {
	t.Parallel()

	graph := &stubInfraGraphQuery{}
	store := NewGraphInfraResourceAggregateStore(graph)
	_, err := store.CountInfraResources(context.Background(), InfraResourceAggregateFilter{
		Category:        "cloud",
		Provider:        "aws",
		ResourceService: "ssm",
	})
	if err != nil {
		t.Fatalf("CountInfraResources: %v", err)
	}
	if len(graph.calls) == 0 {
		t.Fatal("graph.Run never called")
	}
	cypher := graph.calls[0].Cypher
	for _, want := range []string{
		"n:CloudResource",
		"n.source_system = $provider",
		"n.service_kind = $resource_service",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher = %q, want fragment %q", cypher, want)
		}
	}
	for _, forbidden := range []string{
		"n.provider = $provider",
		"n.resource_service = $resource_service",
	} {
		if strings.Contains(cypher, forbidden) {
			t.Fatalf("cypher = %q, must use CloudResource fields instead of %q", cypher, forbidden)
		}
	}
}

func TestInfraResourceInventoryCloudCategoryUsesCloudGroupFields(t *testing.T) {
	t.Parallel()

	providerExpr, err := infraResourceInventoryGroupExpression(
		InfraResourceInventoryByProvider,
		InfraResourceAggregateFilter{Category: "cloud"},
	)
	if err != nil {
		t.Fatalf("provider group expression: %v", err)
	}
	if !strings.Contains(providerExpr, "n.source_system") || strings.Contains(providerExpr, "n.provider") {
		t.Fatalf("provider group expression = %q, want CloudResource source_system", providerExpr)
	}

	serviceExpr, err := infraResourceInventoryGroupExpression(
		InfraResourceInventoryByResourceService,
		InfraResourceAggregateFilter{Category: "cloud"},
	)
	if err != nil {
		t.Fatalf("resource_service group expression: %v", err)
	}
	if !strings.Contains(serviceExpr, "n.service_kind") || strings.Contains(serviceExpr, "n.resource_service") {
		t.Fatalf("resource_service group expression = %q, want CloudResource service_kind", serviceExpr)
	}
}

func TestGraphInfraResourceAggregateDefaultScopeKeepsCloudFiltersReachable(t *testing.T) {
	t.Parallel()

	where := infraResourceAggregateWhereClause(allInfraLabels, InfraResourceAggregateFilter{
		Kind:            "ssm",
		Provider:        "aws",
		ResourceService: "ssm",
	})
	for _, want := range []string{
		"n:CloudResource",
		"n.service_kind = $kind",
		"(n.provider = $provider OR (n:CloudResource AND n.source_system = $provider))",
		"(n.resource_service = $resource_service OR n.service_kind = $resource_service)",
	} {
		if !strings.Contains(where, want) {
			t.Fatalf("where = %q, want fragment %q", where, want)
		}
	}
	if strings.Contains(where, "(n.provider = $provider OR n.source_system = $provider)") {
		t.Fatalf("where = %q, must scope source_system provider fallback to CloudResource", where)
	}
}

func TestInfraResourceInventoryDefaultScopeCoalescesCloudAndTerraformGroups(t *testing.T) {
	t.Parallel()

	providerExpr, err := infraResourceInventoryGroupExpression(
		InfraResourceInventoryByProvider,
		InfraResourceAggregateFilter{},
	)
	if err != nil {
		t.Fatalf("provider group expression: %v", err)
	}
	for _, want := range []string{"n.provider", "n.source_system"} {
		if !strings.Contains(providerExpr, want) {
			t.Fatalf("provider group expression = %q, want %s", providerExpr, want)
		}
	}
	if !strings.Contains(providerExpr, "WHEN n:CloudResource") {
		t.Fatalf("provider group expression = %q, want source_system fallback gated to CloudResource", providerExpr)
	}
	if strings.Contains(providerExpr, "coalesce(n.provider, n.source_system") {
		t.Fatalf("provider group expression = %q, must not coalesce source_system into provider for non-cloud nodes", providerExpr)
	}

	serviceExpr, err := infraResourceInventoryGroupExpression(
		InfraResourceInventoryByResourceService,
		InfraResourceAggregateFilter{},
	)
	if err != nil {
		t.Fatalf("resource_service group expression: %v", err)
	}
	for _, want := range []string{"n.resource_service", "n.service_kind"} {
		if !strings.Contains(serviceExpr, want) {
			t.Fatalf("resource_service group expression = %q, want %s", serviceExpr, want)
		}
	}
}
