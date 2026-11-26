// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type refusingRouterReadStore struct{ db.ReadStore }

var errRouterReaderRefused = errors.New("reader refused")

func (refusingRouterReadStore) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errRouterReaderRefused
}

func TestRouterUsesReadStoreForSemanticScope(t *testing.T) {
	router, err := newRouterWithSemanticEmbeddingWithReadStore(nil, refusingRouterReadStore{}, nil, nil, nil, nil, query.QueryProfile(""), query.GraphBackend(""), nil, nil, searchembedruntime.Config{}, "", component.Policy{}, query.GovernanceStatusConfig{}, nil, false, query.CookieSecureMode(""))
	if err != nil {
		t.Fatal(err)
	}
	_, err = router.SemanticSearch.ScopeResolver.ResolveSemanticSearchScope(t.Context(), "repo")
	if !errors.Is(err, errRouterReaderRefused) {
		t.Fatalf("semantic scope error = %v, want guarded reader refusal", err)
	}
	_, err = router.Infra.CloudResources.ListCloudResourceIdentities(t.Context(), query.CloudResourceListPageFilter{Limit: 1})
	if !errors.Is(err, errRouterReaderRefused) {
		t.Fatalf("cloud resource error = %v, want guarded reader refusal", err)
	}
	_, err = router.Repositories.Freshness.ReadRepositoryFreshness(t.Context(), "repo")
	if !errors.Is(err, errRouterReaderRefused) {
		t.Fatalf("freshness error = %v, want guarded reader refusal", err)
	}
	hybrid, ok := newSemanticSearchHybridWithReadStore(refusingRouterReadStore{}, searchembedruntime.Config{Enabled: true}, nil).(*query.PersistedLocalSemanticSearchHybrid)
	if !ok {
		t.Fatal("semantic hybrid is not persisted local vector backend")
	}
	_, err = hybrid.Metadata.ListActive(t.Context(), pgstatus.EshuSearchVectorMetadataFilter{ScopeID: "scope", ProviderProfileID: "local", SourceClass: "search_documents", EmbeddingModelID: "model", VectorIndexVersion: "v1", Limit: 1})
	if !errors.Is(err, errRouterReaderRefused) {
		t.Fatalf("vector metadata error = %v, want guarded reader refusal", err)
	}
	_, err = router.Admin.ReadStore.ListWorkItems(t.Context(), query.WorkItemFilter{Limit: 1})
	if !errors.Is(err, errRouterReaderRefused) {
		t.Fatalf("admin list error = %v, want guarded reader refusal", err)
	}
	_, err = router.Status.GovernanceAudit.Summary(t.Context())
	if !errors.Is(err, errRouterReaderRefused) {
		t.Fatalf("audit summary error = %v, want guarded reader refusal", err)
	}
}
