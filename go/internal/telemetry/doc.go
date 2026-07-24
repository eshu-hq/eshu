// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package telemetry owns Eshu's frozen Go data-plane OpenTelemetry contract:
// metric instruments, span names, structured log keys, and shared runtime
// attributes.
//
// The frozen contract lives in contract.go (metric, span, scope, phase,
// and failure-class names) and the metric instruments themselves live in
// instruments.go. Metric names use the eshu_dp_ prefix; new dimensions and
// span names must be registered in contract.go before use, including
// documentation extraction counters, Terraform-state collector spans, webhook
// listener spans, OCI registry collector spans, and the safe_locator_hash and
// warning_kind dimensions used by the tfstate output, module, warning emission,
// correlation drift-match, drift-admission, drift-intent enqueue, AWS
// cloud-runtime orphan and unmanaged resource, webhook, and OCI registry
// counters. Webhook listener intake registers provider,
// event_kind, decision, status, SpanWebhookHandle, and SpanWebhookStore. OCI
// registry intake registers operation, media_family, artifact_family,
// SpanOCIRegistryScan, and SpanOCIRegistryAPICall. The reducer drift handlers use
// SpanReducerDriftEvidenceLoad for config_state_drift and
// SpanReducerAWSRuntimeDriftEvidenceLoad for aws_cloud_runtime_drift so traces
// separate Terraform config-vs-state joins from AWS runtime ARN joins.
// SpanQueryChangeSurfaceInvestigation names the prompt-facing change-surface
// route that combines target resolution, content handles, and bounded graph
// traversal. SpanQueryHardcodedSecretInvestigation names the prompt-facing
// security route that returns redacted hardcoded-secret candidates from indexed
// content and keeps that span name in contract_query_spans.go.
// SpanQueryEvidenceCitationPacket names the prompt-facing citation
// hydration route that turns explicit content handles into bounded source and
// documentation proof. SpanQuerySemanticEvidence names the opt-in semantic
// documentation observation and code-hint fact read path, keeping semantic
// provenance traces separate from deterministic documentation, code, and graph
// truth routes. SpanQuerySBOMAttestationAttachments names the
// digest/document-anchored SBOM and attestation attachment read path.
// SpanQueryAdvisoryEvidence names the source-only advisory evidence read path,
// SpanQueryAdvisoryCatalog names the browsable, summary-only CVE-intelligence
// catalog read path,
// SpanQueryWorkItemEvidence names the bounded Jira/work-item source evidence
// read path,
// SpanQueryFreshnessGenerationLifecycle names the bounded scope generation
// lifecycle drilldown read path,
// SpanQueryFreshnessChangedSince names the bounded repository-scope changed-since
// delta read path,
// and SpanQuerySupplyChainImpactExplanation names the bounded vulnerability
// finding/advisory explanation route. Pipeline stage, graph-backend, and
// failure-class names
// stay centralized here so runtime packages can report comparable events
// without inventing local label vocabularies. The drift loader's module-aware join
// (issue #169) registers
// the DriftUnresolvedModuleCalls counter and the
// MetricDimensionDriftUnresolvedModuleReason dimension here so the closed
// enum of unresolvable-module reasons (external_registry, external_git,
// external_archive, cross_repo_local, cycle_detected, depth_exceeded,
// module_renamed) stays anchored to the contract surface. The
// streaming-nested-walker work in
// ADR 2026-05-12-tfstate-parser-composite-capture-for-schema-known-paths
// registers the DriftSchemaUnknownComposite counter and the
// MetricDimensionResourceType and MetricDimensionCompositeSkipReason
// dimensions here so operators can detect provider-schema drift and classify
// each skip's cause via the closed-enum reason label; high-cardinality
// companions (attribute_key, source path, error) stay in summary warning facts
// and bounded LogKeyDriftComposite* log attrs, out of metric labels.
// Scanner-worker contracts register analyzer, target_kind, and limit_kind
// dimensions plus claim, analyzer execution, and fact batch spans so isolated
// security analyzer runtimes can prove queue age, duration, CPU, memory,
// retry, dead-letter, target count, and result count without inventing local
// labels.
// Jira source collection registers SpanJiraObserve, SpanJiraFetch, and bounded
// jira.fetch span attributes for search pages, changelog pages, remote-link
// pages, metadata pages, emitted issues, emitted changelog events,
// emitted/rejected remote links, metadata objects scanned/emitted, unsupported
// provider links, unsupported metadata, permission-hidden metadata, stale
// metadata, metadata redactions, partial failures, rate limits, Retry-After
// seconds, and stale updated windows. These stay span attributes rather than
// metric labels so site IDs, issue keys, user identifiers, summaries, metadata
// names, custom-field IDs, and URLs remain out of dashboard cardinality.
// Grafana source collection registers SpanGrafanaObserve, SpanGrafanaFetch,
// provider request, fact emitted, rate-limit, retry, redaction, and fetch
// duration instruments. Instance IDs, titles, datasource names, URLs, query
// models, contacts, notification routes, and token values stay out of metric
// labels.
//
// Prometheus/Mimir source collection registers SpanPrometheusMimirObserve,
// SpanPrometheusMimirFetch, provider request, fact emitted, rate-limit, retry,
// redaction, stale, and fetch duration instruments. Instance IDs, scrape target
// URLs, label values, raw PromQL, annotations, tenant IDs, tenant headers, and
// token values stay out of metric labels.
//
// Loki source collection registers SpanLokiObserve, SpanLokiFetch, provider
// request, fact emitted, rate-limit, retry, redaction, high-cardinality
// rejection, stale, and fetch duration instruments. Instance IDs, label values,
// private URLs, raw LogQL, tenant IDs, tenant headers, and token values stay
// out of metric labels.
//
// Tempo source collection registers SpanTempoObserve, SpanTempoFetch, and
// bounded provider, fact, retry, rate-limit, redaction, high-cardinality, stale,
// and duration instruments for metadata-only trace-signal collection. Raw tag
// values, tenant IDs, trace IDs, spans, request attributes, TraceQL bodies, and
// provider response bodies stay out of metric labels.
// The supply-chain reducer registers SupplyChainSuppressionDecisions
// (eshu_dp_supply_chain_suppression_decisions_total), labeled by reducer
// domain and outcome state (active, not_affected, accepted_risk,
// false_positive, ignored, expired, provider_dismissed, scope_mismatch),
// so operators can detect VEX/operator-policy suppression drift without
// re-running the reducer; provider-dismissed and scope-mismatch counts
// stay separated from operator-asserted hides because they keep audit
// signal rather than hiding findings. The same reducer registers
// SupplyChainRemediationDecisions
// (eshu_dp_supply_chain_remediation_decisions_total), labeled by reducer
// domain, outcome (confidence: exact, partial, unknown), and reason (closed
// remediation-reason enum: direct_upgrade_allowed, direct_range_blocked,
// transitive_parent_upgrade_required, no_patched_version,
// multiple_patched_branches, package_manager_unsupported,
// manifest_range_missing, manifest_range_malformed,
// installed_version_missing, installed_version_malformed) so operators can
// see how often Eshu produces an exact advisory-only safe-upgrade path
// versus how many findings still need additional ecosystem support to
// graduate from unknown.
// Search decay scoring registers SearchDecayPolicyApplications
// (eshu_dp_search_decay_policy_applications_total), labeled by bounded
// policy_id, evidence_class, and outcome, so operators can diagnose which
// non-canonical ranking metadata policy affected results without turning
// evidence ids, graph handles, repository ids, or service ids into metric
// labels.
// Graph orphan cleanup registers GraphOrphanNodes
// (eshu_dp_graph_orphan_nodes), labeled only by closed node_label values, so
// operators can see residual zero-relationship graph nodes without exposing
// repository or resource identities.
// SpanAttrWorkItemEvidence* constants name the bounded work-item evidence
// query span counts for query volume, returned rows, evidence states, and
// truncation without adding tenant, user, issue, URL, or summary values to
// metric labels.
// SpanAttrGraphReadOutcome, SpanAttrGraphReadAttempts, and
// SpanAttrGraphReadConfiguredDeadlineMS describe the bounded Neo4jReader
// policy. The shared outcome vocabulary distinguishes a graph-policy deadline
// from an earlier caller deadline without recording query text, graph
// addresses, or raw driver errors.
// Callers must reuse existing log keys and Attr* helpers before adding new
// names. High-cardinality values such as file paths, fact identifiers,
// repository names, delivery IDs, source paths, and attribute keys belong in
// spans or logs, never in metric labels, so dashboards and alerts stay bounded.
// SCIP snapshot attempt telemetry uses only bounded language and result labels;
// result values distinguish used, disabled, unsupported-language,
// unavailable-binary, indexer-failed, parse-failed, and empty-result outcomes
// without exposing repository names, file paths, or index paths.
// Resource identifiers that may carry sensitive names use SafeResourceLogAttrs
// before they enter logs, which preserves correlation through a deterministic
// fingerprint and bounded type fields without copying the raw identifier.
package telemetry
