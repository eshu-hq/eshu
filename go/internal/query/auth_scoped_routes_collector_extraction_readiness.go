// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

// scopedCollectorExtractionReadinessRoute reports whether the request targets a
// collector extraction readiness read. These routes serve advisory static
// extraction-policy classification computed from documented repository
// evidence: the payload is identical for every caller and carries no
// repository, tenant, or runtime-state data. There is therefore nothing to
// tenant-filter, and the route is safe for scoped tokens the same way the
// global runtime-status readbacks are. It stays GET-only and matches the list
// route plus the single bounded `{family}` drilldown segment.
func scopedCollectorExtractionReadinessRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if r.URL.Path == "/api/v0/collector-extraction-readiness" {
		return true
	}
	const prefix = "/api/v0/collector-extraction-readiness/"
	family := strings.TrimPrefix(r.URL.Path, prefix)
	return family != r.URL.Path && family != "" && !strings.Contains(family, "/")
}
