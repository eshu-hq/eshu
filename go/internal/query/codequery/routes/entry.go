// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package routes

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Capability is the contract capability for route-to-caller tracing.
const Capability = "call_graph.route_to_caller"

// Request describes one route-to-caller lookup: an endpoint path with an
// optional HTTP method, bounded to one repository, service, or service
// name, with the traversal depth and row limits. It decodes from the
// route's JSON body unchanged from its codequery home; codequery keeps a
// type alias so every existing spelling still resolves.
type Request struct {
	RepoID      string `json:"repo_id"`
	ServiceID   string `json:"service_id"`
	ServiceName string `json:"service_name"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	MaxDepth    int    `json:"max_depth"`
	Limit       int    `json:"limit"`
}

// Normalize trims the selectors, upper-cases the method, and floors and
// caps the depth and row limits to the handler's bounds.
func (r *Request) Normalize() {
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

// Validate rejects requests with no path or no repository/service
// selector.
func (r Request) Validate() error {
	if r.Path == "" {
		return fmt.Errorf("path is required")
	}
	if r.RepoID == "" && r.ServiceID == "" && r.ServiceName == "" {
		return fmt.Errorf("repo_id, service_id, or service_name is required")
	}
	return nil
}

// AllowedByScope reports whether the request's repository selector falls
// inside the caller's grant. A grantless caller sees nothing; an empty
// request selector is allowed through so the read itself resolves to no
// rows rather than leaking which endpoints exist.
func AllowedByScope(access querycontract.RepositoryAccessFilter, req Request) bool {
	if access.Empty() {
		return false
	}
	return req.RepoID == "" || access.AllowsRepositoryID(req.RepoID)
}

// Route is one resolved endpoint with its HANDLES_ROUTE handler, shaped
// from a joined endpoint/handler row.
type Route struct {
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

// RouteFromRow shapes one joined endpoint/handler row into a Route.
func RouteFromRow(row map[string]any) Route {
	return Route{
		EndpointID:  querycontract.StringVal(row, "endpoint_id"),
		Path:        querycontract.StringVal(row, "path"),
		RepoID:      querycontract.StringVal(row, "repo_id"),
		Method:      querycontract.StringVal(row, "http_method"),
		Framework:   querycontract.StringVal(row, "framework"),
		HandlerID:   querycontract.StringVal(row, "handler_id"),
		HandlerName: querycontract.StringVal(row, "handler_name"),
		FilePath:    querycontract.StringVal(row, "handler_file_path"),
		Language:    querycontract.StringVal(row, "handler_language"),
		StartLine:   querycontract.IntVal(row, "handler_start_line"),
		EndLine:     querycontract.IntVal(row, "handler_end_line"),
	}
}

// SelectRoute picks the single route the joined rows describe: the row
// carrying a handler wins, else the first endpoint row. More than one
// distinct endpoint or handler is ambiguous; no rows is not found.
func SelectRoute(rows []map[string]any) (Route, string, bool) {
	if len(rows) == 0 {
		return Route{}, "not_found", false
	}
	var selected Route
	endpoints := map[string]struct{}{}
	handlers := map[string]struct{}{}
	for _, row := range rows {
		route := RouteFromRow(row)
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
		return Route{}, "ambiguous", false
	}
	return selected, "ok", true
}

// RouteMap renders the endpoint half of the response route object.
func (r Route) RouteMap() map[string]any {
	return map[string]any{
		"endpoint_id": r.EndpointID,
		"repo_id":     r.RepoID,
		"method":      r.Method,
		"path":        r.Path,
		"framework":   r.Framework,
	}
}

// HandlerMap renders the handler half of the response route object.
func (r Route) HandlerMap() map[string]any {
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
