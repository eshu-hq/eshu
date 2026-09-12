// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const inventoryFacetLimit = 200

// InventoryStore reads the active-generation Postgres inventory used to
// exclude retained historical graph nodes and to serve full-inventory search
// and facets without loading the corpus into the browser.
type InventoryStore interface {
	SearchActive(context.Context, InventorySearch, querycontract.RepositoryAccessFilter) ([]InventoryCandidate, error)
	Summary(context.Context, querycontract.RepositoryAccessFilter, int) (InventorySummary, error)
}

type InventorySearch struct {
	Kind       resourceKind
	Query      string
	Type       string
	Provider   string
	Module     string
	Repository string
	AfterName  string
	AfterID    string
	Limit      int
}

type InventoryCandidate struct {
	ID           string
	Name         string
	GenerationID string
}

type InventoryFacet struct {
	Kind  resourceKind `json:"kind,omitempty"`
	Value string       `json:"value"`
	Count int          `json:"count"`
}

type InventorySummary struct {
	Total        int                  `json:"total"`
	ByKind       map[resourceKind]int `json:"by_kind"`
	Types        []InventoryFacet     `json:"types"`
	Providers    []InventoryFacet     `json:"providers"`
	Modules      []InventoryFacet     `json:"modules"`
	Repositories []InventoryFacet     `json:"repositories"`
	FacetLimit   int                  `json:"facet_limit"`
	Truncated    map[string]bool      `json:"truncated"`
}

func newIaCInventorySummary(limit int) InventorySummary {
	return InventorySummary{
		ByKind: map[resourceKind]int{
			resourceKindResource:   0,
			resourceKindModule:     0,
			resourceKindDataSource: 0,
		},
		Types:        []InventoryFacet{},
		Providers:    []InventoryFacet{},
		Modules:      []InventoryFacet{},
		Repositories: []InventoryFacet{},
		FacetLimit:   limit,
		Truncated:    make(map[string]bool, 4),
	}
}

func searchActiveIaCInventory(
	ctx context.Context,
	store InventoryStore,
	kind resourceKind,
	query string,
	filter resourceFilter,
	access querycontract.RepositoryAccessFilter,
) ([]InventoryCandidate, error) {
	return store.SearchActive(ctx, InventorySearch{
		Kind:       kind,
		Query:      query,
		Type:       filter.Type,
		Provider:   filter.Provider,
		Module:     filter.Module,
		Repository: filter.Repository,
		AfterName:  filter.AfterName,
		AfterID:    filter.AfterID,
		Limit:      filter.Limit,
	}, access)
}
