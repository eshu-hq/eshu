// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package change

// ImpactRoute is the API path `eshu change impact` posts to.
const ImpactRoute = "/api/v0/impact/pre-change"

// PlanRoute is the API path `eshu change plan` posts to.
const PlanRoute = "/api/v0/impact/developer-change-plan"

// ImpactRequestBody builds the JSON body for ImpactRoute.
//
// Every key is present on every request, including the empty ones. The route
// treats an absent key and an empty one the same way, and sending the full set
// keeps the request shape stable enough to diff between two runs when an
// operator is working out why an answer changed.
func ImpactRequestBody(opts Options) map[string]any {
	return map[string]any{
		"repo_id":       opts.RepoID,
		"base_ref":      opts.BaseRef,
		"head_ref":      opts.HeadRef,
		"changed_paths": opts.ChangedPaths,
		"changes":       opts.Changes,
		"target":        opts.Target,
		"target_type":   opts.TargetType,
		"service_name":  opts.ServiceName,
		"workload_id":   opts.WorkloadID,
		"resource_id":   opts.ResourceID,
		"module_id":     opts.ModuleID,
		"topic":         opts.Topic,
		"environment":   opts.Environment,
		"max_depth":     opts.MaxDepth,
		"limit":         opts.Limit,
		"offset":        opts.Offset,
	}
}

// PlanRequestBody builds the JSON body for PlanRoute. It is ImpactRequestBody
// plus developer_intent, the free-text hint the plan route ranks and explains
// its actions against. The impact route has no use for it and does not receive
// it.
func PlanRequestBody(opts Options) map[string]any {
	body := ImpactRequestBody(opts)
	body["developer_intent"] = opts.DeveloperIntent
	return body
}
