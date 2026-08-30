// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdtools

import (
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// Route selects the internal HTTP request for a CI/CD run-correlation tool
// without executing it. It reports handled only for tools owned by this package.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "list_ci_cd_run_correlations":
		return runCorrelationsRequest(args), true
	case "count_ci_cd_run_correlations":
		return aggregateCountRequest(args), true
	case "get_ci_cd_run_correlation_inventory":
		return aggregateInventoryRequest(args), true
	default:
		return routecontract.Request{}, false
	}
}

func runCorrelationsRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/ci-cd/run-correlations", Query: map[string]string{
		"after_correlation_id": args.String("after_correlation_id"),
		"artifact_digest":      args.String("artifact_digest"),
		"commit_sha":           args.String("commit_sha"),
		"environment":          args.String("environment"),
		"image_ref":            args.String("image_ref"),
		"limit":                strconv.Itoa(args.IntOr("limit", 50)),
		"outcome":              args.String("outcome"),
		"provider":             args.String("provider"),
		"provider_run_id":      providerRunID(args),
		"repository_id":        args.String("repository_id"),
		"run_id":               args.String("run_id"),
		"scope_id":             args.String("scope_id"),
	}}
}

// providerRunID resolves the provider-scoped run identifier the listing route
// filters on. Callers may send either the canonical provider_run_id or the
// older run_id spelling, so an absent, empty, or wrong-typed provider_run_id
// falls back to run_id rather than filtering on an empty value.
func providerRunID(args routecontract.Arguments) string {
	if value := args.String("provider_run_id"); value != "" {
		return value
	}
	return args.String("run_id")
}

func aggregateCountRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/ci-cd/run-correlations/count", Query: map[string]string{
		"scope_id":        args.String("scope_id"),
		"repository_id":   args.String("repository_id"),
		"commit_sha":      args.String("commit_sha"),
		"provider":        args.String("provider"),
		"artifact_digest": args.String("artifact_digest"),
		"image_ref":       args.String("image_ref"),
		"environment":     args.String("environment"),
		"outcome":         args.String("outcome"),
	}}
}

func aggregateInventoryRequest(args routecontract.Arguments) routecontract.Request {
	groupBy := args.String("group_by")
	if groupBy == "" {
		groupBy = "outcome"
	}
	return routecontract.Request{Method: "GET", Path: "/api/v0/ci-cd/run-correlations/inventory", Query: map[string]string{
		"group_by":        groupBy,
		"scope_id":        args.String("scope_id"),
		"repository_id":   args.String("repository_id"),
		"commit_sha":      args.String("commit_sha"),
		"provider":        args.String("provider"),
		"artifact_digest": args.String("artifact_digest"),
		"image_ref":       args.String("image_ref"),
		"environment":     args.String("environment"),
		"outcome":         args.String("outcome"),
		"limit":           strconv.Itoa(args.IntOr("limit", 100)),
		"offset":          strconv.Itoa(args.IntOr("offset", 0)),
	}}
}
