// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	contractImpactCapability     = "platform_impact.contract_impact"
	contractImpactDefaultLimit   = 25
	contractImpactMaxLimit       = 100
	contractImpactFamilyHTTP     = "http"
	contractImpactFamilyTopic    = "topic"
	contractImpactFamilyGRPC     = "grpc"
	contractImpactStateSupported = "deterministic"
	contractImpactStateDeferred  = "unsupported"
)

var errContractImpactGraphUnavailable = errors.New("graph backend is unavailable")

type contractImpactRequest struct {
	Family         string `json:"family"`
	ProviderRepoID string `json:"provider_repo_id"`
	ConsumerRepoID string `json:"consumer_repo_id"`
	RepoID         string `json:"repo_id"`
	Route          string `json:"route"`
	Topic          string `json:"topic"`
	ServiceName    string `json:"service_name"`
	Method         string `json:"method"`
	Limit          int    `json:"limit"`
}

func (h *ImpactHandler) contractImpact(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryChangeSurfaceInvestigation,
		"POST /api/v0/impact/contracts",
		contractImpactCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), contractImpactCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"contract impact requires authoritative graph-backed platform truth",
			ErrorCodeUnsupportedCapability,
			contractImpactCapability,
			h.profile(),
			requiredProfile(contractImpactCapability),
		)
		return
	}

	var req contractImpactRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalized, err := normalizeContractImpactRequest(req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := h.contractImpactResponse(r.Context(), normalized)
	if err != nil {
		if errors.Is(err, errContractImpactGraphUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		if WriteGraphReadError(w, r, err, contractImpactCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truth := BuildTruthEnvelope(
		h.profile(),
		contractImpactCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from deterministic contract registry evidence only",
	)
	WriteSuccess(w, r, http.StatusOK, resp, truth)
}

func normalizeContractImpactRequest(req contractImpactRequest) (contractImpactRequest, error) {
	req.Family = strings.ToLower(strings.TrimSpace(req.Family))
	req.ProviderRepoID = strings.TrimSpace(firstNonEmptyString(req.ProviderRepoID, req.RepoID))
	req.ConsumerRepoID = strings.TrimSpace(req.ConsumerRepoID)
	req.RepoID = ""
	req.Route = strings.TrimSpace(req.Route)
	req.Topic = strings.TrimSpace(req.Topic)
	req.ServiceName = strings.TrimSpace(req.ServiceName)
	req.Method = strings.ToLower(strings.TrimSpace(req.Method))
	if req.Family == "" {
		req.Family = contractImpactFamilyHTTP
	}
	if req.Limit <= 0 {
		req.Limit = contractImpactDefaultLimit
	}
	if req.Limit > contractImpactMaxLimit {
		req.Limit = contractImpactMaxLimit
	}
	switch req.Family {
	case contractImpactFamilyHTTP, contractImpactFamilyTopic, contractImpactFamilyGRPC:
	default:
		return contractImpactRequest{}, fmt.Errorf("family must be one of http, topic, or grpc")
	}
	if req.ProviderRepoID == "" && req.ConsumerRepoID == "" && req.Route == "" && req.Topic == "" && req.ServiceName == "" {
		return contractImpactRequest{}, fmt.Errorf("provider_repo_id, consumer_repo_id, route, topic, or service_name scope is required")
	}
	if req.Family == contractImpactFamilyHTTP && req.ProviderRepoID == "" {
		return contractImpactRequest{}, fmt.Errorf("provider_repo_id is required for http contract registry reads")
	}
	return req, nil
}

func (h *ImpactHandler) contractImpactResponse(
	ctx context.Context,
	req contractImpactRequest,
) (map[string]any, error) {
	resp := map[string]any{
		"family":    req.Family,
		"scope":     contractImpactScope(req),
		"families":  contractImpactFamilyStates(req.Family),
		"providers": []map[string]any{},
		"consumers": []map[string]any{},
		"limit":     req.Limit,
		"truncated": false,
		"coverage": map[string]any{
			"query_shape":             "contract_registry",
			"deterministic_only":      true,
			"string_similarity_edges": false,
		},
	}
	if req.Family != contractImpactFamilyHTTP {
		resp["coverage"].(map[string]any)["query_shape"] = "contract_registry_deferred_family"
		return resp, nil
	}
	if h == nil || h.Neo4j == nil {
		return nil, errContractImpactGraphUnavailable
	}
	// #5167 W3: the only implemented family (http) is anchored on an exact
	// provider_repo_id (required by normalizeContractImpactRequest), so this is
	// the repo-scoped-selector pattern -- deny-by-default when the requested
	// repo is outside the caller's grant, mirroring an unknown/nonexistent
	// provider_repo_id (empty providers, no query issued) rather than
	// distinguishing "not found" from "not yours" to a scoped caller.
	access := repositoryAccessFilterFromContext(ctx)
	if !impactRepoIDAllowed(req.ProviderRepoID, access) {
		return resp, nil
	}
	rows, err := h.Neo4j.Run(ctx, contractImpactHTTPProviderCypher(), map[string]any{
		"provider_repo_id": req.ProviderRepoID,
		"route":            req.Route,
		"method":           req.Method,
		"method_upper":     strings.ToUpper(req.Method),
		"limit":            req.Limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("read contract impact providers: %w", err)
	}
	truncated := len(rows) > req.Limit
	if truncated {
		rows = rows[:req.Limit]
	}
	resp["providers"] = contractImpactHTTPProviders(rows, req)
	resp["truncated"] = truncated
	resp["coverage"].(map[string]any)["query_shape"] = "endpoint_contract_registry_by_provider_repo"
	return resp, nil
}

func contractImpactHTTPProviderCypher() string {
	return `
MATCH (provider:Repository {id: $provider_repo_id})-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)
WHERE ($route = "" OR endpoint.path = $route)
  AND ($method = "" OR $method IN endpoint.methods OR $method_upper IN endpoint.methods)
RETURN endpoint.id AS endpoint_id,
       provider.id AS provider_repo_id,
       provider.name AS provider_repo,
       endpoint.path AS path,
       endpoint.methods AS methods,
       endpoint.source_kinds AS source_kinds,
       endpoint.source_paths AS source_paths,
       endpoint.operation_ids AS operation_ids,
       endpoint.workload_id AS workload_id,
       endpoint.workload_name AS workload_name
ORDER BY endpoint.path, endpoint.id
LIMIT $limit`
}

func contractImpactHTTPProviders(rows []map[string]any, req contractImpactRequest) []map[string]any {
	providers := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		methods := lowerStrings(StringSliceVal(row, "methods"))
		if req.Method != "" {
			methods = filterStrings(methods, req.Method)
		}
		if len(methods) == 0 {
			methods = []string{""}
		}
		for _, method := range methods {
			provider := map[string]any{
				"family":           contractImpactFamilyHTTP,
				"contract_key":     contractImpactKey(contractImpactFamilyHTTP, StringVal(row, "provider_repo_id"), StringVal(row, "path"), method),
				"provider_repo_id": StringVal(row, "provider_repo_id"),
				"provider_repo":    StringVal(row, "provider_repo"),
				"route":            StringVal(row, "path"),
				"http_method":      method,
				"endpoint_id":      StringVal(row, "endpoint_id"),
				"source_kinds":     uniqueSortedStrings(StringSliceVal(row, "source_kinds")),
				"source_paths":     uniqueSortedStrings(StringSliceVal(row, "source_paths")),
				"operation_ids":    uniqueSortedStrings(StringSliceVal(row, "operation_ids")),
				"evidence_state":   contractImpactStateSupported,
			}
			if workloadID := StringVal(row, "workload_id"); workloadID != "" {
				provider["workload_id"] = workloadID
			}
			if workloadName := StringVal(row, "workload_name"); workloadName != "" {
				provider["workload_name"] = workloadName
			}
			providers = append(providers, provider)
		}
	}
	slices.SortFunc(providers, func(a, b map[string]any) int {
		return strings.Compare(StringVal(a, "contract_key"), StringVal(b, "contract_key"))
	})
	return providers
}

func contractImpactFamilyStates(selected string) map[string]any {
	return map[string]any{
		contractImpactFamilyHTTP: map[string]any{
			"state":  contractImpactStateSupported,
			"reason": "endpoint_graph_registry_available",
		},
		contractImpactFamilyTopic: map[string]any{
			"state":  contractImpactStateDeferred,
			"reason": "topic_queue_contract_projection_not_implemented",
		},
		contractImpactFamilyGRPC: map[string]any{
			"state":  contractImpactStateDeferred,
			"reason": "protobuf_grpc_contract_projection_not_implemented",
		},
		"selected": selected,
	}
}

func contractImpactScope(req contractImpactRequest) map[string]any {
	scope := map[string]any{"family": req.Family}
	if req.ProviderRepoID != "" {
		scope["provider_repo_id"] = req.ProviderRepoID
	}
	if req.ConsumerRepoID != "" {
		scope["consumer_repo_id"] = req.ConsumerRepoID
	}
	if req.Route != "" {
		scope["route"] = req.Route
	}
	if req.Topic != "" {
		scope["topic"] = req.Topic
	}
	if req.ServiceName != "" {
		scope["service_name"] = req.ServiceName
	}
	if req.Method != "" {
		scope["method"] = req.Method
	}
	return scope
}

func contractImpactKey(family, repoID, target, method string) string {
	parts := []string{family, repoID, target}
	if method != "" {
		parts = append(parts, method)
	}
	return strings.Join(parts, ":")
}

func filterStrings(values []string, want string) []string {
	out := values[:0]
	for _, value := range values {
		if value == want {
			out = append(out, value)
		}
	}
	return out
}
