// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strings"
)

const routeToCallerCapability = "call_graph.route_to_caller"

type routeToCallerRequest struct {
	RepoID      string `json:"repo_id"`
	ServiceID   string `json:"service_id"`
	ServiceName string `json:"service_name"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	MaxDepth    int    `json:"max_depth"`
	Limit       int    `json:"limit"`
}

type routeToCallerRoute struct {
	EndpointID  string
	Path        string
	RepoID      string
	Method      string
	Framework   string
	HandlerID   string
	HandlerName string
	FilePath    string
	Language    string
	StartLine   int
	EndLine     int
}

func (h *CodeHandler) handleRouteToCaller(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), routeToCallerCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"route-to-caller tracing requires authoritative graph mode",
			ErrorCodeUnsupportedCapability,
			routeToCallerCapability,
			h.profile(),
			requiredProfile(routeToCallerCapability),
		)
		return
	}

	var req routeToCallerRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.normalize()
	if err := req.validate(); err != nil {
		WriteErrorEnvelope(w, r, http.StatusBadRequest, &ErrorEnvelope{
			Code:       ErrorCodeInvalidArgument,
			Message:    err.Error(),
			Capability: routeToCallerCapability,
		})
		return
	}
	if h.Neo4j == nil {
		WriteErrorEnvelope(w, r, http.StatusServiceUnavailable, &ErrorEnvelope{
			Code:       ErrorCodeBackendUnavailable,
			Message:    "route-to-caller tracing requires a configured graph backend",
			Capability: routeToCallerCapability,
		})
		return
	}
	if !routeToCallerAllowedByScope(r, req) {
		h.writeRouteToCallerNotFound(w, r, "route not found")
		return
	}

	routeRows, err := h.routeToCallerRouteRows(r, req)
	if err != nil {
		if WriteGraphReadError(w, r, err, routeToCallerCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	route, status, ok := selectRouteToCallerRoute(routeRows)
	if !ok {
		switch status {
		case "not_found":
			h.writeRouteToCallerNotFound(w, r, "route not found")
		case "ambiguous":
			WriteErrorEnvelope(w, r, http.StatusConflict, &ErrorEnvelope{
				Code:       ErrorCodeAmbiguous,
				Message:    "route selector matched multiple endpoints or handlers",
				Capability: routeToCallerCapability,
			})
		default:
			h.writeRouteToCallerNotFound(w, r, "route not found")
		}
		return
	}

	if route.HandlerID == "" {
		WriteSuccess(w, r, http.StatusOK, map[string]any{
			"status":       "unsupported",
			"partial":      true,
			"truncated":    false,
			"unsupported":  []string{"no exact HANDLES_ROUTE edge exists for this endpoint selector"},
			"route":        route.routeMap(),
			"handler":      nil,
			"callers":      []map[string]any{},
			"callees":      []map[string]any{},
			"impact":       emptyRouteToCallerImpact(),
			"max_depth":    req.MaxDepth,
			"limit":        req.Limit,
			"truth_source": "HANDLES_ROUTE",
		}, BuildTruthEnvelope(h.profile(), routeToCallerCapability, TruthBasisAuthoritativeGraph, "resolved exact route endpoint; no HANDLES_ROUTE handler edge was present"))
		return
	}

	relationshipRows, err := h.routeToCallerRelationshipRows(r, route.HandlerID, req)
	if err != nil {
		if WriteGraphReadError(w, r, err, routeToCallerCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	callers, callees, truncated := splitRouteToCallerRelationships(relationshipRows, req.Limit)
	impact, err := h.routeToCallerImpact(r, route, req.Limit)
	if err != nil {
		if WriteGraphReadError(w, r, err, routeToCallerCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	responseStatus := "complete"
	if truncated {
		responseStatus = "partial"
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"status":       responseStatus,
		"partial":      truncated,
		"truncated":    truncated,
		"unsupported":  []string{},
		"route":        route.routeMap(),
		"handler":      route.handlerMap(),
		"callers":      callers,
		"callees":      callees,
		"impact":       impact,
		"max_depth":    req.MaxDepth,
		"limit":        req.Limit,
		"truth_source": "HANDLES_ROUTE",
	}, BuildTruthEnvelope(h.profile(), routeToCallerCapability, TruthBasisAuthoritativeGraph, "resolved from exact HANDLES_ROUTE edge and bounded CALLS traversal"))
}

func (r *routeToCallerRequest) normalize() {
	r.RepoID = strings.TrimSpace(r.RepoID)
	r.ServiceID = strings.TrimSpace(r.ServiceID)
	r.ServiceName = strings.TrimSpace(r.ServiceName)
	r.Method = strings.ToUpper(strings.TrimSpace(r.Method))
	r.Path = strings.TrimSpace(r.Path)
	if r.MaxDepth <= 0 {
		r.MaxDepth = 2
	}
	if r.MaxDepth > 5 {
		r.MaxDepth = 5
	}
	if r.Limit <= 0 {
		r.Limit = 25
	}
	if r.Limit > 100 {
		r.Limit = 100
	}
}

func (r routeToCallerRequest) validate() error {
	if r.Path == "" {
		return fmt.Errorf("path is required")
	}
	if r.RepoID == "" && r.ServiceID == "" && r.ServiceName == "" {
		return fmt.Errorf("repo_id, service_id, or service_name is required")
	}
	return nil
}

func routeToCallerAllowedByScope(r *http.Request, req routeToCallerRequest) bool {
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		return false
	}
	return req.RepoID == "" || access.allowsRepositoryID(req.RepoID)
}

func (h *CodeHandler) writeRouteToCallerNotFound(w http.ResponseWriter, r *http.Request, message string) {
	WriteErrorEnvelope(w, r, http.StatusNotFound, &ErrorEnvelope{
		Code:       ErrorCodeNotFound,
		Message:    message,
		Capability: routeToCallerCapability,
	})
}

func selectRouteToCallerRoute(rows []map[string]any) (routeToCallerRoute, string, bool) {
	if len(rows) == 0 {
		return routeToCallerRoute{}, "not_found", false
	}
	var selected routeToCallerRoute
	endpoints := map[string]struct{}{}
	handlers := map[string]struct{}{}
	for _, row := range rows {
		route := routeToCallerRouteFromRow(row)
		endpointKey := route.EndpointID
		if endpointKey == "" {
			endpointKey = route.RepoID + "\x00" + route.Path
		}
		endpoints[endpointKey] = struct{}{}
		if route.HandlerID != "" {
			handlers[route.HandlerID] = struct{}{}
			selected = route
		} else if selected.EndpointID == "" {
			selected = route
		}
	}
	if len(endpoints) > 1 || len(handlers) > 1 {
		return routeToCallerRoute{}, "ambiguous", false
	}
	return selected, "ok", true
}

func routeToCallerRouteFromRow(row map[string]any) routeToCallerRoute {
	return routeToCallerRoute{
		EndpointID:  StringVal(row, "endpoint_id"),
		Path:        StringVal(row, "path"),
		RepoID:      StringVal(row, "repo_id"),
		Method:      StringVal(row, "http_method"),
		Framework:   StringVal(row, "framework"),
		HandlerID:   StringVal(row, "handler_id"),
		HandlerName: StringVal(row, "handler_name"),
		FilePath:    StringVal(row, "handler_file_path"),
		Language:    StringVal(row, "handler_language"),
		StartLine:   IntVal(row, "handler_start_line"),
		EndLine:     IntVal(row, "handler_end_line"),
	}
}

func (r routeToCallerRoute) routeMap() map[string]any {
	return map[string]any{
		"endpoint_id": r.EndpointID,
		"repo_id":     r.RepoID,
		"method":      r.Method,
		"path":        r.Path,
		"framework":   r.Framework,
	}
}

func (r routeToCallerRoute) handlerMap() map[string]any {
	return map[string]any{
		"entity_id":   r.HandlerID,
		"name":        r.HandlerName,
		"file_path":   r.FilePath,
		"language":    r.Language,
		"repo_id":     r.RepoID,
		"start_line":  r.StartLine,
		"end_line":    r.EndLine,
		"truth_edge":  "HANDLES_ROUTE",
		"truth_level": "exact",
	}
}
