// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// crossplaneSatisfiedByMaterializationDomainDefinition returns the additive
// definition for the Crossplane Claim -> XRD SATISFIED_BY edge projection. It
// is additive (not part of DefaultDomainDefinitions) because the handler
// requires an explicitly wired CrossplaneSatisfiedByEdgeWriter and
// FactLoader; registering it without them would silently drop every intent.
// Mirrors kubernetesCorrelationMaterializationDomainDefinition. See issue
// #5347.
func crossplaneSatisfiedByMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainCrossplaneSatisfiedByMaterialization,
		Summary: "project Crossplane Claim -> XRD classification decisions into canonical SATISFIED_BY graph edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "crossplane_satisfied_by_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// crossplaneSatisfiedByEdgeEvidenceSource tags SATISFIED_BY edges written by
// this reducer so the prior-generation retract path scopes its delete to
// reducer-owned Crossplane classification edges and never touches edges
// owned by another writer. Must match
// cypher.crossplaneSatisfiedByEvidenceSource byte-for-byte (the two packages
// each own their own constant, mirroring
// kubernetesCorrelationEdgeEvidenceSource).
const crossplaneSatisfiedByEdgeEvidenceSource = "reducer/crossplane-satisfied-by"

// CrossplaneSatisfiedByEdgeWriter persists and retracts canonical
// SATISFIED_BY edges between a K8sResource node (the Claim) and the
// CrossplaneXRD node it resolved against. Implementations MUST be idempotent
// by (claim uid, SATISFIED_BY, xrd uid) so reducer retries and duplicate
// facts converge on one edge, and MUST NOT fabricate endpoint nodes: a row
// whose claim or XRD node is absent is a no-op. Mirrors
// KubernetesCorrelationEdgeWriter.
type CrossplaneSatisfiedByEdgeWriter interface {
	WriteCrossplaneSatisfiedByEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractCrossplaneSatisfiedByEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// crossplaneXRDFactLoader loads the cross-scope active CrossplaneXRD
// content-entity facts a Claim candidate resolves against. XRDs commonly live
// in a platform repo separate from the Claims that reference them, so they
// are loaded across scopes exactly like kubernetesCorrelationSourceFactLoader
// loads cross-scope OCI source evidence.
type crossplaneXRDFactLoader interface {
	ListActiveCrossplaneXRDFacts(ctx context.Context) ([]facts.Envelope, error)
}

// CrossplaneSatisfiedByMaterializationHandler reduces one Crossplane
// classification materialization intent into canonical SATISFIED_BY edge
// writes. It loads the intent's own scope generation's content_entity facts
// (Claim candidates: K8sResource rows) plus the cross-scope active
// CrossplaneXRD facts, resolves each candidate's (group, kind) against
// exactly one XRD's (spec.group, spec.claimNames.kind), and hands the
// resulting batch to the edge writer. A zero-match candidate is an ordinary
// Kubernetes object and an ambiguous (2+ match) candidate stays
// provenance-only; neither fabricates an edge.
//
// Deviation from the locked design's dual-keyspace readiness gate (documented
// for orchestrator review): this handler does NOT gate on a
// canonical-nodes-committed phase before resolving. The
// GraphProjectionKeyspaceCodeEntitiesUID phase that would need to cover both
// the Claim's own repo and the XRD's (commonly different) repo is published
// with each repo's real collector-sync-cycle source_run_id
// (internal/projector/runtime_phase.go's appendCanonicalRepositoryGraphPhase),
// not the generation_id graphProjectionPhaseStateForIntent substitutes for
// every other keyspace's readiness check. Reconstructing the correct
// cross-repo phase key from this handler is not achievable with the existing
// primitives without risking a permanently-mismatched, always-not-ready gate
// (a stuck intent) — worse than the gap it would close. Safety is preserved
// by construction instead: the writer's MATCH-MATCH-MERGE only ever produces
// an edge when both endpoint nodes already exist in the graph, so an
// uncommitted endpoint yields no edge this generation (self-healing on a
// later retry) rather than a fabricated one.
type CrossplaneSatisfiedByMaterializationHandler struct {
	FactLoader  FactLoader
	EdgeWriter  CrossplaneSatisfiedByEdgeWriter
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	// PriorGenerationCheck reports whether the scope has any prior
	// generation. Nil keeps retract behavior conservative (always retract
	// before write).
	PriorGenerationCheck PriorGenerationCheck
}

// Handle executes one Crossplane SATISFIED_BY materialization intent.
func (h CrossplaneSatisfiedByMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainCrossplaneSatisfiedByMaterialization {
		return Result{}, fmt.Errorf(
			"crossplane satisfied-by materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("crossplane satisfied-by materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("crossplane satisfied-by materialization edge writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerCrossplaneSatisfiedByMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	loadStart := time.Now()
	envelopes, err := h.loadEdgeFacts(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	rows, tally, err := ExtractCrossplaneSatisfiedByEdgeRows(envelopes)
	if err != nil {
		return Result{}, err
	}
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.EdgeWriter.RetractCrossplaneSatisfiedByEdges(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			crossplaneSatisfiedByEdgeEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical crossplane satisfied-by edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.EdgeWriter.WriteCrossplaneSatisfiedByEdges(
			ctx,
			rows,
			intent.ScopeID,
			intent.GenerationID,
			crossplaneSatisfiedByEdgeEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical crossplane satisfied-by edges: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	h.recordEdgeCounter(ctx, rows)
	logCrossplaneSatisfiedByMaterializationCompleted(ctx, crossplaneSatisfiedByMaterializationTiming{
		intent:           intent,
		factCount:        len(envelopes),
		edgeCount:        len(rows),
		ambiguousSkipped: tally.ambiguousSkipped,
		skipRetract:      skipRetract,
		loadDuration:     loadDuration,
		extractDuration:  extractDuration,
		retractDuration:  retractDuration,
		writeDuration:    writeDuration,
		totalDuration:    time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainCrossplaneSatisfiedByMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d SATISFIED_BY edge(s) from %d fact(s); %d candidate(s) matched 2+ XRDs (ambiguous, skipped)",
			len(rows),
			len(envelopes),
			tally.ambiguousSkipped,
		),
		CanonicalWrites: len(rows),
	}, nil
}

// loadEdgeFacts loads the intent's own scope generation's content_entity
// facts (Claim candidates) and appends the cross-scope active CrossplaneXRD
// facts, mirroring KubernetesCorrelationMaterializationHandler.loadEdgeFacts.
func (h CrossplaneSatisfiedByMaterializationHandler) loadEdgeFacts(
	ctx context.Context,
	intent Intent,
) ([]facts.Envelope, error) {
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{factKindContentEntity},
	)
	if err != nil {
		return nil, fmt.Errorf("load facts for crossplane satisfied-by materialization: %w", err)
	}
	xrdFacts, err := h.loadActiveXRDFacts(ctx)
	if err != nil {
		return nil, fmt.Errorf("load active crossplane xrd facts: %w", err)
	}
	return append(envelopes, xrdFacts...), nil
}

// loadActiveXRDFacts loads the cross-scope active CrossplaneXRD facts through
// the optional crossplaneXRDFactLoader interface. A loader that does not
// implement it yields no XRD facts (and therefore no edges), never an error.
func (h CrossplaneSatisfiedByMaterializationHandler) loadActiveXRDFacts(ctx context.Context) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(crossplaneXRDFactLoader)
	if !ok {
		return nil, nil
	}
	envelopes, err := loader.ListActiveCrossplaneXRDFacts(ctx)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

// shouldSkipRetract mirrors KubernetesCorrelationMaterializationHandler:
// skip the prior-edge retract on the very first generation for a scope (no
// prior edges to remove) and only on the first attempt.
func (h CrossplaneSatisfiedByMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for crossplane satisfied-by retract: %w", err)
	}
	return !hasPrior, nil
}

// recordEdgeCounter emits the SATISFIED_BY edge-projection counter
// dimensioned by resolution_mode, mirroring
// KubernetesCorrelationMaterializationHandler.recordEdgeCounter.
func (h CrossplaneSatisfiedByMaterializationHandler) recordEdgeCounter(
	ctx context.Context,
	rows []map[string]any,
) {
	if h.Instruments == nil || h.Instruments.CrossplaneSatisfiedByEdges == nil {
		return
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[anyToString(row["resolution_mode"])]++
	}
	for mode, count := range counts {
		h.Instruments.CrossplaneSatisfiedByEdges.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrResolutionMode(mode),
		))
	}
}

type crossplaneSatisfiedByMaterializationTiming struct {
	intent           Intent
	factCount        int
	edgeCount        int
	ambiguousSkipped int
	skipRetract      bool
	loadDuration     time.Duration
	extractDuration  time.Duration
	retractDuration  time.Duration
	writeDuration    time.Duration
	totalDuration    time.Duration
}

func logCrossplaneSatisfiedByMaterializationCompleted(
	ctx context.Context,
	timing crossplaneSatisfiedByMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "crossplane satisfied-by materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("fact_count", timing.factCount),
		slog.Int("edge_count", timing.edgeCount),
		slog.Int("ambiguous_skipped", timing.ambiguousSkipped),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("classify_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
