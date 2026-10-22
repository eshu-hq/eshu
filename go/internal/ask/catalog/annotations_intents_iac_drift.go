// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// iacDriftToolIntents curates router intents for the runtime drift, IaC
// management, replatforming, cloud inventory, correlation, and evidence-packet
// tools. Families are named in annotations_intents_families.go.
func iacDriftToolIntents() map[string]toolIntent {
	return map[string]toolIntent{
		// runtime-drift
		"list_cloud_runtime_drift_findings": canonicalIntents("cloud runtime drift", "drift between cloud and terraform", "aws gcp azure drift findings", "resources that drifted from code", "multi cloud drift list"),
		"list_aws_runtime_drift_findings":   intents("aws runtime drift", "aws drift reducer findings", "aws drift rejected promotions", "aws account drift"),
		"export_cloud_runtime_drift_packet": intents("export drift evidence packet", "runtime drift investigation packet", "shareable drift report", "drift packet artifact"),
		// terraform config vs state drift (not in a family)
		"list_terraform_config_state_drift_findings": intents("terraform config vs state drift", "state snapshot drift", "terraform address drift", "config state mismatch"),
		// iac-management
		"find_unmanaged_resources":       canonicalIntents("unmanaged aws resources", "resources not in terraform", "click ops resources", "cloud resources without iac owner", "image version drift"),
		"find_unmanaged_resource_owners": intents("who owns unmanaged resource", "owner candidates for drifted resource", "ownership packet for unmanaged resource", "which repo should manage resource"),
		"get_iac_management_status":      intents("is this resource managed by terraform", "iac management status", "safety gate for resource", "management state of arn"),
		"explain_iac_management_status":  intents("terraform ownership proof for one resource", "why is resource unmanaged", "evidence for management state", "terraform vs cloud evidence for arn"),
		// replatforming
		"compose_replatforming_plan":    canonicalIntents("replatforming plan", "migration plan to terraform", "ordered migration waves", "plan to bring service under iac", "import ready and blocked items"),
		"get_replatforming_rollups":     intents("replatforming rollups", "readiness by account and environment", "import ready vs needs review counts", "migration progress summary"),
		"propose_terraform_import_plan": intents("terraform import plan", "generate terraform import blocks", "import candidates for unmanaged resources", "terraform import commands"),
		// cloud inventory (not in a family)
		"list_cloud_resource_inventory": intents("cloud resource inventory", "multi cloud resource identities", "resources by management origin", "list cloud resources by provider"),
		// correlations and ownership (not in a family)
		"list_service_catalog_correlations":        intents("service catalog ownership", "service owner from catalog", "catalog drift for service", "backstage descriptor correlation"),
		"list_codeowners_ownership":                intents("codeowners rules", "who owns this path", "effective owner of repository", "codeowners declarations"),
		"list_kubernetes_correlations":             intents("kubernetes workload ownership", "workloads in namespace", "which cluster runs image", "kubernetes drift correlation"),
		"list_observability_coverage_correlations": intents("alarm and dashboard coverage", "is this service monitored", "missing observability coverage", "log and trace coverage gaps"),
		"list_admission_decisions":                 intents("why was candidate rejected", "correlation admission decisions", "ambiguous or stale correlations", "admission state for anchor"),
		// relationship evidence (not in a family)
		"get_relationship_evidence": intents("relationship evidence by resolved id", "evidence behind relationship", "confidence basis for edge", "dereference evidence pointer"),
		"list_relationship_edges":   intents("list relationship edges by verb", "typed graph edges", "edges stamped by tool", "browse relationship edges"),
		// evidence packets (not in a family)
		"export_deployable_unit_packet":  intents("deployable unit evidence packet", "export deployable unit admission truth", "admission decisions packet", "deployable unit report"),
		"build_evidence_citation_packet": intents("citation packet from handles", "hydrate file handles into citations", "cite sources for answer", "evidence citations"),
		"derive_visualization_packet":    intents("chart service evidence visually", "derive diagram from answer", "visualization from evidence", "render answer as graph"),
		// incident (not in a family)
		"get_incident_context":    intents("incident context", "who is on call for service", "pagerduty routing for service", "what changed before incident", "incident evidence packet"),
		"list_work_item_evidence": intents("jira work item evidence", "tickets for service", "work items by project", "issue evidence by key"),
	}
}
