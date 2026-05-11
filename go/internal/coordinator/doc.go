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
// resolved discovery candidates. The planner consumes BackendFacts which now
// returns both Terraform backend block candidates and Terragrunt remote_state
// candidates resolved into their underlying backend kind, so no separate
// scheduler shape is needed for Terragrunt sources.
package coordinator
