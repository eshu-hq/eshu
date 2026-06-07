// Package status owns the shared reporting shape for Eshu pipeline state,
// backlog, generation lifecycle, and request-lifecycle health.
//
// Types in this package project raw runtime counts and lifecycle events
// into operator-facing reports consumed by the CLI, HTTP admin surfaces,
// and runtime status views. Keep these surfaces aligned: operators should
// not need a different mental model for each Eshu service. JSON shapes here
// are part of the operator contract and must change in lockstep with the CLI
// reference and runtime admin docs. The health projection treats reducer-owned
// shared projection backlog as unfinished graph-visible work, while lease-only
// shared-projection activity remains observable without blocking healthy, so
// code graph and dead-code queries do not look ready while accepted edges are
// still being written. The degraded health state weighs workflow-coordinator
// failures within CoordinatorRecentFailures (a bounded recent window) rather
// than cumulative all-time counts, so a recovered stack reports healthy again
// instead of staying degraded until aged failure rows are pruned; cumulative
// counts remain in the report as informational detail. The TerraformStateReport
// section, surfaced under Report.TerraformState, exposes per-locator state
// serial advance, safe source handles, and recent warning_fact rows grouped by
// warning_kind with severity/actionability classification so operators can
// separate blocking missing-state evidence from accepted parser guardrails.
// RegistryCollectorSnapshot rows expose aggregate OCI and package-registry
// runtime liveness, bounded failure classes, and package-registry metadata
// target counts without registry object names, package names, or credentials.
// CollectorRuntimeStatus rows derive a unified collector inventory from
// workflow coordinator registrations, durable direct status evidence, and
// active persisted source or reducer fact evidence, including Git repository
// ingestion facts, so coordinator-managed, direct-mode, disabled, and
// unregistered collectors are visible in one operator view.
// AWSCloudScanStatus rows expose per-account, per-region, per-service AWS
// scanner liveness, throttle counts, warning state, and commit status so
// operators can separate throttling, credential failure, budget exhaustion, and
// commit failures without scanning logs.
package status
