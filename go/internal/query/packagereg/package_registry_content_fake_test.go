// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagereg

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// fakePackageRegistryContentStore is a minimal querycontract.ContentStore
// double for this family's repository-selector tests. It embeds the (nil)
// interface to satisfy ContentStore's other ~25 methods, none of which this
// family's tests call, and overrides only MatchRepositories -- the single
// method queryselector.ResolveExactForAccess reads off a non-nil ContentStore.
//
// Root's much larger fakePortContentStore (internal/query/ports_test.go)
// cannot be reused here: Go never compiles one package's _test.go files into
// anything another package can import, so this is this family's own copy of
// just the slice it needs, not a fork of the shared fake's full surface.
type fakePackageRegistryContentStore struct {
	querycontract.ContentStore
	repositories []querycontract.RepositoryCatalogEntry
}

// MatchRepositories returns the canned repository catalog entries regardless
// of selector; callers that need selector-sensitive matching should filter
// repositories before constructing the fake.
func (f fakePackageRegistryContentStore) MatchRepositories(_ context.Context, _ string) ([]querycontract.RepositoryCatalogEntry, error) {
	return f.repositories, nil
}

// repositorySelectorReadModelContentStore returns a fake ContentStore with
// one canned repository (repo://example/api), matching root's
// repositorySelectorReadModelContentStore fixture used by the equivalent
// pre-move tests.
func repositorySelectorReadModelContentStore() fakePackageRegistryContentStore {
	return fakePackageRegistryContentStore{
		repositories: []querycontract.RepositoryCatalogEntry{{
			ID:        "repo://example/api",
			Name:      "payments-api",
			LocalPath: "/srv/payments-api",
			RepoSlug:  "example/payments-api",
		}},
	}
}
