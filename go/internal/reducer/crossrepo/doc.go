// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package crossrepo resolves cross-repository relationships from persisted
// evidence facts and emits the durable repo-dependency projection intents the
// shared projection later turns into graph edges.
//
// The family owns one handler, CrossRepoRelationshipHandler, and the pipeline
// behind it: the handler loads evidence facts and assertions, runs
// relationships.Resolve, persists candidates and resolved edges for the audit
// trail, then converts the resolved set into shared projection intent rows
// (buildResolvedEdgeIntentRows, exported for Ifá as
// ExtractRepoDependencyIntentRows) or, for a source repo that resolved to
// nothing, retraction rows (buildResolvedEdgeRetractionIntentRows). Two pure
// classifiers shape what those rows carry:
// resolvedRelationshipEvidenceArtifacts builds the bounded EvidenceArtifact
// summaries, and resolvedRelationshipEvidenceType /
// resolvedRelationshipSourceTool derive the edge's evidence_type and
// source_tool tokens from its primary evidence kind.
//
// CrossRepoEvidenceSource is exported because the Ifá assert gate partitions on
// it: workload materialization writes the identical
// (WorkloadInstance)-[:RUNS_ON]->(Platform) shape under a different
// evidence_source, so relationship type and endpoint labels cannot tell the two
// families' edges apart.
//
// # Package boundary
//
// Imports point strictly downward: this package reaches [reducercontract],
// [gpphase], [payloadcore], [sharedintent], internal/environment,
// internal/ghactionsref, internal/relationships, internal/telemetry and
// pkg/log, and never the parent internal/reducer package. The reducer root keeps
// compatibility aliases in cross_repo_compat.go so its own callers -- and
// cmd/reducer, internal/ifa/materializededges and internal/storage/cypher,
// which spell these names as reducer.X -- compile unchanged. That direction is
// root importing this family, never the reverse. See AGENTS.md in this
// directory before adding an import.
//
// # Observability
//
// Resolve emits four instruments registered in internal/telemetry:
// eshu_dp_cross_repo_resolution_duration_seconds once per generation,
// eshu_dp_cross_repo_evidence_loaded_total after deduping the loaded evidence
// facts, eshu_dp_cross_repo_edges_resolved_total once the resolved edges are
// counted, and eshu_dp_cross_repo_activation_fenced_total when durable
// acceptance intents fail to commit and activation is fenced. Every Instruments
// access is nil-guarded, so a handler constructed without telemetry resolves
// normally and reports nothing.
package crossrepo
