// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Capability keys for the root status read routes, the live evidence bundle,
// the operator control plane, and the two governance audit reads (#8227). Each
// route reads through the guarded PostgreSQL reader, so a stale reader answers
// a retryable 503 that names the route's key in the error envelope. The
// handlers spell the key as a literal or their own package's const; these
// consts feed only the registrations below. The rows live in this file because
// the package sits at the dirgate 40-file cap.
const (
	// operationsPipelineStatusCapability names operations.pipeline_status, the key for GET /api/v0/status/pipeline.
	operationsPipelineStatusCapability = "operations.pipeline_status"

	// operationsCollectorStatusCapability names operations.collector_status, the key for GET /api/v0/status/collectors and GET /api/v0/collectors.
	operationsCollectorStatusCapability = "operations.collector_status"

	// operationsIngesterStatusCapability names operations.ingester_status, the key for GET /api/v0/status/ingesters/{ingester} and GET /api/v0/ingesters/{ingester}.
	operationsIngesterStatusCapability = "operations.ingester_status"

	// operationsIndexStatusCapability names operations.index_status, the key for GET /api/v0/status/index and GET /api/v0/index-status.
	operationsIndexStatusCapability = "operations.index_status"

	// operationsHostedReadinessStatusCapability names operations.hosted_readiness_status, the key for GET /api/v0/status/hosted-readiness.
	operationsHostedReadinessStatusCapability = "operations.hosted_readiness_status"

	// operationsFreshnessCausalityStatusCapability names operations.freshness_causality_status, the key for GET /api/v0/status/freshness-causality.
	operationsFreshnessCausalityStatusCapability = "operations.freshness_causality_status"

	// operationsCollectorReadinessStatusCapability names operations.collector_readiness_status, the key for GET /api/v0/status/collector-readiness and GET /api/v0/collector-readiness.
	operationsCollectorReadinessStatusCapability = "operations.collector_readiness_status"

	// operationsOperatorControlPlaneStatusCapability names operations.operator_control_plane_status, the key for GET /api/v0/status/operator-control-plane.
	operationsOperatorControlPlaneStatusCapability = "operations.operator_control_plane_status"

	// operationsEvidenceBundleLiveCapability names operations.evidence_bundle_live, the key for GET /api/v0/evidence/bundle.
	operationsEvidenceBundleLiveCapability = "operations.evidence_bundle_live"

	// auditEventsListCapability names audit.events.list, the key for GET /api/v0/auth/admin/audit/events.
	auditEventsListCapability = "audit.events.list"

	// auditSummaryReadCapability names audit.summary.read, the key for GET /api/v0/auth/admin/audit/summary.
	auditSummaryReadCapability = "audit.summary.read"
)

func init() {
	register(operationsStatusCapability, capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	})
	register(operationsPipelineStatusCapability, capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	})
	register(operationsCollectorStatusCapability, capabilitySupport{
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalLightweight,
	})
	register(operationsIngesterStatusCapability, capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	})
	// index_status reads the repository count from the graph on both the
	// scoped and unscoped paths, and local_lightweight wires a driverless graph
	// reader, so the lightweight profile cannot serve it (#8227).
	register(operationsIndexStatusCapability, capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	})
	register(operationsHostedReadinessStatusCapability, capabilitySupport{
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalLightweight,
	})
	register(operationsFreshnessCausalityStatusCapability, capabilitySupport{
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalLightweight,
	})
	register(operationsCollectorReadinessStatusCapability, capabilitySupport{
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalLightweight,
	})
	register(operationsOperatorControlPlaneStatusCapability, capabilitySupport{
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalLightweight,
	})
	register(operationsEvidenceBundleLiveCapability, capabilitySupport{
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalLightweight,
	})
	register(auditEventsListCapability, capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	})
	register(auditSummaryReadCapability, capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	})
}
