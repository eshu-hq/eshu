// Package coordinator runs the workflow coordinator's reconcile,
// webhook-freshness handoff, expired-claim reap, and workflow-run reconciliation
// loops.
//
// Service reconciles declarative collector instances against the durable store
// on every reconcile interval. In active mode it also applies optional hosted
// collector egress policy before planning supported collector work, requires
// explicit hosted extension egress policy before planning component extension
// work, drains expired claims on the reap interval, and advances workflow run progress.
// Config is loaded from workflow-coordinator environment variables; deployment
// mode is "dark" or "active" and active mode requires claims enabled with at
// least one enabled claim-capable collector instance.
//
// TerraformStateWorkPlanner plans Terraform-state collection runs from resolved
// discovery candidates. OCIRegistryWorkPlanner, PackageRegistryWorkPlanner,
// VulnerabilityIntelligenceWorkPlanner, CICDRunWorkPlanner, JiraWorkPlanner,
// and LokiWorkPlanner each plan bounded work items without opening provider
// connections; the Loki planner emits one work item per enabled configured Loki
// target and partitions claims by a per-target fairness key. Package and
// vulnerability planners preserve direct and owned target priority ahead of
// broad fanout and report aggregate skipped-target evidence when an
// owned-package derivation budget is exhausted or partial dependency evidence
// cannot safely become an exact vulnerability source query.
// Service reads one bounded owned-package lookahead beyond each planning
// budget so requested scope sets can show exhaustion without widening admitted
// work. CICDRunWorkPlanner plans bounded CI/CD run collection work from
// configured GitHub Actions repository targets. PagerDutyWorkPlanner plans
// incident-evidence work from configured PagerDuty targets.
// PrometheusMimirWorkPlanner plans bounded metric-metadata work, one item per
// enabled Prometheus or Grafana Mimir target, partitioned by target scope so
// concurrent reconciles never contend for one metric source.
// TempoWorkPlanner plans one bounded trace-signal work item per enabled
// Grafana Tempo target parsed from collector instance configuration, skipping
// disabled targets. GrafanaWorkPlanner plans one bounded observability work item
// per enabled Grafana target parsed from configuration.targets, skipping disabled
// targets and partitioning by a per-target fairness key so concurrent reconciles
// never claim the same target twice. ScannerWorkerWorkPlanner plans explicit
// scanner-worker source evidence targets so a healthy worker must still have
// claimable work before a proof can count source evidence. AWSScheduledWorkPlanner and
// AWSFreshnessWorkPlanner plan ordinary AWS collector work from configured
// schedules or webhook freshness triggers. ComponentExtensionWorkPlanner plans
// source-evidence-only work for verified claim-capable component activations
// loaded from the local component registry after hosted extension egress policy
// allows the component identity; it stores component identity, manifest digest,
// runtime protocol, and a safe config handle, not raw component configuration
// or credentials.
// Incident freshness handoff narrows PagerDuty and Jira webhook wake-ups to
// authorized configured scope IDs before creating normal collector work. Planners
// produce workflow rows only. When a workflow tenant boundary is configured,
// Service intersects planned work items with active tenant/workspace scope
// grants before durable rows exist, and claim ownership and fact emission stay
// with the collector runtimes.
package coordinator
