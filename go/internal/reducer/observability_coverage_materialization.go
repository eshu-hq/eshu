// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// observabilityCoverageMaterializationDomainDefinition returns the additive
// definition for observability COVERS edge projection. It is additive (not part
// of DefaultDomainDefinitions) because the handler requires an explicitly wired
// ObservabilityCoverageEdgeWriter and FactLoader; registering it without them
// would silently drop every intent. It mirrors
// awsRelationshipMaterializationDomainDefinition (#805 PR2). See issue #391 PR3
// and docs/internal/design/391-observability-coverage-correlation.md §6.
func observabilityCoverageMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainObservabilityCoverageMaterialization,
		Summary: "project exact observability coverage decisions into canonical COVERS graph edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "observability_coverage_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// observabilityCoverageEvidenceSource tags COVERS edges written by this reducer
// so the prior-generation retract path scopes its delete to reducer-owned
// coverage edges and never touches edges owned by other writers.
const observabilityCoverageEvidenceSource = "reducer/observability-coverage"

// ObservabilityCoverageEdgeWriter persists and retracts canonical COVERS edges
// between an observability CloudResource node and the monitored CloudResource it
// covers. Implementations MUST be idempotent by (observability uid,
// coverage_signal, target uid) so reducer retries and duplicate facts converge
// on one edge, and MUST NOT fabricate endpoint nodes: a row whose observability
// or target node is absent is a no-op.
type ObservabilityCoverageEdgeWriter interface {
	WriteObservabilityCoverageEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractObservabilityCoverageEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// ObservabilityCoverageMaterializationHandler reduces one observability coverage
// materialization follow-up into canonical COVERS edge writes. It gates on the
// GraphProjectionPhaseCanonicalNodesCommitted readiness phase that
// DomainAWSResourceMaterialization (#805 PR1) publishes on the CloudResource
// keyspace, so coverage edges never resolve against a generation whose nodes
// have not committed. It then loads the scope generation's aws_resource and
// aws_relationship facts, re-runs the same pure classifier the PR1 read model
// uses, extracts only the exact-coverage decisions that resolved a target
// CloudResource uid into a bounded batch (no per-edge graph round trip), and
// hands the batch to the edge writer. Provenance-only coverage (derived /
// ambiguous / unresolved / stale / rejected) fabricates no edge; X-Ray derived
// coverage that resolves no target uid is counted skipped, never dropped
// silently.
//
// See issue #391 PR3 and
// docs/internal/design/391-observability-coverage-correlation.md §6.
type ObservabilityCoverageMaterializationHandler struct {
	FactLoader FactLoader
	EdgeWriter ObservabilityCoverageEdgeWriter
	// ReadinessLookup reports whether the canonical-nodes-committed phase has
	// been published for the intent's scope generation. A nil lookup keeps the
	// gate open (test wiring); production wires the durable Postgres lookup.
	ReadinessLookup GraphProjectionReadinessLookup
	// PriorGenerationCheck reports whether the scope has any prior generation.
	// Nil keeps retract behavior conservative (always retract before write).
	PriorGenerationCheck PriorGenerationCheck
	Tracer               trace.Tracer
	Instruments          *telemetry.Instruments
}

// Handle executes one observability coverage materialization intent.
func (h ObservabilityCoverageMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainObservabilityCoverageMaterialization {
		return Result{}, fmt.Errorf(
			"observability coverage materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("observability coverage materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("observability coverage materialization edge writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerObservabilityCoverageMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	// Readiness gate: coverage edges may only resolve against nodes the same
	// generation already committed. If the canonical-nodes phase is not yet
	// published, the intent re-enters the durable queue (retryable) rather than
	// writing edges against a node set that does not exist yet.
	if !h.canonicalNodesReady(intent) {
		return Result{}, observabilityCoverageNodesNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
		}
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		observabilityCoverageCorrelationFactKinds(),
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for observability coverage materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	rows, tally, err := ExtractObservabilityCoverageEdgeRows(envelopes)
	if err != nil {
		// A malformed aws_resource/aws_relationship payload (a missing required
		// identity field) is a classified input_invalid decode failure;
		// dead-letter the intent instead of writing a coverage edge against an
		// empty-string node identity.
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
		if err := h.EdgeWriter.RetractObservabilityCoverageEdges(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			observabilityCoverageEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical observability coverage edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.EdgeWriter.WriteObservabilityCoverageEdges(
			ctx,
			rows,
			intent.ScopeID,
			intent.GenerationID,
			observabilityCoverageEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical observability coverage edges: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	h.recordEdgeCounter(ctx, rows)
	logObservabilityCoverageMaterializationCompleted(ctx, observabilityCoverageMaterializationTiming{
		intent:          intent,
		factCount:       len(envelopes),
		edgeCount:       len(rows),
		materialized:    tally.materialized,
		skipped:         tally.skipped,
		skipRetract:     skipRetract,
		loadDuration:    loadDuration,
		extractDuration: extractDuration,
		retractDuration: retractDuration,
		writeDuration:   writeDuration,
		totalDuration:   time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainObservabilityCoverageMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d observability COVERS edge(s) from %d fact(s); %d derived coverage(s) had no target node",
			len(rows),
			len(envelopes),
			tally.totalSkipped(),
		),
		CanonicalWrites: len(rows),
	}, nil
}

// canonicalNodesReady reports whether the #805 PR1 canonical-nodes-committed
// phase is published for this intent's scope generation. The phase key is
// derived the same way DomainAWSResourceMaterialization publishes it, so the
// lookup matches the published row. A nil lookup keeps the gate open for test
// wiring.
func (h ObservabilityCoverageMaterializationHandler) canonicalNodesReady(intent Intent) bool {
	if h.ReadinessLookup == nil {
		return true
	}
	state, ok := graphProjectionPhaseStateForIntent(
		intent,
		GraphProjectionKeyspaceCloudResourceUID,
		GraphProjectionPhaseCanonicalNodesCommitted,
		time.Now().UTC(),
	)
	if !ok {
		return false
	}
	ready, found := h.ReadinessLookup(state.Key, GraphProjectionPhaseCanonicalNodesCommitted)
	return found && ready
}

// shouldSkipRetract mirrors the AWS relationship domain: skip the prior-edge
// retract on the very first generation for a scope (no prior edges to remove)
// and only on the first attempt, so a retried attempt still cleans up a partial
// prior write.
func (h ObservabilityCoverageMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for observability coverage retract: %w", err)
	}
	return !hasPrior, nil
}

// recordEdgeCounter emits the COVERS edge-projection counter dimensioned by
// (coverage_signal, resolution_mode), the contract registered for
// eshu_dp_observability_coverage_edges_total. Each materialized edge row carries
// its coverage signal and the resolution mode that proved it; the skipped tally
// (derived coverage with no target node) goes to the completion log, not a
// metric label, so cardinality stays bounded.
func (h ObservabilityCoverageMaterializationHandler) recordEdgeCounter(
	ctx context.Context,
	rows []map[string]any,
) {
	if h.Instruments == nil || h.Instruments.ObservabilityCoverageEdges == nil {
		return
	}
	type signalMode struct {
		signal string
		mode   string
	}
	counts := make(map[signalMode]int, len(rows))
	for _, row := range rows {
		counts[signalMode{anyToString(row["coverage_signal"]), anyToString(row["resolution_mode"])}]++
	}
	for key, count := range counts {
		h.Instruments.ObservabilityCoverageEdges.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrCoverageSignal(key.signal),
			telemetry.AttrResolutionMode(key.mode),
		))
	}
}

// totalSkipped returns the count of derived coverage decisions that resolved no
// target CloudResource and therefore produced no edge.
func (t observabilityCoverageEdgeTally) totalSkipped() int {
	total := 0
	for _, count := range t.skipped {
		total += count
	}
	return total
}

// observabilityCoverageNodesNotReadyError marks the readiness-gate miss as
// retryable so the durable queue re-runs the intent once #805 PR1 nodes commit,
// instead of failing terminally or writing edges against absent nodes.
type observabilityCoverageNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e observabilityCoverageNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical cloud resource nodes not committed for scope %s generation %s",
		e.scopeID,
		e.generationID,
	)
}

func (observabilityCoverageNodesNotReadyError) Retryable() bool { return true }

func (observabilityCoverageNodesNotReadyError) FailureClass() string {
	return "observability_coverage_nodes_not_ready"
}

// observabilityCoverageMaterializationTiming groups stage durations and the
// edge tally so the completion log identifies fact-load, classify, retract, and
// graph-write time, plus which coverage signals materialized or were skipped.
type observabilityCoverageMaterializationTiming struct {
	intent          Intent
	factCount       int
	edgeCount       int
	materialized    map[string]int
	skipped         map[string]int
	skipRetract     bool
	loadDuration    time.Duration
	extractDuration time.Duration
	retractDuration time.Duration
	writeDuration   time.Duration
	totalDuration   time.Duration
}

func logObservabilityCoverageMaterializationCompleted(
	ctx context.Context,
	timing observabilityCoverageMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "observability coverage materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("fact_count", timing.factCount),
		slog.Int("edge_count", timing.edgeCount),
		slog.String("materialized_by_signal", formatCoverageTally(timing.materialized)),
		slog.String("skipped_derived_by_signal", formatCoverageTally(timing.skipped)),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("classify_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}

// formatCoverageTally renders a bounded count map as a deterministic key=value
// string for the structured completion log. Cardinality is bounded by the closed
// set of coverage signals.
func formatCoverageTally(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ",")
}
