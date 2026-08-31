// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package observabilitycoveragetools

import (
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// Route selects the internal HTTP request for an observability-coverage tool
// without executing it. It reports handled only for tools owned by this package.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "list_observability_coverage_correlations":
		return correlationsRequest(args), true
	default:
		return routecontract.Request{}, false
	}
}

// correlationsRequest maps list_observability_coverage_correlations to the
// bounded read-only route GET /api/v0/observability/coverage/correlations,
// which query.ObservabilityCoverageHandler serves; the handler owns the
// 1-200 limit bound and the anchor requirement.
//
// Twelve keys travel together here, more than any other route the repository
// router answers. Each is a filter the handler reads by name -- there is no
// catch-all. A dropped key fails two different ways: limit and the scope anchor
// are required, so losing either 400s, while losing a plain filter silently
// widens the caller's page to rows they filtered out.
func correlationsRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/observability/coverage/correlations", Query: map[string]string{
		"after_correlation_id":     args.String("after_correlation_id"),
		"coverage_signal":          args.String("coverage_signal"),
		"coverage_status":          args.String("coverage_status"),
		"limit":                    strconv.Itoa(args.IntOr("limit", 50)),
		"observability_object_ref": args.String("observability_object_ref"),
		"outcome":                  args.String("outcome"),
		"provider":                 args.String("provider"),
		"resource_class":           args.String("resource_class"),
		"scope_id":                 args.String("scope_id"),
		"source_class":             args.String("source_class"),
		"target_service_ref":       args.String("target_service_ref"),
		"target_uid":               args.String("target_uid"),
	}}
}
