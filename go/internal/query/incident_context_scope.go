// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// Scoped-token authorization for the single-incident context read. A scoped
// token may read an incident's context only when the incident correlates to a
// granted repository through the reducer-owned durable incident→repository
// edge (reducer_incident_repository_correlation, exact/derived outcomes only).
// Every other case — empty grant, incident with no durable edge, or an incident
// whose durable owning repositories are all outside the grant — returns
// not-found with no existence disclosure, so a scoped caller can never tell an
// out-of-grant incident apart from one that does not exist. Shared, admin, and
// local callers are unscoped and skip this boundary entirely.

// authorizeScopedIncidentContext reports whether the request may proceed to the
// incident-context store read. It returns true for unscoped (shared/admin/local)
// callers without touching the durable edge. For scoped callers it fails closed:
// an empty grant is denied before any read; otherwise the incident's durable
// owning repositories are resolved through the authorizer and the read proceeds
// only when at least one is granted. Every denial writes an identical not-found
// envelope so the response never discloses whether the incident exists, which
// repository owns it, or whether the denial was an empty grant, a missing edge,
// or an out-of-grant owner.
func (h *IncidentHandler) authorizeScopedIncidentContext(
	w http.ResponseWriter,
	r *http.Request,
	provider string,
	providerIncidentID string,
	scopeID string,
) bool {
	access := repositoryAccessFilterFromContext(r.Context())
	if !access.scoped() {
		return true
	}
	if access.empty() {
		h.writeScopedIncidentContextNotFound(w, r)
		return false
	}
	if h.Authorizer == nil {
		// A scoped token reached a route that cannot resolve the durable edge.
		// Fail closed rather than serving an unauthorized incident.
		h.writeScopedIncidentContextNotFound(w, r)
		return false
	}
	repositories, err := h.Authorizer.ResolveDurableIncidentRepositories(
		r.Context(),
		provider,
		providerIncidentID,
		scopeID,
	)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "incident context authorization failed")
		return false
	}
	for _, repositoryID := range repositories {
		if access.allowsRepositoryID(repositoryID) {
			return true
		}
	}
	h.writeScopedIncidentContextNotFound(w, r)
	return false
}

// writeScopedIncidentContextNotFound writes the canonical not-found envelope for
// a scoped-token denial. The message is the same low-cardinality string the
// store uses for a genuinely missing incident, and carries no incident id,
// service id, or repository id, so error envelopes and metric labels stay free
// of tenant-identifying data.
func (h *IncidentHandler) writeScopedIncidentContextNotFound(
	w http.ResponseWriter,
	r *http.Request,
) {
	writeIncidentContextEnvelopeError(
		w,
		r,
		http.StatusNotFound,
		ErrorCodeNotFound,
		ErrIncidentContextNotFound.Error(),
		nil,
	)
}
