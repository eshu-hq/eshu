// Package coordinator runs the workflow coordinator's reconcile, expired-claim
// reap, and workflow-run reconciliation loops.
//
// Service ticks reconcile declarative collector instances against the durable
// store on every reconcile interval. In active mode it also drains expired
// claims on the reap interval and advances workflow run progress. Config is
// loaded from ESHU_WORKFLOW_COORDINATOR_* environment variables; deployment
// mode is "dark" or "active" and active mode requires claims enabled with at
// least one claim-capable collector instance.
//
// TerraformStateWorkPlanner plans Terraform-state collection runs from the
// resolved discovery candidates. OCIRegistryWorkPlanner and
// PackageRegistryWorkPlanner each plan one bounded work item per configured
// target without opening provider connections. Planners produce workflow rows
// only; claim ownership and fact emission stay with the collector runtimes.
package coordinator
