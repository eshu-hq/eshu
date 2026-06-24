// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func freshnessTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "get_generation_lifecycle",
			Description: "Inspect bounded scope generation lifecycle history (active, pending, superseded, completed, failed) for a scope, repository, collector, source system, generation, or status. Each row carries the current active generation, trigger kind, freshness hint, observed/activated/superseded timestamps, the per-generation queue status, and the latest failure when present. Unknown scope/repository/generation selectors return an explicit not-found, never a confident empty list.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Optional exact ingestion scope id, for example git-repository-scope:owner/repo.",
					},
					"repository": map[string]any{
						"type":        "string",
						"description": "Optional canonical repository id (matches repository-kind scopes by source_key).",
					},
					"collector_kind": map[string]any{
						"type":        "string",
						"description": "Optional collector kind filter, for example git, aws, or terraform_state.",
					},
					"source_system": map[string]any{
						"type":        "string",
						"description": "Optional source system filter, for example github.",
					},
					"generation_id": map[string]any{
						"type":        "string",
						"description": "Optional exact generation id to drill into a single lifecycle row.",
					},
					"status": map[string]any{
						"type":        "string",
						"description": "Optional generation status filter.",
						"enum":        []string{"pending", "active", "superseded", "completed", "failed"},
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum generation lifecycle rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     500,
					},
				},
			},
		},
		{
			Name:        "get_changed_since",
			Description: "Summarize what changed in a repository scope since a prior generation or instant. Diffs the prior generation's fact set against the current active generation's fact set, keyed by stable fact key, into per-category counts (files, content entities, facts) for added, updated, unchanged, retired, and superseded keys plus bounded sample handles. Supply since_generation_id for an exact prior generation or since_observed_at (RFC3339) for the generation observed at or before that instant. Unknown scope/repository returns not-found; a scope with no current active generation returns an explicit unavailable diff rather than zero deltas. Retired and superseded are never collapsed into unchanged.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Exact ingestion scope id (required unless repository is set), for example git-repository-scope:owner/repo.",
					},
					"repository": map[string]any{
						"type":        "string",
						"description": "Canonical repository id (matches repository-kind scopes by source_key; required unless scope_id is set).",
					},
					"since_generation_id": map[string]any{
						"type":        "string",
						"description": "Prior generation id to diff from (required unless since_observed_at is set).",
					},
					"since_observed_at": map[string]any{
						"type":        "string",
						"description": "RFC3339 instant; the diff baseline is the generation observed at or before this time (required unless since_generation_id is set).",
					},
					"sample_limit": map[string]any{
						"type":        "integer",
						"description": "Maximum sample handles returned per classification per category.",
						"default":     25,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
		{
			Name:        "get_service_changed_since",
			Description: "Summarize what changed for a service since a prior service materialization generation. Diffs the prior service generation's evidence snapshot set against the current active generation's set, keyed by a generation-independent service_evidence_key, into per-family counts (ownership, deployment, runtime, dependencies, docs, incidents, vulnerabilities) for added, updated, unchanged, retired, and superseded keys plus bounded sample handles. Supply service_id and since_generation_id. An unknown service_id returns service_not_found; a since reference that matches no service generation returns not_found; a service with no current active generation returns an explicit unavailable diff rather than zero deltas. Retired and superseded are never collapsed into unchanged.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service_id": map[string]any{
						"type":        "string",
						"description": "Exact service id whose evidence lineage to diff.",
					},
					"since_generation_id": map[string]any{
						"type":        "string",
						"description": "Prior service materialization generation id to diff from.",
					},
					"sample_limit": map[string]any{
						"type":        "integer",
						"description": "Maximum sample handles returned per classification per family.",
						"default":     25,
						"minimum":     1,
						"maximum":     200,
					},
				},
				"required": []string{"service_id", "since_generation_id"},
			},
		},
	}
}
