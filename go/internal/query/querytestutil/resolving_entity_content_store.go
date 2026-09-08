// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ResolvingEntityContentStore is the entity-resolve read double: it answers
// GetEntityContent from a fixture map plus ListRepositories from a fixture
// slice, embedding FakePortContentStore for the rest of the content port.
//
// It lives here rather than in an entity _test.go file for the reason that
// shapes all of epic #6053 (#6060): a symbol declared in a _test.go file is
// not part of the importable package, so the staying root resolve tests
// cannot reach a double declared in the moved entity family tests. The
// fields are exported because an unexported field is unreachable from
// another package.
type ResolvingEntityContentStore struct {
	FakePortContentStore
	EntitiesByID map[string]querycontract.EntityContent
	Repositories []querycontract.RepositoryCatalogEntry
}

// GetEntityContent answers one entity by ID from the fixture map.
func (s ResolvingEntityContentStore) GetEntityContent(_ context.Context, entityID string) (*querycontract.EntityContent, error) {
	entity, ok := s.EntitiesByID[entityID]
	if !ok {
		return nil, nil
	}
	return &entity, nil
}

// ListRepositories answers the fixture repository catalog.
func (s ResolvingEntityContentStore) ListRepositories(context.Context) ([]querycontract.RepositoryCatalogEntry, error) {
	return append([]querycontract.RepositoryCatalogEntry(nil), s.Repositories...), nil
}
