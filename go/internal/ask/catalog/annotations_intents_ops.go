// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// opsToolIntents curates router intents for the runtime status, freshness,
// failed-work, capability, call-plan, semantic, and ask tools. Families are
// named in annotations_intents_families.go.
func opsToolIntents() map[string]toolIntent {
	return map[string]toolIntent{
		// runtime-health
		"get_operator_control_plane": canonicalIntents("is eshu healthy", "operator control plane", "queue depth and stuck work", "reducer backlog", "collector promotion verdicts", "runtime health overview"),
		"get_hosted_readiness":       intents("is the deployment ready", "hosted readiness report", "queue drained and collectors complete", "api and mcp readback ready"),
		"get_index_status":           intents("index status", "is indexing finished", "latest checkpointed index status", "repositories checkpointed in latest index run"),
		"get_collector_readiness":    intents("collector promotion readiness", "which collectors are promoted", "collector family proof status", "collector blockers"),
		// ingester-status
		"list_ingesters":      canonicalIntents("ingester status", "which ingesters are configured", "are ingesters running", "repo sync and parse status"),
		"get_ingester_status": intents("status of one ingester", "ingester by name", "single ingester health"),
		// collectors (not in a family)
		"list_collectors": intents("collector runtime status", "which collectors are running", "which collectors are enabled", "disabled or unregistered collectors"),
		// extraction-readiness
		"list_collector_extraction_readiness": canonicalIntents("collector extraction readiness", "which collectors can move out of tree", "extraction candidates", "collector extraction catalog"),
		"get_collector_extraction_readiness":  intents("extraction readiness for one collector", "extraction checklist for collector family", "extraction blockers for collector"),
		// fact-schema
		"list_fact_schema_versions": canonicalIntents("fact schema versions", "supported fact kinds", "core fact kind registry", "which schema versions are supported"),
		"get_fact_schema_version":   intents("schema version for fact kind", "is collector fact version supported", "classify candidate fact version", "fact kind compatibility"),
		// capability-catalog
		"get_capability_catalog": canonicalIntents("what can eshu do", "capability maturity", "capability owners and gaps", "supported capabilities list", "feature maturity"),
		"get_surface_inventory":  intents("surface inventory", "list all mcp tools and api routes", "readiness of each surface", "implemented vs partial surfaces"),
		// call-plans
		"resolve_query_playbook":         canonicalIntents("call plan for question", "resolve playbook", "which tools answer this question", "ordered call sequence", "query playbook plan"),
		"resolve_investigation_workflow": intents("guided investigation next calls", "what should i check next in this investigation", "recommended next calls for missing evidence", "investigation plan from evidence state"),
		"list_query_playbooks":           intents("list playbooks", "available query playbooks", "playbook catalog", "prompt families"),
		"list_investigation_workflows":   intents("what guided investigations exist", "guided workflows", "workflow catalog", "available investigations"),
		// failed-work
		"list_dead_letter_work_items":       canonicalIntents("dead letter queue", "failed work items", "why is work stuck", "dead lettered reducer domains", "failure class counts"),
		"list_reducer_input_invalid_facts":  intents("invalid facts quarantined by reducer", "facts missing required field", "reducer decode failures", "quarantined facts"),
		"list_changed_since_poisoned_links": intents("poisoned changed since links", "retrying changed since cursor", "changed since counting failures", "stuck changed since scope"),
		// freshness
		"get_freshness_causality":  canonicalIntents("why is this answer stale", "freshness causality", "pending generation or reducer backlog", "stale data cause", "why is data out of date"),
		"get_repository_freshness": intents("did eshu pick up latest commit", "repository commit receipt", "is repo build complete", "repo freshness verdict"),
		"get_generation_lifecycle": intents("generation lifecycle", "active and superseded generations", "failed generations for scope", "generation history"),
		// semantic and governance (not in a family)
		"list_semantic_documentation_observations": intents("llm documentation observations", "semantic doc observations", "opt in semantic extraction results", "documentation observations with truth basis"),
		"list_semantic_code_hints":                 intents("llm code hints", "semantic code hints", "non canonical code hints", "corroborated code hints"),
		"search_semantic_context":                  intents("search curated context", "semantic context search for repo", "curated eshu context", "context search with truth metadata"),
		"get_semantic_capability_status":           intents("semantic extraction status", "is llm extraction enabled", "semantic provider profiles", "semantic queue and budget"),
		"get_answer_narration_status":              intents("answer narration status", "is narration enabled", "narration retention posture", "narration validator reasons"),
		"get_hosted_governance_status":             intents("hosted governance status", "policy mode and tenancy", "egress and retention policy", "audit and redaction posture"),
		"list_component_extensions":                intents("component extensions", "installed extensions", "failed or revoked extensions", "extension inventory"),
		"get_component_extension_diagnostics":      intents("extension diagnostics", "why is extension failing", "extension digest and lifecycle", "component extension policy status"),
		// ask (not in a family)
		"ask": intents("ask a question", "free form question about repositories", "natural language question", "answer with evidence", "ask eshu"),
	}
}
