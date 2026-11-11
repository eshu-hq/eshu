// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicetools //nolint:dirgate // Root owns service tool registration aggregation (Catalog/Context/Intelligence); route selection lives in the context child.

import "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"

// ContextTools returns the service context, story, endpoint list, and
// investigation registrations in their canonical local order.
func ContextTools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "get_service_context",
			Description: "Alias for workload context that accepts service workload selectors through workload_id.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Service workload identifier, or a service name passed through the workload_id field",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
					"downstream_read_limit": map[string]any{
						"type":        "integer",
						"description": "Indirect-evidence row bound for the dependents and consumer_repositories reads. Defaults to 25 when absent; clamps into (0, 100], and the effective bound is echoed on result_limits.downstream_read_limit and the investigation coverage. Raise it (at most 100) to recover a truncated fan-out in one re-call instead of paging.",
					},
					"evidence_detail": map[string]any{
						"type":        "string",
						"enum":        []string{"full", "handles"},
						"description": "Row detail for deployment evidence artifacts and API surface endpoints. handles (the default for this tool) returns identity rows only (id, relationship_type, resolved_id for artifacts; id, path, methods for endpoints), drops the evidence_index and the content-derived evidence lists (deployment_artifacts, delivery_paths, topology_story and similar), keeps every count, and names each reduced family with its total in truth.omissions; repeat the call with full for the rows. full returns every row in full and can exceed the response budget on a populated service.",
					},
				},
				"required": []string{"workload_id"},
			},
		},
		{
			Name:        "get_service_story",
			Description: "Get the one-call service dossier for a service: identity, API surface, deployment lanes, dependencies, consumers, and evidence graph. Select the service with one of service_id, workload_id, or service_name; the first one given wins in that order. repo separates only services that live in different repositories, so two same-named services in one repository stay ambiguous. When the answer is ambiguous, call again with service_id set to a candidate's next_selector value; a candidate with legacy_id true is a leftover, not the live workload; choose a scoped candidate.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service_id": map[string]any{
						"type":        "string",
						"description": "Exact service identifier: a full scoped id (workload:repository:<repo_id>:<name>) or a workload:<name> handle, as listed in the candidates of an ambiguous answer. Selects one service on its own and wins over workload_id and service_name",
					},
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Service workload identifier, or a service name passed through the workload_id field",
					},
					"service_name": map[string]any{
						"type":        "string",
						"description": "Optional service name selector when the caller starts from repository-scoped service story context",
					},
					"repo": map[string]any{
						"type":        "string",
						"description": "Optional repository selector used with service_name. It separates only services in different repositories, not two services in the same repository; use service_id for those",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional repository selector alias used with service_name. It separates only services in different repositories; use service_id for the rest",
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional repository selector alias used with service_name. It separates only services in different repositories; use service_id for the rest",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
					"downstream_read_limit": map[string]any{
						"type":        "integer",
						"description": "Indirect-evidence row bound for the dependents and consumer_repositories reads. Defaults to 25 when absent; clamps into (0, 100], and the effective bound is echoed on result_limits.downstream_read_limit and the investigation coverage. Raise it (at most 100) to recover a truncated fan-out in one re-call instead of paging.",
					},
					"evidence_detail": map[string]any{
						"type":        "string",
						"enum":        []string{"full", "handles"},
						"description": "Row detail for the story's evidence lists. handles (the default for this tool) keeps every section, count, and top-level status, truth, and freshness field, and projects the bulky lists to identity rows that keep their row-level truth labels (including confidence, relationship_types, consumer_kinds, evidence_kinds, relationship_basis, resolution_mode, candidate_status, missing_relationship, service_anchor_status, and service_anchor_reason): API endpoints, code_to_runtime_trace segment evidence (a preview of 10 where the rows also ship in another section), upstream_dependencies, hostnames, entrypoints, entrypoint_candidates, network_paths, dependents, consumer_repositories, provisioning_source_chains, cloud_resources, uncorrelated_cloud_resources, evidence_graph edges and touched nodes, and investigation repositories. Exception: deployment artifact rows keep id, relationship_type, and resolved_id; get_relationship_evidence by resolved_id returns the rest, including confidence. It ships api_surface and the dependents and consumer_repositories rows once, drops the content-derived deployment evidence lists, and names each reduced family with its total in truth.omissions. Each total is the story's own pre-cap count and may be a lower bound, because the story carries a flag for only some of the bounds its reads apply; treat a total as exact only for the families the Context evidence budget reference lists as exact. Repeat the call with full for every field of every row; full can exceed the response budget on a populated service.",
					},
				},
			},
		},
		listServiceAPIEndpointsTool(),
		listServiceDownstreamConsumersTool(),
		{
			Name:        "investigate_service",
			Description: "Plan a service investigation across related repositories, deployment sources, indexed docs, and evidence drilldowns. Select the service with service_id or service_name; service_id wins. repo separates only services that live in different repositories. When the answer is ambiguous, call again with service_id set to a candidate's next_selector value; a candidate with legacy_id true is a leftover, not the live workload; choose a scoped candidate.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service_id": map[string]any{
						"type":        "string",
						"description": "Exact service identifier: a full scoped id (workload:repository:<repo_id>:<name>) or a workload:<name> handle, as listed in the candidates of an ambiguous answer. Selects one service on its own and wins over service_name",
					},
					"service_name": map[string]any{
						"type":        "string",
						"description": "Service name or canonical workload identifier to investigate. Optional when service_id is given",
					},
					"repo": map[string]any{
						"type":        "string",
						"description": "Optional repository selector used with service_name. It separates only services in different repositories; use service_id for the rest",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional repository selector alias used with service_name. It separates only services in different repositories; use service_id for the rest",
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Optional repository selector alias used with service_name. It separates only services in different repositories; use service_id for the rest",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Optional environment context",
					},
					"intent": map[string]any{
						"type":        "string",
						"description": "Optional investigation intent such as runbook, onboarding, or incident",
					},
					"question": map[string]any{
						"type":        "string",
						"description": "Optional user question to preserve in the investigation packet",
					},
				},
			},
		},
	}
}

// listServiceAPIEndpointsTool builds the dedicated API-surface endpoint list
// registration (#10477). It lives in its own constructor because the
// ContextTools literal would otherwise exceed the funlen budget; the
// registration order and serialized shape are unchanged.
func listServiceAPIEndpointsTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "list_service_api_endpoints",
		Description: "Page a service's API surface endpoints in path, endpoint_id order. Select the service with one of service_id, workload_id, or service_name; the first one given wins in that order. repo separates only services that live in different repositories. Returns the endpoints page with endpoint_count (the true total), detail_truncated, and next_offset (absent when the page is complete); repeat with offset set to next_offset until it is absent. The story and context dossiers keep only a 50-row api_surface preview and point here.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"service_id": map[string]any{
					"type":        "string",
					"description": "Exact service identifier: a full scoped id (workload:repository:<repo_id>:<name>) or a workload:<name> handle, as listed in the candidates of an ambiguous answer. Selects one service on its own and wins over workload_id and service_name",
				},
				"workload_id": map[string]any{
					"type":        "string",
					"description": "Service workload identifier, or a service name passed through the workload_id field",
				},
				"service_name": map[string]any{
					"type":        "string",
					"description": "Optional service name selector when the caller starts from repository-scoped service story context",
				},
				"repo": map[string]any{
					"type":        "string",
					"description": "Optional repository selector used with service_name. It separates only services in different repositories; use service_id for the rest",
				},
				"repository_id": map[string]any{
					"type":        "string",
					"description": "Optional repository selector alias used with service_name. It separates only services in different repositories; use service_id for the rest",
				},
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional repository selector alias used with service_name. It separates only services in different repositories; use service_id for the rest",
				},
				"environment": map[string]any{
					"type":        "string",
					"description": "Optional environment context",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Endpoints per page. Defaults to 50 when absent; clamps into (0, 100].",
					"default":     50,
					"minimum":     1,
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Rows to skip, in path, endpoint_id order. Defaults to 0 when absent; repeat the call with offset set to the previous page's next_offset until next_offset is absent.",
					"default":     0,
					"minimum":     0,
				},
			},
		},
	}
}

// listServiceDownstreamConsumersTool defines the dedicated paged downstream
// consumer list (#10535 follow-up to #10477). It lives in its own constructor
// for the same funlen reason as listServiceAPIEndpointsTool; the registration
// order and serialized shape are unchanged.
func listServiceDownstreamConsumersTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name:        "list_service_downstream_consumers",
		Description: "Page a service's downstream consumer repositories in (repository, repo_id) order. Select the service with one of service_id, workload_id, or service_name; the first one given wins in that order. repo separates only services that live in different repositories. Returns the page with truncated, next_cursor (absent when the page is terminal), consumer_truncation_sources naming which read bound fired, searched_hostnames, and effective_read_bound. Repeat with after_repository/after_repo_id set to the previous page's next_cursor until next_cursor is absent. Counts are lower bounds, never totals.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"service_id": map[string]any{
					"type":        "string",
					"description": "Exact service identifier: a full scoped id (workload:repository:<repo_id>:<name>) or a workload:<name> handle, as listed in the candidates of an ambiguous answer. Selects one service on its own and wins over workload_id and service_name",
				},
				"workload_id": map[string]any{
					"type":        "string",
					"description": "Service workload identifier, or a service name passed through the workload_id field",
				},
				"service_name": map[string]any{
					"type":        "string",
					"description": "Optional service name selector when the caller starts from repository-scoped service story context",
				},
				"repo": map[string]any{
					"type":        "string",
					"description": "Optional repository selector used with service_name. It separates only services in different repositories; use service_id for the rest",
				},
				"repository_id": map[string]any{
					"type":        "string",
					"description": "Optional repository selector alias used with service_name. It separates only services in different repositories; use service_id for the rest",
				},
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Optional repository selector alias used with service_name. It separates only services in different repositories; use service_id for the rest",
				},
				"environment": map[string]any{
					"type":        "string",
					"description": "Optional environment context",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Consumers per page. Defaults to 50 when absent; clamps into (0, 100].",
					"default":     50,
					"minimum":     1,
				},
				"after_repository": map[string]any{
					"type":        "string",
					"description": "Cursor: repository of the last row served on the previous page. Must be provided together with after_repo_id; repeat the call with both set from next_cursor until next_cursor is absent.",
				},
				"after_repo_id": map[string]any{
					"type":        "string",
					"description": "Cursor: repo_id of the last row served on the previous page. Must be provided together with after_repository.",
				},
			},
		},
	}
}
