// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshnesstools

import "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"

// Tools returns fresh MCP definitions for generation, repository, and service
// freshness reads.
func Tools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "get_generation_lifecycle",
			Description: "Inspect bounded scope generation lifecycle history (active, pending, superseded, completed, failed) for a scope, repository, collector, source system, generation, or status. Each row carries the current active generation, trigger kind, freshness hint, observed/activated/superseded timestamps, the per-generation queue status, and the latest failure when present. Unknown scope/repository/generation selectors return an explicit not-found, never a confident empty list. Scoped tokens receive only granted repositories and scopes; an ungranted selector returns not-found. An all-scope bearer token carries no grant for that filter to bind, so it is refused with a 403 under hosted_multi_tenant and under any unrecognized governance mode; local_no_policy, hosted_single_tenant, and an unset mode (which defaults to local_no_policy) admit it when it is bound to one tenant and workspace, and it then reads the whole corpus, as an admin credential does on every other route there.",
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
			Description: "Summarize what changed in a repository scope since a prior generation or instant. Exactly one mutually exclusive scope selector is required: scope_id or repository. Diffs the prior generation's fact set against the current active generation's fact set, keyed by stable fact key, into per-category counts (files, content entities, facts) for added, updated, unchanged, retired, and superseded keys plus bounded sample handles. Supply since_generation_id for an exact prior generation or since_observed_at (RFC3339) for the generation observed at or before that instant. Unknown scope/repository returns not-found; a scope with no current active generation returns an explicit unavailable diff rather than zero deltas. Retired and superseded are never collapsed into unchanged. Scoped tokens receive only granted repositories and scopes; an ungranted selector returns not-found. An all-scope bearer token carries no grant for that filter to bind, so it is refused with a 403 under hosted_multi_tenant and under any unrecognized governance mode; local_no_policy, hosted_single_tenant, and an unset mode (which defaults to local_no_policy) admit it when it is bound to one tenant and workspace, and it then reads the whole corpus, as an admin credential does on every other route there.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Exact ingestion scope id. Mutually exclusive with repository; exactly one selector is required.",
					},
					"repository": map[string]any{
						"type":        "string",
						"description": "Canonical repository id matched to repository-kind scopes by source_key. Mutually exclusive with scope_id; exactly one selector is required.",
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
			Name:        "get_repository_freshness",
			Description: "Get the per-repository commit receipt and build-completeness verdict for one repository selector (#5143): did eshu pick up its latest commit, and is the evidence for that commit fully built. verdict is one of current, building, behind, unobserved, or unknown. verdict=current speaks to build completeness for the resolved generation, not necessarily a commit receipt: observed_commit may be empty -- legitimate for non-git scopes, pre-delta-baseline generations, and snapshot-trigger git generations (trigger_kind=snapshot, for example a cassette-replayed source with no commit to report) -- represented explicitly rather than fabricated. shared_enrichment reports cross-repo materialization backlog referencing this repository's generation as a separate axis from stages, so a different repository's shared backlog is never attributed here. Optionally supply expected_commit to ask whether a specific commit SHA is reflected; a mismatch always renders behind, whether or not a generation is actively progressing.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Repository selector: canonical ID, name, repo slug, or indexed path",
					},
					"expected_commit": map[string]any{
						"type":        "string",
						"description": "Optional commit SHA the caller expects to be observed. A mismatch renders verdict=behind.",
					},
				},
				"required": []string{"repo_id"},
			},
		},
		{
			Name:        "get_service_changed_since",
			Description: "Summarize what changed for a service since a prior service materialization generation. Diffs the prior service generation's evidence snapshot set against the current active generation's set, keyed by a generation-independent service_evidence_key, into per-family counts (ownership, deployment, runtime, dependencies, docs, incidents, vulnerabilities) for added, updated, unchanged, retired, and superseded keys plus bounded sample handles. Supply service_id and since_generation_id. An unknown service_id returns service_not_found; a since reference that matches no service generation returns not_found; a service with no current active generation returns an explicit unavailable diff rather than zero deltas. Retired and superseded are never collapsed into unchanged. A service id is catalog-relative, so two tenants may both declare it; each ingestion scope that materialized it holds its own lineage (#6475). Pass scope_id to pick one. With no scope_id, the one readable lineage with an active generation is served; more than one admitted lineage with an active generation (or, when none is active, more than one attributed lineage) returns a 409 ambiguous error whose details.scope_ids lists only the scope ids the caller may read; re-ask with one of them. An unattributed legacy lineage is served only to an unscoped caller and only when no attributed lineage has an active generation (unattributed=true). After the service is re-materialized under a scope, a pre-upgrade baseline generation id from an unattributed row no longer resolves and returns not_found. Scoped tokens receive only lineages of granted scopes and repositories; an ungranted service_id or scope_id returns service_not_found, and another lineage's since_generation_id returns the same not_found as an unknown id. An all-scope bearer token carries no grant for that filter to bind, so it is refused with a 403 under hosted_multi_tenant and under any unrecognized governance mode; local_no_policy, hosted_single_tenant, and an unset mode (which defaults to local_no_policy) admit it when it is bound to one tenant and workspace, and it then reads every lineage, as an admin credential does on every other route there.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service_id": map[string]any{
						"type":        "string",
						"description": "Exact service id whose evidence lineage to diff.",
					},
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Ingestion scope whose lineage of service_id to diff. Needed only when the service id has more than one admitted lineage with an active generation (or, when none is active, more than one attributed lineage); the ambiguity error lists them.",
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
