// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package serviceintelhttp is the HTTP adapter that serves the service
// intelligence report route, GET /api/v0/services/{service_name}/intelligence-report.
//
// It is the single composition point that joins the lower layers without an
// import cycle: it calls the query package's BuildServiceStoryEnvelope seam to
// build the service-story dossier and truth, then composes the report with the
// pure serviceintel composer. The query package imports none of this package's
// dependencies, so the dependency flows one way (serviceintelhttp -> {query,
// serviceintel, reducer, storage/postgres}; serviceintel -> query).
//
// The handler runs no LLM path and re-derives no service-story truth: the
// report's overall truth is the service-story truth, and resolution failures
// (capability gate, scoped-token access, ambiguous or missing service) return the
// same error envelope as the service-story route.
//
// The supply_chain section is sourced from reducer-owned supply-chain impact
// inventory through the SupplyChainEvidenceSource seam
// (DurableSupplyChainEvidenceSource in production): it loads a bounded,
// workload-scoped inventory page and carries supply-chain-impact truth, not the
// service-story platform truth. No inventory or a load error (logged, never
// report-fatal) leaves the section unsupported with its fallback next call
// rather than fabricating an empty supported section.
//
// The incidents_support section is sourced from durable incident-routing evidence
// through the IncidentEvidenceSource seam (DurableIncidentEvidenceSource in
// production): it resolves the workload's catalog service id, then loads that
// service's incident routing evidence and carries incident-context truth, not the
// service-story platform truth. The section is attributed only when the workload
// resolves to exactly one catalog service; an unresolved or ambiguous workload, a
// load error (logged, never report-fatal), or no routed incidents all leave the
// section unsupported with its fallback next call rather than fabricating a false
// "no incidents".
package serviceintelhttp
