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
// still being written. The TerraformStateReport
// section, surfaced under Report.TerraformState, exposes per-locator state
// serial advance and recent warning_fact rows grouped by warning_kind so
// operators can confirm tfstate liveness without scanning the fact stream.
// RegistryCollectorSnapshot rows expose aggregate OCI and package-registry
// runtime liveness and bounded failure classes without registry object names.
// AWSCloudScanStatus rows expose per-account, per-region, per-service AWS
// scanner liveness, throttle counts, warning state, and commit status so
// operators can separate throttling, credential failure, budget exhaustion, and
// commit failures without scanning logs.
package status
