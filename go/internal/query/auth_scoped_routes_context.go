// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

// This file holds the scoped-token allowlist matchers for the entity, incident,
// workload, service, and investigation read surfaces. They are split out of
// auth_scoped_routes.go (which owns the auth/admin allowlist) to keep each file
// under the 500-line cap; the scoped-route matchers are already deliberately
// spread across sibling files in this package (see scopedContextRoute in
// auth_scoped_routes_status_semantic.go), so a reviewer audits the full
// allowlist across the auth_scoped_routes_*.go set, not a single file.

func scopedEntityContextRoute(path string) bool {
	const (
		prefix = "/api/v0/entities/"
		suffix = "/context"
	)
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return false
	}
	entityID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return entityID != "" && !strings.Contains(entityID, "/")
}

// scopedIncidentContextRoute reports whether the request targets the
// single-incident context read GET /api/v0/incidents/{incident_id}/context. The
// handler authorizes the read against the reducer-owned durable
// incident→repository correlation edge (reducer_incident_repository_correlation,
// exact/derived outcomes only): an incident whose durable owning repository is
// outside the scoped grant, or that has no durable edge at all, is served as
// not-found with no existence disclosure. Adjacent incident sub-resources stay
// fail-closed for scoped tokens until each is separately proven tenant-filtered.
func scopedIncidentContextRoute(path string) bool {
	const (
		prefix = "/api/v0/incidents/"
		suffix = "/context"
	)
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return false
	}
	incidentID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return incidentID != "" && !strings.Contains(incidentID, "/")
}

func scopedWorkloadContextRoute(path string) bool {
	return scopedContextRoute(path, "/api/v0/workloads/")
}

func scopedServiceContextRoute(path string) bool {
	return scopedContextRoute(path, "/api/v0/services/")
}

func scopedServiceInvestigationRoute(path string) bool {
	const prefix = "/api/v0/investigations/services/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	selector := strings.TrimPrefix(path, prefix)
	return selector != "" && !strings.Contains(selector, "/")
}

// scopedServiceIntelligenceReportRoute matches the service intelligence report
// route. The report composes the service-story dossier through the same scoped
// access filter, so it qualifies for scoped-token tenant filtering exactly like
// the service-story route.
func scopedServiceIntelligenceReportRoute(path string) bool {
	const prefix = "/api/v0/services/"
	const suffix = "/intelligence-report"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	// CutSuffix (not TrimSuffix) so the suffix must be a distinct trailing
	// segment: "/api/v0/services/intelligence-report" (no service segment) does
	// not match because its remainder lacks the leading "/" of the suffix.
	selector, ok := strings.CutSuffix(strings.TrimPrefix(path, prefix), suffix)
	return ok && selector != "" && !strings.Contains(selector, "/")
}

func scopedQueryPlaybookRoute(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v0/query-playbooks":
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/api/v0/query-playbooks/resolve":
		return true
	default:
		return false
	}
}

func scopedInvestigationWorkflowRoute(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v0/investigation-workflows":
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/api/v0/investigation-workflows/resolve":
		return true
	default:
		return false
	}
}
