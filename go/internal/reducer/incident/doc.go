// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package incident owns the PagerDuty incident-routing reducer family: exact
// graph-evidence materialization for one scope generation's declared,
// applied, and observed PagerDuty routing (#2161), plus the durable
// incident-to-repository correlation that resolves an applied PagerDuty
// service to its owning config repository through the Terraform
// backend-locator join.
//
// [IncidentRoutingMaterializationHandler] is the reducer intent handler for
// the contract package's DomainIncidentRoutingMaterialization. It loads the
// RAW incident-context and incident-routing fact envelopes through
// [IncidentRoutingEvidenceLoader], decodes each through the typed contracts
// seam (buildIncidentRoutingEvidenceInputs), and projects only full
// declared/applied/observed exact convergence — or exact live-only no-IaC
// evidence — into graph rows via [ExtractIncidentRoutingEvidenceRows].
// Drifted, stale, permission-hidden, ambiguous, unresolved, rejected,
// derived, and missing evidence stays provenance-only. A fact whose payload
// is missing a required field is quarantined per-fact as an input_invalid
// dead-letter (factdecode.PartitionDecodeFailures) rather than silently
// dropped or failing the whole intent; only a genuinely fatal decode error
// aborts the intent.
//
// [IncidentRepositoryCorrelationHandler] is the reducer intent handler for
// DomainIncidentRepositoryCorrelation. [BuildIncidentRepositoryCorrelations]
// groups applied PagerDuty service routing rows by provider service id,
// resolves each distinct (backend_kind, locator_hash) to its owning
// repository through the supplied [BackendRepositoryResolver] exactly once
// (memoized), and classifies each provider service id into exactly one
// exact/derived/ambiguous/unresolved/rejected decision. Only exact and
// derived decisions carry a durable repository edge and a non-empty
// RepositoryID; every weaker outcome stays provenance-only so a downstream
// scoped predicate fails closed. [PostgresIncidentRepositoryCorrelationWriter]
// persists every outcome — never only the edge-bearing ones — through the
// shared batched fact-insert path.
package incident
