// Package coordinator runs the workflow coordinator's reconcile, expired-claim
// reap, and workflow-run reconciliation loops.
//
// Service reconciles declarative collector instances against the durable store
// on every reconcile interval. In active mode it also plans supported
// collector work, drains expired claims on the reap interval, and advances
// workflow run progress. Config is loaded from workflow-coordinator
// environment variables; deployment mode is "dark" or "active" and active mode
// requires claims enabled with at least one claim-capable collector instance.
//
// TerraformStateWorkPlanner plans Terraform-state collection runs from resolved
// discovery candidates. OCIRegistryWorkPlanner, PackageRegistryWorkPlanner, and
// VulnerabilityIntelligenceWorkPlanner each plan bounded work items without
// opening provider connections; package and vulnerability planners preserve
// direct and owned target priority ahead of broad fanout. AWSScheduledWorkPlanner
// and AWSFreshnessWorkPlanner plan ordinary AWS collector work from configured
// schedules or webhook freshness triggers. Planners produce workflow rows only;
// claim ownership and fact emission stay with the collector runtimes.
package coordinator
