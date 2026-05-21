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
// discovery candidates. OCIRegistryWorkPlanner and PackageRegistryWorkPlanner
// each plan one bounded work item per configured target without opening
// provider connections. AWSScheduledWorkPlanner and AWSFreshnessWorkPlanner
// plan ordinary AWS collector work from configured schedules or webhook
// freshness triggers. Planners produce workflow rows only; claim ownership and
// fact emission stay with the collector runtimes.
package coordinator
