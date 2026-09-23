// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B3 selector forwarder shim for #6060: the request-orchestration forwarders are excluded from querycontract by review, so they must stay in package query for root callers.

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
)

// The repository-selector resolution moved to selector for #6060, so a
// handler-family subpackage can resolve a selector without importing this
// package, which it cannot do without an import cycle. It is not in
// querycontract because resolveRepositorySelectorForRequestWithAccess writes to
// a ResponseWriter, and request-time orchestration in the dependency-neutral
// contract package is exactly what review rejected on the collector-readiness
// seam.

// resolveRepositorySelectorExactForAccess's only caller was iac.go's
// handleDeadIaC. It moved to iac/handler.go (#6642 Part A) and calls
// selector.ResolveExactForAccess directly (the leaf can import
// selector without a cycle), so this root forwarder is dead and was
// removed rather than kept as an unused wrapper.

func resolveRepositorySelectorForRequestWithAccess(
	w http.ResponseWriter,
	r *http.Request,
	graph GraphQuery,
	content ContentStore,
	rawSelector string,
	access querycontract.RepositoryAccessFilter,
	capability string,
) (string, bool) {
	return selector.ResolveForRequestWithAccess(w, r, graph, content, rawSelector, access, capability)
}
