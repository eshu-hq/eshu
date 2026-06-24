// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// Materialization edge-confidence weights.
//
// These weights are graph-write edge-property values stamped onto canonical
// relationships during reducer materialization. They are deliberately separate
// from the relationships.DefaultConfidenceRegistry (#3509, #3516): that registry
// holds per-EvidenceKind *priors* that feed the Bayesian corroboration math
// before a relationship is admitted, whereas these weights are the *settled*
// confidence written onto an already-admitted materialized edge.
//
// Each weight is parameterized into its materialization Cypher statement as a
// named Cypher parameter rather than embedded as a bare literal, so the value
// has a single documented home and the graph-write template carries no magic
// number (#3518).
const (
	// MaterializationConfidenceParam is the Cypher parameter name that carries a
	// materialization edge-confidence weight into a materialization statement.
	// It is a statement-scoped scalar parameter, distinct from the per-row $rows
	// batch, so the planner treats the weight as a constant for the whole UNWIND.
	MaterializationConfidenceParam = "edge_confidence"

	// ProvisionsPlatformEdgeConfidence is the confidence stamped on a
	// Repository-[:PROVISIONS_PLATFORM]->Platform edge during infrastructure
	// platform materialization.
	//
	// Provenance: the edge is admitted only when correlated Terraform cluster and
	// module data already declare the platform provisioning relationship, so the
	// target platform is named by infrastructure-as-code rather than inferred.
	// This is the strongest materialized edge weight here (just below a certain
	// 1.0) because the binding is explicit and machine-authored; the small margin
	// below 1.0 reserves headroom for a contradicting downstream signal.
	ProvisionsPlatformEdgeConfidence = 0.98

	// RuntimeServiceDependencyEdgeConfidence is the confidence stamped on a
	// DEPENDS_ON edge (Repository->Repository and Workload->Workload) materialized
	// from a runtime services-list declaration.
	//
	// Provenance: a runtime services list explicitly enumerates the dependency, so
	// the edge reflects a declared, authored relationship rather than a value- or
	// convention-matched inference. It sits below ProvisionsPlatformEdgeConfidence
	// because a services-list declaration names intent to depend, which is one
	// step weaker than IaC declaring concrete platform provisioning.
	RuntimeServiceDependencyEdgeConfidence = 0.9

	// DefaultRuntimePlatformEdgeConfidence is the fallback confidence stamped on a
	// WorkloadInstance-[:RUNS_ON]->Platform edge when the projected platform row
	// carries no positive per-row confidence.
	//
	// Provenance: the RUNS_ON edge normally inherits the projected platform
	// confidence; when that value is absent or non-positive the platform was
	// inferred without a stronger corroborating signal, so it defaults to the same
	// services-list-declaration strength rather than claiming a higher binding it
	// has not earned.
	DefaultRuntimePlatformEdgeConfidence = 0.9
)
