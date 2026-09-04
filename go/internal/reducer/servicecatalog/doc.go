// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package servicecatalog owns the service-catalog correlation family: matching
// service-catalog entity declarations (Backstage-shaped catalog-info.yaml
// facts, or any provider emitting the same fact kinds) against canonical
// repository evidence without letting catalog names create workloads, and the
// additive per-service evidence-generation lineage
// (ServiceMaterializationWrite / PostgresServiceMaterializationWriter) that
// commits alongside it.
//
// It covers the correlation decision builder
// (BuildServiceCatalogCorrelationDecisions), the reducer handler
// (ServiceCatalogCorrelationHandler), the Postgres correlation writer
// (PostgresServiceCatalogCorrelationWriter), and the seven per-service
// materialization evidence families the handler commits into the same
// generation as ownership when their respective loaders are wired: deployment
// (ServiceEvidenceFamilyDeployment, keyed by ServiceDeploymentEvidenceKey over
// the resolved deployment relationship's identity), dependencies
// (ServiceEvidenceFamilyDependencies, sharing deployment's resolved
// relationships and keyed by ServiceDependencyEvidenceKey), runtime
// (ServiceEvidenceFamilyRuntime, keyed by ServiceRuntimeEvidenceKey over the
// durable platform/environment/workload identity), docs
// (ServiceEvidenceFamilyDocs), incidents (ServiceEvidenceFamilyIncidents), and
// vulnerabilities (ServiceEvidenceFamilyVulnerabilities). Each family is
// evidence attached to a service, not a separate reducer domain: the
// service_materialization_* files are ServiceCatalogCorrelationHandler's own
// methods and evidence builders, not a sibling family that happens to share a
// filename prefix (issue #6061).
//
// The package never imports the parent reducer package. Everything it needs
// from the reducer's shared vocabulary comes from leaf packages instead:
// contract for the domain, intent, and result types plus the
// ServiceCatalogCorrelationFactKind fact-kind constant; factload for fact
// loading; factdecode for quarantine handling; factwrite for the batched
// fact-row writer; payloadcore for payload accessors and identity helpers;
// schemadecode for the sdk/go/factschema decode seam (service-catalog entity,
// ownership, and repository-link facts); and packagesourcecore for the exact
// and canonicalized repository-URL matching the classifier shares with the
// still-in-root package-source-correlation family. The reducer root keeps a
// compatibility surface (service_catalog_correlation_compat.go) that aliases
// this family's exported symbols back for its own remaining callers
// (cmd/reducer, internal/storage/postgres, and the still-in-root
// supply_chain_impact and service_runtime_instance_lookup families), so the
// import direction stays one-way: root depends on servicecatalog, never the
// reverse.
//
// RepositoryScopedResolvedRelationshipLoader is declared here rather than
// imported. The reducer root owns an identical interface
// (workload_materialization_handler.go) shared by several families that have
// not moved yet, and importing the root to reach it would invert the
// one-way dependency above. Go interfaces are structural, so a local
// declaration with the same method set is satisfied by the same concrete
// implementation root wires in, without duplicating any logic. The
// codetaint package resolves the same problem the same way.
//
// Telemetry: the correlation handler increments
// eshu_dp_service_catalog_correlations_total (labeled by domain and outcome)
// for every correlation decision, and
// eshu_dp_service_catalog_correlation_guardrails_total (labeled by domain and
// guardrail) whenever the candidate_fanout, dropped_ambiguous_candidate, or
// missing_anchor_entity guardrail fires -- kept separate from the decision
// counter so a guardrail event is never miscounted as a decision outcome.
// Facts rejected for a malformed payload feed the shared
// eshu_dp_reducer_input_invalid_facts_total counter through factdecode
// instead of a family-specific one. The service-materialization evidence
// families register no instrument of their own; their writes are covered by
// the same eshu_dp_reducer_executions_total /
// eshu_dp_reducer_run_duration_seconds pair every reducer handler execution
// emits.
package servicecatalog
