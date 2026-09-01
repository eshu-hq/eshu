// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package infrasearchtools

import "github.com/eshu-hq/eshu/go/internal/mcp/routecontract"

// Route selects the internal HTTP request for an infrastructure-search tool
// without executing it. It reports handled only for tools owned by this
// package.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "find_infra_resources":
		return resourceSearchRequest(args), true
	default:
		return routecontract.Request{}, false
	}
}

// resourceSearchRequest maps find_infra_resources to the bounded read-only
// route POST /api/v0/infra/resources/search, which query.InfraHandler serves;
// the handler owns the scope rule (at least one of query, kind, category,
// provider, environment, resource_service, or resource_category must be
// non-blank after trimming), the category vocabulary (k8s, terraform, argocd,
// crossplane, helm, or cloud), and the limit bound.
//
// The bound is asymmetric and never rejects: the handler substitutes 50 for
// any limit at or below zero and clamps anything above 200 down to 200, so
// the dispatcher's default of 50 is indistinguishable from an omitted or
// nonpositive limit at the handler. A caller who stringifies the number gets
// the default rather than an error, because routecontract.Arguments.IntOr
// does not parse strings.
//
// Eight keys travel together in the JSON body, and they do not fail alike
// when one is lost. Each of the seven scope keys is required only as part of
// the group, so dropping one 400s only the caller whose sole scope it was
// with "query or structured filter is required" and silently widens every
// other caller's page past the filter they named. limit fails nothing:
// dropping it hands every caller the handler's 50-row substitute, so a caller
// who asked for 5 rows or for 200 sees 50 with no error.
func resourceSearchRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "POST", Path: "/api/v0/infra/resources/search", Body: map[string]any{
		"query":             args.String("query"),
		"category":          args.String("category"),
		"kind":              args.String("kind"),
		"provider":          args.String("provider"),
		"environment":       args.String("environment"),
		"resource_service":  args.String("resource_service"),
		"resource_category": args.String("resource_category"),
		"limit":             args.IntOr("limit", 50),
	}}
}
