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

// cloudResourceListCursorFromRequest extracts the keyset continuation anchor.
// Both halves must be present for the cursor predicate to apply; a lone
// after_resource_type without after_id is treated as no cursor so the query
// cannot silently drop the first page.
func cloudResourceListCursorFromRequest(r *http.Request) cloudResourceListCursor {
	afterResourceType := strings.TrimSpace(QueryParam(r, "after_resource_type"))
	afterID := strings.TrimSpace(QueryParam(r, "after_id"))
	if afterResourceType == "" || afterID == "" {
		return cloudResourceListCursor{}
	}
	return cloudResourceListCursor{
		AfterResourceType: afterResourceType,
		AfterID:           afterID,
	}
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
