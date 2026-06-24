// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

// scopedCollectorStatusRoute allows scoped tokens to reach the collector status
// list; the handler collapses per-instance rows into aggregate readback.
func scopedCollectorStatusRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/status/collectors"
}

// scopedCollectorReadinessRoute allows scoped tokens to reach the collector
// readiness read model. The handler redacts the per-instance identifier for
// scoped callers, so the route must be reachable for that redaction to apply.
func scopedCollectorReadinessRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	return r.URL.Path == "/api/v0/status/collector-readiness" ||
		r.URL.Path == "/api/v0/collector-readiness"
}

// scopedIngesterStatusRoute allows scoped tokens to reach the ingester status
// list and per-ingester detail routes.
func scopedIngesterStatusRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if r.URL.Path == "/api/v0/status/ingesters" {
		return true
	}
	const prefix = "/api/v0/status/ingesters/"
	ingester := strings.TrimPrefix(r.URL.Path, prefix)
	return ingester != r.URL.Path && ingester != "" && !strings.Contains(ingester, "/")
}
