// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"log/slog"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
)

// Dual-write reconciliation (#3559).
//
// Graph edges carry denormalized provenance — generation_id and resolved_id —
// that point back to Postgres truth. The Postgres generation swap and the graph
// edge projection are NOT one atomic transaction (the reducer writes edges, then
// marks intents complete in a separate Postgres write; a generation swap flips
// Postgres authority independently). A partial failure across that boundary can
// leave a graph edge whose denormalized generation_id no longer matches the
// now-authoritative Postgres generation, or whose resolved_id no longer exists
// in the authoritative generation.
//
// This file implements a backend-neutral reconciliation pass that compares the
// authoritative Postgres generation against the denormalized provenance carried
// on graph edges, classifies each edge as in-sync or drifted, and produces a
// bounded retract plan that converges the graph back to Postgres truth. It does
// not query a backend itself: callers supply the authoritative Postgres view and
// the graph edge view, keeping the classifier pure and exhaustively testable.

// ReconciliationDriftClass classifies one denormalized graph edge against the
// authoritative Postgres generation. The set is closed so the operator-facing
// drift_kind metric label stays bounded.
type ReconciliationDriftClass string

const (
	// ReconciliationDriftInSync means the edge's denormalized generation_id
	// matches the authoritative Postgres generation for its acceptance unit and
	// its resolved_id still exists in that generation. No repair is required.
	ReconciliationDriftInSync ReconciliationDriftClass = "in_sync"

	// ReconciliationDriftStaleGeneration means the edge carries a generation_id
	// that is not the authoritative Postgres generation for its acceptance unit.
	// This is the partial-failure signature where Postgres swapped generations
	// but the graph still holds an edge stamped with a superseded generation
	// (graph-behind), or a graph write committed against a generation Postgres
	// never made authoritative (graph-ahead). Either way the edge is stranded
	// and must be retracted so the authoritative write re-projects clean truth.
	ReconciliationDriftStaleGeneration ReconciliationDriftClass = "stale_generation"

	// ReconciliationDriftOrphanResolvedID means the edge's generation_id matches
	// the authoritative Postgres generation, but its resolved_id is absent from
	// the authoritative resolved-relationship set. This is the partial-failure
	// signature where Postgres retired a resolved relationship within the active
	// generation but the graph edge denormalized to it was never retracted.
	ReconciliationDriftOrphanResolvedID ReconciliationDriftClass = "orphan_resolved_id"
)

// AcceptanceIdentity is the exact bounded-unit freshness key that Postgres
// keys shared_projection_acceptance on: (scope_id, acceptance_unit_id,
// source_run_id). The same acceptance unit can appear under different scopes or
// source runs with different authoritative generations, so the reconciliation
// pass MUST join an edge to Postgres truth on the full tuple, never on
// acceptance_unit_id alone — keying on the unit alone collapses sibling slices
// and classifies an edge against the wrong generation.
type AcceptanceIdentity struct {
	// ScopeID is the ingestion scope the acceptance row belongs to.
	ScopeID string
	// AcceptanceUnitID is the bounded unit within the scope.
	AcceptanceUnitID string
	// SourceRunID is the source run that produced the acceptance row.
	SourceRunID string
}

// AuthoritativePostgresGeneration is the Postgres-truth view for one exact
// acceptance identity at reconciliation time: the generation Postgres currently
// treats as authoritative, plus the resolved_id set that generation legitimately
// contains. Callers build this from shared_projection_acceptance (the
// authoritative generation per (scope_id, acceptance_unit_id, source_run_id))
// joined to the resolved_relationships rows of that generation.
type AuthoritativePostgresGeneration struct {
	// Identity is the exact (scope_id, acceptance_unit_id, source_run_id) key
	// this view describes.
	Identity AcceptanceIdentity
	// GenerationID is the generation Postgres currently treats as authoritative
	// for the acceptance identity.
	GenerationID string
	// ResolvedIDs is the set of resolved_relationship primary keys that the
	// authoritative generation legitimately contains. An empty map means the
	// authoritative generation contains no resolved relationships, which makes
	// every resolved_id-bearing graph edge an orphan.
	ResolvedIDs map[string]struct{}
}

// GraphDenormalizedEdge is one denormalized graph edge observed at
// reconciliation time, reduced to the provenance the reconciliation pass
// compares against Postgres truth. EdgeKey identifies the edge for logs and
// reporting; Identity joins it to the authoritative Postgres view; RetractAnchor
// is the domain-specific key the existing retract path acts on.
type GraphDenormalizedEdge struct {
	// EdgeKey uniquely identifies the edge within its domain for logs and
	// reporting. It is opaque to the classifier and is NOT a retract key.
	EdgeKey string
	// Domain is the shared-projection domain that owns the edge.
	Domain string
	// Identity joins the edge to its authoritative Postgres view on the exact
	// (scope_id, acceptance_unit_id, source_run_id) tuple.
	Identity AcceptanceIdentity
	// RetractAnchor is the domain-specific identifier EdgeWriter.RetractEdges
	// actually keys on: a repository id for repo-id-anchored domains, a section
	// scope id for documentation edges, or a file path for delta-scoped domains.
	// The convergence path feeds this to the retract builder; the opaque EdgeKey
	// must never be used as a retract key.
	RetractAnchor string
	// GenerationID is the denormalized generation stamped on the edge.
	GenerationID string
	// ResolvedID is the denormalized Postgres back-reference. Empty when the
	// domain does not carry a resolved_id; such edges are reconciled on
	// generation identity alone.
	ResolvedID string
}

// ReconciliationFinding is the classification of one graph edge against
// Postgres truth, including whether the authoritative view was available.
type ReconciliationFinding struct {
	// Edge is the graph edge that was classified.
	Edge GraphDenormalizedEdge
	// Class is the drift classification.
	Class ReconciliationDriftClass
}

// NeedsRetract reports whether the finding requires the edge to be retracted to
// converge the graph back to Postgres truth.
func (f ReconciliationFinding) NeedsRetract() bool {
	return f.Class != ReconciliationDriftInSync
}

// ReconciliationReport is the result of one reconciliation pass: the per-edge
// findings plus per-class counts an operator can read to confirm convergence.
type ReconciliationReport struct {
	// Findings is one entry per input edge, in input order.
	Findings []ReconciliationFinding
	// Counts is the number of edges in each drift class.
	Counts map[ReconciliationDriftClass]int
}

// DriftedEdgeKeys returns the opaque edge keys that need retraction to converge,
// grouped by domain and sorted. These keys are for logs and reporting only; they
// are NOT retract keys. Use RepairAnchors to drive the actual retract path.
func (r ReconciliationReport) DriftedEdgeKeys() map[string][]string {
	grouped := make(map[string][]string)
	for _, finding := range r.Findings {
		if !finding.NeedsRetract() {
			continue
		}
		grouped[finding.Edge.Domain] = append(grouped[finding.Edge.Domain], finding.Edge.EdgeKey)
	}
	for domain := range grouped {
		sort.Strings(grouped[domain])
	}
	return grouped
}

// RepairAnchors returns the domain-specific retract anchors that converge the
// graph, grouped by domain, deduped, and sorted. Each anchor is the key
// EdgeWriter.RetractEdges acts on for that domain (repository id, section scope
// id, or file path) — never the opaque EdgeKey. A caller drives convergence by
// building SharedProjectionIntentRows from these anchors and passing them to
// RetractEdges per domain. Edges with an empty RetractAnchor are skipped so a
// blank id can never widen a retract to every row.
func (r ReconciliationReport) RepairAnchors() map[string][]string {
	grouped := make(map[string][]string)
	seen := make(map[string]map[string]struct{})
	for _, finding := range r.Findings {
		if !finding.NeedsRetract() {
			continue
		}
		anchor := finding.Edge.RetractAnchor
		if anchor == "" {
			continue
		}
		domain := finding.Edge.Domain
		if seen[domain] == nil {
			seen[domain] = make(map[string]struct{})
		}
		if _, ok := seen[domain][anchor]; ok {
			continue
		}
		seen[domain][anchor] = struct{}{}
		grouped[domain] = append(grouped[domain], anchor)
	}
	for domain := range grouped {
		sort.Strings(grouped[domain])
	}
	return grouped
}

// Converged reports whether every classified edge is in sync with Postgres
// truth, meaning no edge is stranded and no retract is required.
func (r ReconciliationReport) Converged() bool {
	for class, count := range r.Counts {
		if class == ReconciliationDriftInSync {
			continue
		}
		if count > 0 {
			return false
		}
	}
	return true
}

// ClassifyReconciliationDrift compares denormalized graph edges against the
// authoritative Postgres generation per exact acceptance identity
// (scope_id, acceptance_unit_id, source_run_id) and classifies each edge. An
// edge whose exact identity has no authoritative Postgres view is treated as
// stale_generation: Postgres no longer recognizes any generation for that
// identity, so the edge is stranded and must be retracted.
//
// Keying on the full identity tuple — not acceptance_unit_id alone — is required
// for correctness: the same acceptance unit can appear under different scopes or
// source runs with different authoritative generations, and collapsing those
// siblings would classify an edge against the wrong generation, producing a
// false retract or a missed drift.
//
// The classifier is deterministic and side-effect free. Callers record the
// returned report's counts to telemetry via RecordReconciliationConvergence and
// drive a retract over RepairAnchors to converge the graph.
func ClassifyReconciliationDrift(
	authoritative []AuthoritativePostgresGeneration,
	edges []GraphDenormalizedEdge,
) ReconciliationReport {
	byIdentity := make(map[AcceptanceIdentity]AuthoritativePostgresGeneration, len(authoritative))
	for _, view := range authoritative {
		byIdentity[view.Identity] = view
	}

	report := ReconciliationReport{
		Findings: make([]ReconciliationFinding, 0, len(edges)),
		Counts:   make(map[ReconciliationDriftClass]int),
	}
	for _, edge := range edges {
		class := classifyEdge(byIdentity, edge)
		report.Findings = append(report.Findings, ReconciliationFinding{Edge: edge, Class: class})
		report.Counts[class]++
	}
	return report
}

func classifyEdge(
	byIdentity map[AcceptanceIdentity]AuthoritativePostgresGeneration,
	edge GraphDenormalizedEdge,
) ReconciliationDriftClass {
	view, ok := byIdentity[edge.Identity]
	if !ok {
		// Postgres recognizes no authoritative generation for this exact identity:
		// the edge is stranded from a slice that has been fully retired.
		return ReconciliationDriftStaleGeneration
	}
	if edge.GenerationID != view.GenerationID {
		return ReconciliationDriftStaleGeneration
	}
	if edge.ResolvedID != "" {
		if _, present := view.ResolvedIDs[edge.ResolvedID]; !present {
			return ReconciliationDriftOrphanResolvedID
		}
	}
	return ReconciliationDriftInSync
}

const reconciliationDriftDomainDualWrite = "dual_write"

// RecordReconciliationConvergence records the per-class drift counts from one
// reconciliation pass to the operator-facing convergence counter. Recording the
// full report — including in_sync — lets an operator confirm at 3 AM that a
// reconciliation pass ran and converged (all in_sync, no drift) rather than
// inferring health from the absence of a drift metric.
func RecordReconciliationConvergence(
	ctx context.Context,
	instruments *telemetry.Instruments,
	report ReconciliationReport,
) {
	if instruments == nil || instruments.ReconciliationConvergence == nil {
		return
	}
	for _, class := range reconciliationDriftClasses() {
		count := report.Counts[class]
		if count <= 0 {
			continue
		}
		instruments.ReconciliationConvergence.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrDomain(reconciliationDriftDomainDualWrite),
			telemetry.AttrDriftKind(string(class)),
		))
	}
}

// reconciliationDriftClasses returns the closed set of drift classes in a stable
// order so convergence recording stays deterministic.
func reconciliationDriftClasses() []ReconciliationDriftClass {
	return []ReconciliationDriftClass{
		ReconciliationDriftInSync,
		ReconciliationDriftStaleGeneration,
		ReconciliationDriftOrphanResolvedID,
	}
}

// reconciliationDriftSampleLimit bounds the number of drifted edge keys logged
// per domain so an operator log line stays readable and cheap regardless of how
// large a drift the pass detected.
const reconciliationDriftSampleLimit = 10

// LogReconciliationReport emits one operator-facing structured log line for a
// reconciliation pass: the per-class counts, whether the graph converged, and a
// bounded sample of the drifted edge keys an operator can use to investigate at
// 3 AM. A converged pass logs at info; a pass that found stranded edges logs at
// warn so it surfaces in alert pipelines. It is a no-op when logger is nil.
func LogReconciliationReport(logger *slog.Logger, report ReconciliationReport) {
	if logger == nil {
		return
	}
	attrs := []any{
		"domain", reconciliationDriftDomainDualWrite,
		"converged", report.Converged(),
		"in_sync", report.Counts[ReconciliationDriftInSync],
		"stale_generation", report.Counts[ReconciliationDriftStaleGeneration],
		"orphan_resolved_id", report.Counts[ReconciliationDriftOrphanResolvedID],
	}
	if drifted := boundedDriftedSample(report); len(drifted) > 0 {
		attrs = append(attrs, "drifted_edge_sample", drifted)
	}
	if report.Converged() {
		logger.Info("dual-write reconciliation converged", attrs...)
		return
	}
	logger.Warn("dual-write reconciliation detected stranded edges", attrs...)
}

// boundedDriftedSample returns a bounded, deterministic sample of drifted edge
// keys per domain for operator logs.
func boundedDriftedSample(report ReconciliationReport) map[string][]string {
	drifted := report.DriftedEdgeKeys()
	if len(drifted) == 0 {
		return nil
	}
	sample := make(map[string][]string, len(drifted))
	for domain, keys := range drifted {
		if len(keys) > reconciliationDriftSampleLimit {
			keys = keys[:reconciliationDriftSampleLimit]
		}
		sample[domain] = keys
	}
	return sample
}
