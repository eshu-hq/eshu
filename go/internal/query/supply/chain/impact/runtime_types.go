// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// Runtime-evidence read-model types relocated from root package query
// (#6060 lane A). The declarations moved here byte-identical because the
// moved finding rows and runtime-context store name them and this package
// must not import root; the staying probe files keep `type X = impact.X`
// aliases (see root compat_supply_chain.go) so handler code, tests,
// and cmd wiring keep compiling unchanged. The probes that populate these
// types stay in root until the hub PR3 (they drive off SupplyChainHandler
// and, for the fair probe, root GraphQuery).
//
// Relocated declarations and their root sources:
//   - RuntimeContext, packet.RuntimeContextResult,
//     packet.RuntimeEnvironmentEvidenceProbe,
//     RuntimeEnvironmentCandidate: from
//     runtime_context_probe.go.
//   - packet.KubernetesRuntimeWorkloadRef: from
//     kubernetes_runtime_probe.go.
//   - packet.KubernetesRuntimeProbeMetadata: from
//     kubernetes_runtime_probe_fair.go.

// RuntimeContext is one repository's read-time-resolved runtime
// context: the workloads, services, deployments, environments, and catalog
// refs that repository currently maps to, resolved from active
// workload_identity, service_catalog_correlation, platform_materialization,
// and deployment-correlation facts at query time (issue #5746).
//
// Read-time resolution is deliberate: a finding's baked payload fields are
// stamped at reduce time and go stale the moment runtime reality changes
// (redeploy, delete, promote), while this join reads the CURRENT facts on
// every request. Absence of a workload here is an honest "current state of
// knowledge" that self-heals on the next read — no readiness gate, no
// re-enqueue, no fan-out.
type RuntimeContext struct {
	WorkloadIDs       []string
	ServiceIDs        []string
	DeploymentIDs     []string
	Environments      []string
	CatalogEntityRefs []string
	CatalogOwnerRefs  []string
}

// RuntimeEnvironmentCandidate identifies one finding-bound
// digest/environment pair that must be revalidated against current accepted
// CI/CD correlation facts before it can enter read-time runtime_context.
// Relocated from root package query's
// runtime_context_probe.go (#6060 lane A): the moved
// runtime-environment store names it and this package must not import root,
// so the declaration lives here and root keeps a `type X = impact.X` alias
// (see root compat_supply_chain.go).
type RuntimeEnvironmentCandidate struct {
	SubjectDigest string
	Environment   string
}
