// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/queryselector"
)

// The repository-selector resolution moved to queryselector for #6060, so a
// handler-family subpackage can resolve a selector without importing this
// package, which it cannot do without an import cycle. It is not in
// querycontract because resolveRepositorySelectorForRequestWithAccess writes to
// a ResponseWriter, and request-time orchestration in the dependency-neutral
// contract package is exactly what review rejected on the collector-readiness
// seam.

// repositorySelectorNotFoundError reports a selector that matched no
// repository. Aliased, so the errors.As in isRepositorySelectorNotFound and
// every existing construction keep working.
type repositorySelectorNotFoundError = queryselector.NotFoundError

// repositorySelectorAmbiguousError reports a selector that matched more than
// one repository.
type repositorySelectorAmbiguousError = queryselector.AmbiguousError

func resolveRepositorySelectorExactForAccess(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	selector string,
	access repositoryAccessFilter,
) (string, error) {
	return queryselector.ResolveExactForAccess(ctx, graph, content, selector, access)
}

func resolveRepositorySelectorForRequestWithAccess(
	w http.ResponseWriter,
	r *http.Request,
	graph GraphQuery,
	content ContentStore,
	selector string,
	access repositoryAccessFilter,
	capability string,
) (string, bool) {
	return queryselector.ResolveForRequestWithAccess(w, r, graph, content, selector, access, capability)
}

func isRepositorySelectorNotFound(err error) bool {
	return queryselector.IsNotFound(err)
}

func resolveRepositoryCatalogMatches(entries []RepositoryCatalogEntry, selector string) []string {
	return queryselector.CatalogMatches(entries, selector)
}
