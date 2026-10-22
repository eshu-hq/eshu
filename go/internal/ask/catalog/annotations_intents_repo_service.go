// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// repoServiceToolIntents curates router intents for the repository, ecosystem,
// language, service, workload, entity, change-impact, deployment, and
// infrastructure tools. Families are named in annotations_intents_families.go.
func repoServiceToolIntents() map[string]toolIntent {
	return map[string]toolIntent{
		// repository-overview
		"get_repo_story":          canonicalIntents("what does this repo do", "repository story", "explain this repository", "repo overview narrative", "summarize repository purpose"),
		"get_repo_summary":        intents("quick repo summary", "repo file count and languages", "repository identity and coverage", "lightweight repo overview"),
		"get_repo_context":        intents("full repository context", "repo entry points and infrastructure", "repo api surface", "repository relationships and deployment", "everything about this repo"),
		"get_repository_stats":    intents("repository statistics", "entity counts for repository", "repo size stats", "how big is this repository"),
		"get_repository_coverage": intents("repository index coverage", "is repo fully indexed", "coverage completeness for repo", "indexing coverage state"),
		// ecosystem-overview
		"get_ecosystem_overview":    canonicalIntents("ecosystem overview", "high level view of all repos", "repos tiers and infrastructure counts", "cross repo relationship summary", "what is indexed"),
		"get_graph_summary_packet":  intents("bounded graph overview packet", "hot entities by call degree", "relationship type counts", "summary of repo graph shape", "summary first graph packet"),
		"list_indexed_repositories": intents("list repositories", "how many repositories are indexed", "indexed repository names", "all indexed repos", "repository count"),
		// repository-language
		"list_repositories_by_language":     canonicalIntents("repositories written in language", "which repos use python", "list go repositories", "repos containing typescript", "repos by language"),
		"count_repositories_by_language":    intents("how many repos use language", "count of go repositories", "number of python repositories", "language repo count"),
		"get_repository_language_inventory": intents("language breakdown", "language buckets across index", "files per language", "language inventory of ecosystem"),
		// service-workload
		"get_service_story":               canonicalIntents("explain this service", "service dossier", "service story", "service api surface and dependencies", "who consumes this service", "onboard to service"),
		"investigate_service":             intents("plan a service investigation", "investigate service across repos", "service docs and deployment sources", "service evidence drilldown"),
		"get_service_intelligence_report": intents("service intelligence report", "code to runtime trace for service", "service supply chain and incidents", "one call service report"),
		"get_service_context":             intents("service context by workload id", "service workload selector context", "context for service name"),
		"get_workload_context":            intents("workload context", "environment specific workload context", "logical workload context", "workload by canonical id"),
		"get_workload_story":              intents("workload story", "explain this workload", "structured workload narrative", "what does workload run"),
		// entity (not in a family)
		"get_entity_context": intents("context for entity id", "entity neighborhood", "details of canonical entity", "relationships of entity"),
		// change-impact
		"investigate_change_surface":  canonicalIntents("what breaks if i change this", "change impact investigation", "impact of changing service or module", "transitive impact surface", "who is affected by this change"),
		"find_blast_radius":           intents("blast radius", "repos affected by change", "resources affected by changing target", "downstream impact of target"),
		"find_change_surface":         intents("change surface of workload", "change surface of terraform module", "what depends on this cloud resource", "workload change surface"),
		"analyze_pre_change_impact":   intents("impact of this diff", "analyze changed files before merge", "pre change impact analysis", "base head diff impact", "what does this pr affect"),
		"plan_developer_change":       intents("plan this code change", "developer change plan", "ordered actions and tests for change", "risk of this change", "what tests to run"),
		"investigate_contract_impact": intents("api contract impact", "who consumes this http endpoint", "cross repo contract consumers", "breaking api change impact"),
		"explain_dependency_path":     intents("chain of dependencies between two things", "how does a depend on b", "dependency path between entities", "why is this a dependency"),
		// changed-since
		"get_changed_since":         canonicalIntents("what changed since", "changes in repository since generation", "diff since last index", "changed facts since instant", "what is new in repo"),
		"get_service_changed_since": intents("what changed for service since", "service changes since generation", "service evidence diff", "service materialization changes"),
		// deployment
		"trace_deployment_chain":        canonicalIntents("how is this deployed", "deployment chain", "gitops application for service", "trace deployment of service", "deployment path to cluster"),
		"investigate_deployment_config": intents("deployment configuration story", "image tag source", "helm values layers", "resource limits for service", "what config influences deployment"),
		"compare_environments":          intents("environment diff for a workload", "difference between staging and prod", "shared and dedicated dependencies across environments", "workload across two environments"),
		// infra-resource-search
		"find_infra_resources":         canonicalIntents("find infrastructure resources", "search terraform resources", "search kubernetes resources", "find cloud resources by name", "search crossplane resources"),
		"count_infra_resources":        intents("how many infrastructure resources", "infrastructure resource totals", "resource counts by provider", "count of cloud resources"),
		"get_infra_resource_inventory": intents("infrastructure inventory grouped by dimension", "resources by environment", "resources by resource category", "grouped infra resource counts"),
		// infra-resource-trace
		"investigate_resource":        canonicalIntents("what uses this database", "what uses this queue", "who uses this bucket", "investigate cloud resource", "resource users and provisioning repo"),
		"analyze_infra_relationships": intents("what deploys what", "what provisions what", "what image does workload run", "workloads running an image", "lambda function image"),
		"trace_resource_to_code":      intents("which code owns this resource", "trace resource to repository", "infrastructure resource to source code", "terraform resource owner repo"),
	}
}
