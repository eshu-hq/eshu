// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttools

import "github.com/eshu-hq/eshu/go/internal/mcp/routecontract"

// Route selects the internal HTTP request for an impact-analysis tool without
// executing it. It reports handled only for the nine tools this package owns.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "trace_deployment_chain":
		return traceDeploymentChainRequest(args), true
	case "investigate_deployment_config":
		return deploymentConfigInfluenceRequest(args), true
	case "find_blast_radius":
		return blastRadiusRequest(args), true
	case "find_change_surface":
		return changeSurfaceRequest(args), true
	case "investigate_contract_impact":
		return contractImpactRequest(args), true
	case "investigate_change_surface":
		return changeSurfaceInvestigationRequest(args), true
	case "trace_resource_to_code":
		return resourceToCodeRequest(args), true
	case "explain_dependency_path":
		return dependencyPathRequest(args), true
	case "trace_exposure_path":
		return exposurePathRequest(args), true
	default:
		return routecontract.Request{}, false
	}
}

// traceDeploymentChainRequest maps trace_deployment_chain to
// POST /api/v0/impact/trace-deployment-chain.
//
// The trace-deployment-chain schema in the parent's ecosystem child documents
// that an omitted max_depth resolves to the handler's own operator-safe
// default (boundedTraceEnrichmentLimit(0) = 25), not to 8. Forwarding 0 for
// an omitted argument mirrors an HTTP caller who leaves max_depth out of the
// JSON body entirely (the Go zero value); forwarding 8 previously widened the
// resolved search limit to boundedTraceEnrichmentLimit(8) = 80 for a caller
// that changed nothing. The handler normalizes rather than rejects
// (normalizeTraceDeploymentChainMaxDepth clamps into [0, 1000]), so no value
// selected here can turn into a 400.
func traceDeploymentChainRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "POST", Path: "/api/v0/impact/trace-deployment-chain", Body: map[string]any{
		"service_name":                 args.String("service_name"),
		"direct_only":                  args.BoolOr("direct_only", true),
		"max_depth":                    args.IntOr("max_depth", 0),
		"include_related_module_usage": args.BoolOr("include_related_module_usage", false),
	}}
}

// deploymentConfigInfluenceRequest maps investigate_deployment_config to
// POST /api/v0/impact/deployment-config-influence with a dispatcher-side
// limit default of 25.
func deploymentConfigInfluenceRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "POST", Path: "/api/v0/impact/deployment-config-influence", Body: map[string]any{
		"service_name": args.String("service_name"),
		"workload_id":  args.String("workload_id"),
		"environment":  args.String("environment"),
		"limit":        args.IntOr("limit", 25),
	}}
}

// blastRadiusRequest maps find_blast_radius to
// POST /api/v0/impact/blast-radius with a dispatcher-side limit default
// of 50.
func blastRadiusRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "POST", Path: "/api/v0/impact/blast-radius", Body: map[string]any{
		"target":      args.String("target"),
		"target_type": args.String("target_type"),
		"limit":       args.IntOr("limit", 50),
	}}
}

// changeSurfaceRequest maps find_change_surface to
// POST /api/v0/impact/change-surface with a dispatcher-side limit default
// of 50.
func changeSurfaceRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "POST", Path: "/api/v0/impact/change-surface", Body: map[string]any{
		"target":      args.String("target"),
		"environment": args.String("environment"),
		"limit":       args.IntOr("limit", 50),
	}}
}

// contractImpactRequest maps investigate_contract_impact to
// POST /api/v0/impact/contracts, carrying eight string selectors and a
// dispatcher-side limit default of 25.
func contractImpactRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "POST", Path: "/api/v0/impact/contracts", Body: map[string]any{
		"family":           args.String("family"),
		"provider_repo_id": args.String("provider_repo_id"),
		"consumer_repo_id": args.String("consumer_repo_id"),
		"repo_id":          args.String("repo_id"),
		"route":            args.String("route"),
		"topic":            args.String("topic"),
		"service_name":     args.String("service_name"),
		"method":           args.String("method"),
		"limit":            args.IntOr("limit", 25),
	}}
}

// changeSurfaceInvestigationRequest maps investigate_change_surface to
// POST /api/v0/impact/change-surface/investigate: nine string selectors, the
// changed_paths string array, and dispatcher-side defaults of max_depth 4,
// limit 25, and offset 0. changed_paths follows
// routecontract.Arguments.StringSlice, so an absent or wrong-typed value
// travels as JSON null rather than an empty array.
func changeSurfaceInvestigationRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "POST", Path: "/api/v0/impact/change-surface/investigate", Body: map[string]any{
		"target":        args.String("target"),
		"target_type":   args.String("target_type"),
		"service_name":  args.String("service_name"),
		"workload_id":   args.String("workload_id"),
		"resource_id":   args.String("resource_id"),
		"module_id":     args.String("module_id"),
		"topic":         args.String("topic"),
		"repo_id":       args.String("repo_id"),
		"changed_paths": args.StringSlice("changed_paths"),
		"environment":   args.String("environment"),
		"max_depth":     args.IntOr("max_depth", 4),
		"limit":         args.IntOr("limit", 25),
		"offset":        args.IntOr("offset", 0),
	}}
}

// resourceToCodeRequest maps trace_resource_to_code to
// POST /api/v0/impact/trace-resource-to-code with dispatcher-side defaults of
// max_depth 8 and limit 50.
func resourceToCodeRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "POST", Path: "/api/v0/impact/trace-resource-to-code", Body: map[string]any{
		"start":       args.String("start"),
		"environment": args.String("environment"),
		"max_depth":   args.IntOr("max_depth", 8),
		"limit":       args.IntOr("limit", 50),
	}}
}

// dependencyPathRequest maps explain_dependency_path to
// POST /api/v0/impact/explain-dependency-path. Uniquely in this family, the
// decoded argument map itself is the body: no key is selected, defaulted, or
// coerced here, so the handler sees exactly what the caller sent and the
// returned body aliases the caller's map rather than copying it. This
// pass-through predates the extraction and is pinned by the tests.
func dependencyPathRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "POST", Path: "/api/v0/impact/explain-dependency-path", Body: map[string]any(args)}
}

// exposurePathRequest maps trace_exposure_path to
// POST /api/v0/impact/trace-exposure-path with a dispatcher-side max_depth
// default of 5 and no limit key.
func exposurePathRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "POST", Path: "/api/v0/impact/trace-exposure-path", Body: map[string]any{
		"source":           args.String("source"),
		"source_entity_id": args.String("source_entity_id"),
		"repo_id":          args.String("repo_id"),
		"max_depth":        args.IntOr("max_depth", 5),
	}}
}
