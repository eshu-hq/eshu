// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "observability" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_observability.go).
//
// Eighteen fact kinds live here, spanning two collection lanes that share one
// reducer domain (observability_coverage_correlation):
//
//   - DECLARED lane (git collector, generic passthrough): DeclaredFolder,
//     DeclaredDashboard, DeclaredDatasource, DeclaredAlertRule,
//     DeclaredScrapeConfig, DeclaredMetricRule, DeclaredMetricRoute,
//     DeclaredLogRoute, DeclaredTraceRoute.
//   - OBSERVED/APPLIED lane (live grafana/loki/tempo/prometheusmimir plus the
//     git applied passthrough): AppliedResource, AppliedSyncState,
//     ObservedDashboard, ObservedTarget, ObservedRule, ObservedLogSignal,
//     ObservedTraceSignal.
//   - SourceInstance and CoverageWarning span both lanes.
//
// # Why every struct carries the full candidate-key union
//
// The family's single reducer payload consumer is the coverage-metadata
// classifier (go/internal/reducer/observability_coverage_metadata.go,
// observabilityMetadataEvidenceFromEnvelope). It reads a bounded union of named
// keys — a 20-entry object-ref fallback chain plus the
// provider/backend_kind/source_kind/source_class/resource_class/outcome/
// freshness_state/warning_kind/drift_candidate_reason/declared_match_state/
// service_hints/service_ref reads — via firstNonBlank and switch, the SAME
// union regardless of fact kind. Each struct therefore models that full union so
// the reducer reads every candidate from the typed struct. Because the parent
// module's marshal-free decoder (decode_map.go) ignores unknown top-level keys,
// a closed struct exposing exactly those keys is byte-identical to the raw
// payloadString map access it replaces: a key the emitter never sets simply
// decodes to nil, exactly as payloadString returned "" for an absent key.
//
// # Required fields
//
// SourceInstanceID (source_instance_id) is required on every kind. It is the one
// identity field EVERY observability collector injects on EVERY kind in BOTH
// lanes: git_observability_facts.go's observabilityBasePayload sets it
// unconditionally, and every live-collector basePayload sets it from a
// validate-non-blank ctx.SourceInstanceID. A fact missing it is a malformed
// collector emission, so it dead-letters as a classified input_invalid decode
// error rather than yielding a coverage decision with no source anchor.
//
// ProviderObjectUID (provider_object_uid) is ALSO required on the four observed
// kinds whose sole live emitter validates the uid non-blank and always writes
// it: ObservedDashboard (grafana), ObservedTarget (prometheusmimir),
// ObservedLogSignal (loki), ObservedTraceSignal (tempo). It stays OPTIONAL on
// ObservedRule, because the Grafana observed_rule emitter identifies the rule by
// alert_rule_uid instead — requiring provider_object_uid there would dead-letter
// every Grafana observed rule.
//
// No per-kind UID or name field is required on the DECLARED lane. That lane is a
// generic passthrough that copies whatever keys the source manifest declared, so
// requiring, say, dashboard_uid on DeclaredDashboard would dead-letter a valid
// declared fact whose manifest simply lacks that exact key — a silent accuracy
// regression, the opposite of this family's decode-time guarantee. Every declared
// per-kind key is therefore an optional pointer.
//
// # Flatness and version shims
//
// Every struct here is FLAT — none carries an untyped Attributes pass-through.
// Unlike the AWS/GCP cloud-inventory families, no observability kind is a
// polymorphic multi-shape envelope whose service-specific fields must be typed
// per subtype: the reducer reads only the bounded candidate union above, which a
// closed struct covers. A required field is non-pointer with no omitempty; an
// optional field is a pointer carrying omitempty, so an absent value decodes to
// nil and stays distinct from an observed empty string.
//
// The reducer decodes only the latest struct for each kind. Version shims for an
// older schema major live in the parent factschema package's decode seam
// (decodeLatestMajor in decode.go), never in this package or in reducer handler
// code.
package v1
