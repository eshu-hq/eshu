// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// parseCloudResourceListLimit reads and validates the page size. limit is
// optional and defaults to cloudResourceListDefaultLimit; it must be between 1
// and cloudResourceListMaxLimit so a single page stays bounded. On an invalid
// value it writes a 400 and returns ok=false.
func parseCloudResourceListLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(QueryParam(r, "limit"))
	if raw == "" {
		return cloudResourceListDefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > cloudResourceListMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", cloudResourceListMaxLimit))
		return 0, false
	}
	return limit, true
}

// cloudResourceListFilterFromRequest extracts the optional equality filters.
// Values pass through as bound Cypher parameters, so no Go-side enum validation
// is required; an unknown value simply returns no rows.
func cloudResourceListFilterFromRequest(r *http.Request) cloudResourceListFilter {
	return cloudResourceListFilter{
		Provider:     strings.TrimSpace(QueryParam(r, "provider")),
		ResourceType: strings.TrimSpace(QueryParam(r, "resource_type")),
		Region:       strings.TrimSpace(QueryParam(r, "region")),
		AccountID:    strings.TrimSpace(QueryParam(r, "account_id")),
	}
}

// parseCloudResourceListCursor extracts and validates the keyset continuation
// anchor. Both parameters are required together; rejecting a partial cursor
// avoids silently restarting at the first page.
func parseCloudResourceListCursor(w http.ResponseWriter, r *http.Request) (cloudResourceListCursor, bool) {
	values := r.URL.Query()
	hasResourceType := values.Has("after_resource_type")
	hasID := values.Has("after_id")
	if hasResourceType != hasID {
		WriteError(w, http.StatusBadRequest, "after_resource_type and after_id must be provided together")
		return cloudResourceListCursor{}, false
	}
	if !hasResourceType {
		return cloudResourceListCursor{}, true
	}
	afterResourceType := strings.TrimSpace(QueryParam(r, "after_resource_type"))
	afterID := strings.TrimSpace(QueryParam(r, "after_id"))
	if afterID == "" {
		WriteError(w, http.StatusBadRequest, "after_id must not be empty")
		return cloudResourceListCursor{}, false
	}
	return cloudResourceListCursor{
		AfterResourceType: afterResourceType,
		AfterID:           afterID,
	}, true
}

// cloudResourceListScope echoes the applied filters back to the caller so the
// response is self-describing. Empty filters are omitted.
func cloudResourceListScope(filter cloudResourceListFilter) map[string]string {
	out := map[string]string{}
	if filter.Provider != "" {
		out["provider"] = filter.Provider
	}
	if filter.ResourceType != "" {
		out["resource_type"] = filter.ResourceType
	}
	if filter.Region != "" {
		out["region"] = filter.Region
	}
	if filter.AccountID != "" {
		out["account_id"] = filter.AccountID
	}
	return out
}
