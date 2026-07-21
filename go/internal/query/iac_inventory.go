// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

const iacInventoryFacetLimit = 200

// IaCInventoryStore reads the active-generation Postgres inventory used to
// exclude retained historical graph nodes and to serve full-inventory search
// and facets without loading the corpus into the browser.
type IaCInventoryStore interface {
	SearchActive(context.Context, iacInventorySearch, repositoryAccessFilter) ([]iacInventoryCandidate, error)
	Summary(context.Context, repositoryAccessFilter, int) (iacInventorySummary, error)
}

type iacInventorySearch struct {
	Kind       iacResourceKind
	Query      string
	Type       string
	Provider   string
	Module     string
	Repository string
	AfterName  string
	AfterID    string
	Limit      int
}

type iacInventoryCandidate struct {
	ID           string
	Name         string
	GenerationID string
}

type iacInventoryFacet struct {
	Kind  iacResourceKind `json:"kind,omitempty"`
	Value string          `json:"value"`
	Count int             `json:"count"`
}

type iacInventorySummary struct {
	Total        int                     `json:"total"`
	ByKind       map[iacResourceKind]int `json:"by_kind"`
	Types        []iacInventoryFacet     `json:"types"`
	Providers    []iacInventoryFacet     `json:"providers"`
	Modules      []iacInventoryFacet     `json:"modules"`
	Repositories []iacInventoryFacet     `json:"repositories"`
	FacetLimit   int                     `json:"facet_limit"`
	Truncated    map[string]bool         `json:"truncated"`
}

func newIaCInventorySummary(limit int) iacInventorySummary {
	return iacInventorySummary{
		ByKind: map[iacResourceKind]int{
			iacResourceKindResource:   0,
			iacResourceKindModule:     0,
			iacResourceKindDataSource: 0,
		},
		Types:        []iacInventoryFacet{},
		Providers:    []iacInventoryFacet{},
		Modules:      []iacInventoryFacet{},
		Repositories: []iacInventoryFacet{},
		FacetLimit:   limit,
		Truncated:    make(map[string]bool, 4),
	}
}

func searchActiveIaCInventory(
	ctx context.Context,
	store IaCInventoryStore,
	kind iacResourceKind,
	query string,
	filter iacResourceFilter,
	access repositoryAccessFilter,
) ([]iacInventoryCandidate, error) {
	return store.SearchActive(ctx, iacInventorySearch{
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
