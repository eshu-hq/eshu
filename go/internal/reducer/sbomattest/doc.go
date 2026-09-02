// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sbomattest decides which SBOM and attestation documents attach to a
// container image, and publishes those decisions as durable reducer facts.
//
// The family owns one handler, SBOMAttestationAttachmentHandler, and the
// pipeline behind it: BuildSBOMAttestationAttachmentDecisions turns a batch of
// fact envelopes into per-document decisions, the classifier normalizes
// verification status, the index and SLSA index resolve which components and
// provenance statements an attachment covers, and
// PostgresSBOMAttestationAttachmentWriter persists the admitted decisions.
//
// Decisions carry an SBOMAttachmentStatus rather than a boolean, because
// "attached", "unverified" and "rejected" are different answers a caller acts
// on differently. Callers that only need the durable fact name should use
// SBOMAttestationAttachmentFactKind, which aliases the exported constant in
// [reducercontract] so the reducer root's supply_chain_impact family and this
// package can both name it without either importing the other.
//
// # Package boundary
//
// Imports point strictly downward: this package reaches
// [reducercontract], [factdecode], [factload], [factwrite], [payloadcore],
// [schemadecode], internal/boundedset, internal/facts, internal/telemetry, and
// internal/truth, and never the parent internal/reducer package. The reducer
// root keeps compatibility aliases so its own callers compile unchanged; that
// direction is root importing this family, never the reverse. See AGENTS.md in
// this directory before adding an import.
//
// # Observability
//
// Handle emits eshu_dp_sbom_attestation_attachments_total (labeled by domain
// and outcome) once per non-empty attachment status after building a batch of
// decisions. Documents rejected for malformed payloads increment the shared
// eshu_dp_reducer_input_invalid_facts_total counter instead, and the reducer
// executions that run this handler stay covered by
// eshu_dp_reducer_executions_total and eshu_dp_reducer_run_duration_seconds.
package sbomattest
