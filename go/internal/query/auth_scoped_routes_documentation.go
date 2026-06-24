// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

// Scoped-token route matchers for the documentation read route family. These
// are split out of auth_scoped_routes.go to keep the central gate file under the
// repository's source-file size cap; the dispatch in
// scopedHTTPRouteSupportsTenantFilter is unchanged.

func scopedDocumentationListRoute(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v0/documentation/findings":
		return true
	case r.Method == http.MethodGet && r.URL.Path == "/api/v0/documentation/facts":
		return true
	default:
		return false
	}
}

func scopedDocumentationAggregateRoute(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v0/documentation/findings/count":
		return true
	case r.Method == http.MethodGet && r.URL.Path == "/api/v0/documentation/findings/inventory":
		return true
	default:
		return false
	}
}

func scopedDocumentationEvidencePacketRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if scopedDocumentationFindingPacketRoute(r.URL.Path) {
		return true
	}
	return scopedDocumentationPacketFreshnessRoute(r.URL.Path)
}

func scopedDocumentationFindingPacketRoute(path string) bool {
	const (
		prefix = "/api/v0/documentation/findings/"
		suffix = "/evidence-packet"
	)
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return false
	}
	findingID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return findingID != "" && !strings.Contains(findingID, "/")
}

func scopedDocumentationPacketFreshnessRoute(path string) bool {
	const (
		prefix = "/api/v0/documentation/evidence-packets/"
		suffix = "/freshness"
	)
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return false
	}
	packetID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return packetID != "" && !strings.Contains(packetID, "/")
}
