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

// kubernetesCorrelationMaterializationDomainDefinition returns the additive
// definition for the live-workload RUNS_IMAGE edge projection. It is additive
// (not part of DefaultDomainDefinitions) because the handler requires an
// explicitly wired KubernetesCorrelationEdgeWriter and FactLoader; registering it
// without them would silently drop every intent. It mirrors
// awsRelationshipMaterializationDomainDefinition (#805 PR2) and
// observabilityCoverageMaterializationDomainDefinition (#391 PR3). See issue #388
// PR3 and docs/internal/design/388-kubernetes-correlation-readmodel.md.
func kubernetesCorrelationMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainKubernetesCorrelationMaterialization,
		Summary: "project exact live Kubernetes correlation decisions into canonical RUNS_IMAGE graph edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "kubernetes_correlation_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// kubernetesCorrelationEdgeEvidenceSource tags RUNS_IMAGE edges written by this
// reducer so the prior-generation retract path scopes its delete to reducer-owned
// live-workload image edges and never touches edges owned by other writers.
const kubernetesCorrelationEdgeEvidenceSource = "reducer/kubernetes-correlation"

// KubernetesCorrelationEdgeWriter persists and retracts canonical RUNS_IMAGE
// edges between a live KubernetesWorkload node and the digest-addressed OCI source
// node it was observed running. Implementations MUST be idempotent by
// (workload uid, RUNS_IMAGE, source uid) so reducer retries and duplicate facts
// converge on one edge, and MUST NOT fabricate endpoint nodes: a row whose
// workload or source node is absent is a no-op.
type KubernetesCorrelationEdgeWriter interface {
	WriteKubernetesCorrelationEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractKubernetesCorrelationEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// KubernetesCorrelationMaterializationHandler reduces one live-workload
// correlation materialization follow-up into canonical RUNS_IMAGE edge writes. It
// gates on the GraphProjectionPhaseCanonicalNodesCommitted readiness phase that
// DomainKubernetesWorkloadMaterialization (#388 node slice) publishes on the
// KubernetesWorkload keyspace, so edges never resolve against a generation whose
// workload nodes have not committed. It then loads the cluster scope generation's
// kubernetes_live.* facts plus the cross-scope active deployment-source image
// facts, re-runs the same pure classifier the PR1 read model uses, extracts only
// the exact image decisions that resolved both a workload node uid and a
// digest-addressed OCI source node uid into a bounded batch (no per-edge graph
// round trip), and hands the batch to the edge writer. Provenance-only
// correlation (derived/ambiguous/unresolved/stale/rejected) and the structural
// owner_reference identity decision fabricate no edge; an exact image decision
// whose source digest resolves no canonical node is counted skipped, never
// dropped silently and never written as a dangling edge.
//
// See issue #388 PR3 and
// docs/internal/design/388-kubernetes-correlation-readmodel.md.
type KubernetesCorrelationMaterializationHandler struct {
	FactLoader FactLoader
	EdgeWriter KubernetesCorrelationEdgeWriter
	// ReadinessLookup reports whether the canonical-nodes-committed phase has been
	// published for the intent's scope generation. A nil lookup keeps the gate open
	// (test wiring); production wires the durable Postgres lookup.
	ReadinessLookup GraphProjectionReadinessLookup
	// PriorGenerationCheck reports whether the scope has any prior generation. Nil
	// keeps retract behavior conservative (always retract before write).
	PriorGenerationCheck PriorGenerationCheck
	Tracer               trace.Tracer
	Instruments          *telemetry.Instruments
}

// Handle executes one live-workload correlation materialization intent.
func (h KubernetesCorrelationMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainKubernetesCorrelationMaterialization {
		return Result{}, fmt.Errorf(
			"kubernetes correlation materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("kubernetes correlation materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("kubernetes correlation materialization edge writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerKubernetesCorrelationMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	// Readiness gate: edges may only resolve against workload nodes the same
	// generation already committed. If the canonical-nodes phase is not yet
	// published, the intent re-enters the durable queue (retryable) rather than
	// writing edges against a node set that does not exist yet.
	if !h.workloadNodesReady(intent) {
		return Result{}, kubernetesCorrelationNodesNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
		}
	}

	loadStart := time.Now()
	envelopes, err := h.loadEdgeFacts(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	rows, tally := ExtractKubernetesCorrelationEdgeRows(envelopes)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.EdgeWriter.RetractKubernetesCorrelationEdges(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			kubernetesCorrelationEdgeEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical kubernetes correlation edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.EdgeWriter.WriteKubernetesCorrelationEdges(
			ctx,
			rows,
			intent.ScopeID,
			intent.GenerationID,
			kubernetesCorrelationEdgeEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical kubernetes correlation edges: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	h.recordEdgeCounter(ctx, rows)
	logKubernetesCorrelationMaterializationCompleted(ctx, kubernetesCorrelationMaterializationTiming{
		intent:          intent,
		factCount:       len(envelopes),
		edgeCount:       len(rows),
		skipped:         tally.totalSkipped(),
		skipRetract:     skipRetract,
		loadDuration:    loadDuration,
		extractDuration: extractDuration,
		retractDuration: retractDuration,
		writeDuration:   writeDuration,
		totalDuration:   time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainKubernetesCorrelationMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d RUNS_IMAGE edge(s) from %d fact(s); %d exact decision(s) had no resolvable source node",
			len(rows),
			len(envelopes),
			tally.totalSkipped(),
		),
		CanonicalWrites: len(rows),
	}, nil
}

// loadEdgeFacts loads the cluster scope generation's kubernetes_live.* facts and
// appends the cross-scope active deployment-source image facts, the exact same
// substrate the PR1 correlation read model classifies. Loading both is required
// because the edge extractor re-runs the classifier (which needs both sides) and
// builds the digest->uid source index from the OCI source facts.
func (h KubernetesCorrelationMaterializationHandler) loadEdgeFacts(
	ctx context.Context,
	intent Intent,
) ([]facts.Envelope, error) {
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		kubernetesCorrelationFactKinds(),
	)
	if err != nil {
		return nil, fmt.Errorf("load facts for kubernetes correlation materialization: %w", err)
	}
	sourceFacts, err := h.loadActiveSourceFacts(ctx)
	if err != nil {
		return nil, fmt.Errorf("load active kubernetes correlation source facts: %w", err)
	}
	return append(envelopes, sourceFacts...), nil
}

// loadActiveSourceFacts loads the cross-scope active deployment-source image facts
// through the optional ListActiveContainerImageIdentityFacts loader, mirroring the
// PR1 correlation handler. A loader that does not implement the interface yields
// no source facts (and therefore no exact digest edges), never an error.
func (h KubernetesCorrelationMaterializationHandler) loadActiveSourceFacts(ctx context.Context) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(kubernetesCorrelationSourceFactLoader)
	if !ok {
		return nil, nil
	}
	envelopes, err := loader.ListActiveContainerImageIdentityFacts(ctx)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

// workloadNodesReady reports whether the #388 node slice's
// canonical-nodes-committed phase is published for this intent's scope
// generation. The phase key is derived the same way
// DomainKubernetesWorkloadMaterialization publishes it on the
// GraphProjectionKeyspaceKubernetesWorkloadUID keyspace, so the lookup matches the
// published row. A nil lookup keeps the gate open for test wiring.
func (h KubernetesCorrelationMaterializationHandler) workloadNodesReady(intent Intent) bool {
	if h.ReadinessLookup == nil {
		return true
	}
	state, ok := graphProjectionPhaseStateForIntent(
		intent,
		GraphProjectionKeyspaceKubernetesWorkloadUID,
		GraphProjectionPhaseCanonicalNodesCommitted,
		time.Now().UTC(),
	)
	if !ok {
		return false
	}
	ready, found := h.ReadinessLookup(state.Key, GraphProjectionPhaseCanonicalNodesCommitted)
	return found && ready
}

// shouldSkipRetract mirrors the AWS relationship and observability coverage
// domains: skip the prior-edge retract on the very first generation for a scope
// (no prior edges to remove) and only on the first attempt, so a retried attempt
// still cleans up a partial prior write.
func (h KubernetesCorrelationMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for kubernetes correlation retract: %w", err)
	}
	return !hasPrior, nil
}

// recordEdgeCounter emits the RUNS_IMAGE edge-projection counter dimensioned by
// resolution_mode (digest — the only edge-eligible exact join), the contract
// registered for eshu_dp_kubernetes_correlation_edges_total. The skipped tally
// (exact decisions with no resolvable source node) goes to the completion log, not
// a metric label, so cardinality stays bounded.
func (h KubernetesCorrelationMaterializationHandler) recordEdgeCounter(
	ctx context.Context,
	rows []map[string]any,
) {
	if h.Instruments == nil || h.Instruments.KubernetesCorrelationEdges == nil {
		return
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[anyToString(row["resolution_mode"])]++
	}
	for mode, count := range counts {
		h.Instruments.KubernetesCorrelationEdges.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrResolutionMode(mode),
		))
	}
}

// kubernetesCorrelationNodesNotReadyError marks the readiness-gate miss as
// retryable so the durable queue re-runs the intent once the #388 node slice's
// workload nodes commit, instead of failing terminally or writing edges against
// absent nodes.
type kubernetesCorrelationNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e kubernetesCorrelationNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical kubernetes workload nodes not committed for scope %s generation %s",
		e.scopeID,
		e.generationID,
	)
}

func (kubernetesCorrelationNodesNotReadyError) Retryable() bool { return true }

func (kubernetesCorrelationNodesNotReadyError) FailureClass() string {
	return "kubernetes_correlation_nodes_not_ready"
}

// kubernetesCorrelationMaterializationTiming groups stage durations and the edge
// counts so the completion log identifies fact-load, classify+resolve, retract,
// and graph-write time, plus how many exact decisions could not anchor a source
// node.
type kubernetesCorrelationMaterializationTiming struct {
	intent          Intent
	factCount       int
	edgeCount       int
	skipped         int
	skipRetract     bool
	loadDuration    time.Duration
	extractDuration time.Duration
	retractDuration time.Duration
	writeDuration   time.Duration
	totalDuration   time.Duration
}

func logKubernetesCorrelationMaterializationCompleted(
	ctx context.Context,
	timing kubernetesCorrelationMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "kubernetes correlation materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("fact_count", timing.factCount),
		slog.Int("edge_count", timing.edgeCount),
		slog.Int("skipped_unresolvable_source", timing.skipped),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("classify_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
