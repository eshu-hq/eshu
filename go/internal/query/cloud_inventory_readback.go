// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// cloudInventoryReadbackCapability is the conformance-matrix capability id for
// the canonical multi-cloud resource inventory readback. It is gated to
// reducer-owning profiles; local_lightweight returns unsupported_capability
// because it cannot materialize the reducer_cloud_resource_identity rows.
const cloudInventoryReadbackCapability = "cloud_inventory.readback.list"

const (
	cloudInventoryReadbackMaxLimit     = 200
	cloudInventoryReadbackDefaultLimit = 50
)

// cloudInventoryManagementOriginDeclared, cloudInventoryManagementOriginApplied,
// and cloudInventoryManagementOriginObserved mirror the reducer ManagementOrigin
// precedence (declared > applied > observed) that the canonical
// reducer_cloud_resource_identity payload records. The readback validates the
// management_origin filter against this closed set so an unrecognized value can
// never silently widen the query.
const (
	cloudInventoryManagementOriginDeclared = "declared"
	cloudInventoryManagementOriginApplied  = "applied"
	cloudInventoryManagementOriginObserved = "observed"
)

// cloudInventoryProviders is the closed set of providers the canonical inventory
// readback accepts as a filter. It matches the multi-cloud collector contract
// providers; an unrecognized provider is rejected as invalid input rather than
// silently ignored.
var cloudInventoryProviders = map[string]struct{}{
	"aws":   {},
	"gcp":   {},
	"azure": {},
}

// cloudInventoryManagementOrigins is the closed set of management_origin filter
// values, keyed for validation.
var cloudInventoryManagementOrigins = map[string]struct{}{
	cloudInventoryManagementOriginDeclared: {},
	cloudInventoryManagementOriginApplied:  {},
	cloudInventoryManagementOriginObserved: {},
}

// CloudInventoryHandler serves a bounded, paginated, truth-labeled readback of
// canonical multi-cloud resource identities from the reducer-owned
// reducer_cloud_resource_identity rows. It is read-only and never fabricates
// identity: it projects only the reducer-resolved canonical fields and never
// echoes raw provider locators, tags, or credentials.
type CloudInventoryHandler struct {
	// Content is the relational store; it must also implement
	// cloudInventoryReadModelStore (ContentReader does) for the readback to serve.
	Content ContentStore
	// Profile selects the active runtime profile for capability gating.
	Profile QueryProfile
}

// cloudInventoryReadModelStore reads canonical CloudResource identity rows from
// the durable fact store. ContentReader implements it; the handler type-asserts
// h.Content to this narrow interface so unit tests can supply a fixture-backed
// reader without a live database or graph backend.
type cloudInventoryReadModelStore interface {
	cloudInventoryIdentities(context.Context, cloudInventoryFilter) (cloudInventoryListReadModel, error)
}

// cloudInventoryFilter holds the optional, bounded filters for the readback.
// Empty values mean "no filter". Scope, account, project, and subscription all
// match the canonical scope_id selector because the reducer keys canonical rows
// by scope; provider determines which of those scope kinds applies.
type cloudInventoryFilter struct {
	Provider         string
	ScopeID          string
	ManagementOrigin string
	Limit            int
	Offset           int
}

// cloudInventoryListReadModel is the bounded page of canonical identity payloads
// plus the keyset continuation offset.
type cloudInventoryListReadModel struct {
	Resources  []map[string]any
	NextCursor string
}

// Mount registers the canonical cloud inventory readback route.
func (h *CloudInventoryHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/cloud/inventory", h.listInventory)
}

func (h *CloudInventoryHandler) profile() QueryProfile {
	if h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

// listInventory serves the bounded, filterable, paginated readback of canonical
// multi-cloud resource identities.
//
// GET /api/v0/cloud/inventory?provider=&scope_id=&management_origin=&limit=&cursor=
func (h *CloudInventoryHandler) listInventory(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCloudInventoryReadback,
		"GET /api/v0/cloud/inventory",
		cloudInventoryReadbackCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), cloudInventoryReadbackCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"cloud inventory readback requires reducer-owned canonical CloudResource identity rows",
			ErrorCodeUnsupportedCapability,
			cloudInventoryReadbackCapability,
			h.profile(),
			requiredProfile(cloudInventoryReadbackCapability),
		)
		return
	}

	filter, ok := h.filterFromRequest(w, r)
	if !ok {
		return
	}
	store, ok := h.store(w, r)
	if !ok {
		return
	}
	readModel, err := store.cloudInventoryIdentities(r.Context(), filter)
	if err != nil {
		WriteContractError(
			w,
			r,
			http.StatusInternalServerError,
			"cloud inventory readback failed",
			ErrorCodeInternalError,
			cloudInventoryReadbackCapability,
			h.profile(),
			requiredProfile(cloudInventoryReadbackCapability),
		)
		return
	}

	WriteSuccess(w, r, http.StatusOK, cloudInventoryResponse(readModel, filter), BuildTruthEnvelope(
		h.profile(),
		cloudInventoryReadbackCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned canonical CloudResource identity facts (reducer_cloud_resource_identity)",
	))
}

// store resolves the canonical inventory read model from h.Content. It mirrors
// the documentation handler pattern: the Postgres-backed ContentReader satisfies
// the narrow read interface, and a missing or incompatible store is reported as
// an explicit read-model-unavailable error rather than a silent empty result.
func (h *CloudInventoryHandler) store(w http.ResponseWriter, r *http.Request) (cloudInventoryReadModelStore, bool) {
	if h.Content == nil {
		h.writeReadModelUnavailable(w, r)
		return nil, false
	}
	store, ok := h.Content.(cloudInventoryReadModelStore)
	if !ok {
		h.writeReadModelUnavailable(w, r)
		return nil, false
	}
	return store, true
}

func (h *CloudInventoryHandler) writeReadModelUnavailable(w http.ResponseWriter, r *http.Request) {
	WriteContractError(
		w,
		r,
		http.StatusNotImplemented,
		"cloud inventory readback requires the Postgres canonical identity read model",
		ErrorCodeReadModelUnavailable,
		cloudInventoryReadbackCapability,
		h.profile(),
		requiredProfile(cloudInventoryReadbackCapability),
	)
}

// filterFromRequest parses and validates the bounded request filters. Unknown
// provider or management_origin values are rejected as invalid input so an
// unrecognized filter never silently returns the full inventory.
func (h *CloudInventoryHandler) filterFromRequest(w http.ResponseWriter, r *http.Request) (cloudInventoryFilter, bool) {
	provider := strings.ToLower(strings.TrimSpace(QueryParam(r, "provider")))
	if provider != "" {
		if _, known := cloudInventoryProviders[provider]; !known {
			h.writeInvalidArgument(w, r, "provider must be one of aws, gcp, or azure")
			return cloudInventoryFilter{}, false
		}
	}
	managementOrigin := strings.ToLower(strings.TrimSpace(QueryParam(r, "management_origin")))
	if managementOrigin != "" {
		if _, known := cloudInventoryManagementOrigins[managementOrigin]; !known {
			h.writeInvalidArgument(w, r, "management_origin must be one of declared, applied, or observed")
			return cloudInventoryFilter{}, false
		}
	}
	limit, offset, ok := h.pagination(w, r)
	if !ok {
		return cloudInventoryFilter{}, false
	}
	return cloudInventoryFilter{
		Provider:         provider,
		ScopeID:          strings.TrimSpace(cloudInventoryScopeSelector(r)),
		ManagementOrigin: managementOrigin,
		Limit:            limit,
		Offset:           offset,
	}, true
}

// cloudInventoryScopeSelector resolves the canonical scope filter. The readback
// accepts scope_id directly and the provider-flavored aliases account_id,
// project_id, and subscription_id, all of which target the same canonical
// scope_id column. The first non-empty alias wins; they are mutually consistent
// because a given row belongs to exactly one canonical scope.
func cloudInventoryScopeSelector(r *http.Request) string {
	for _, key := range []string{"scope_id", "account_id", "project_id", "subscription_id"} {
		if value := strings.TrimSpace(QueryParam(r, key)); value != "" {
			return value
		}
	}
	return ""
}

func (h *CloudInventoryHandler) writeInvalidArgument(w http.ResponseWriter, r *http.Request, message string) {
	WriteContractError(
		w,
		r,
		http.StatusBadRequest,
		message,
		ErrorCodeInvalidArgument,
		cloudInventoryReadbackCapability,
		h.profile(),
		requiredProfile(cloudInventoryReadbackCapability),
	)
}

// pagination parses the bounded limit and the keyset cursor (a non-negative
// integer offset). limit defaults to cloudInventoryReadbackDefaultLimit and is
// capped at cloudInventoryReadbackMaxLimit; out-of-range values are rejected.
func (h *CloudInventoryHandler) pagination(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	limit := cloudInventoryReadbackDefaultLimit
	if raw := strings.TrimSpace(QueryParam(r, "limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > cloudInventoryReadbackMaxLimit {
			h.writeInvalidArgument(w, r, "limit must be an integer between 1 and 200")
			return 0, 0, false
		}
		limit = parsed
	}
	offset := 0
	if raw := strings.TrimSpace(QueryParam(r, "cursor")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			h.writeInvalidArgument(w, r, "cursor must be a non-negative integer offset")
			return 0, 0, false
		}
		offset = parsed
	}
	return limit, offset, true
}

// cloudInventoryResponse builds the bounded list envelope body. Each resource is
// projected through cloudInventoryResourceView so raw provider locators never
// reach the wire and every row carries its provider-neutral source state.
func cloudInventoryResponse(readModel cloudInventoryListReadModel, filter cloudInventoryFilter) map[string]any {
	resources := make([]map[string]any, 0, len(readModel.Resources))
	for _, payload := range readModel.Resources {
		resources = append(resources, cloudInventoryResourceView(payload))
	}
	nextCursor := strings.TrimSpace(readModel.NextCursor)
	body := map[string]any{
		"resources": resources,
		"count":     len(resources),
		"limit":     filter.Limit,
		"truncated": nextCursor != "",
		"scope":     cloudInventoryScope(filter),
	}
	if nextCursor != "" {
		body["next_cursor"] = nextCursor
	}
	return body
}

// cloudInventoryScope reports the bounded, non-sensitive filter scope applied to
// the readback so a caller can confirm what was queried without echoing raw
// provider identity. Empty filters are omitted.
func cloudInventoryScope(filter cloudInventoryFilter) map[string]any {
	scope := map[string]any{}
	if filter.Provider != "" {
		scope["provider"] = filter.Provider
	}
	if filter.ScopeID != "" {
		scope["scope_id"] = filter.ScopeID
	}
	if filter.ManagementOrigin != "" {
		scope["management_origin"] = filter.ManagementOrigin
	}
	return scope
}
