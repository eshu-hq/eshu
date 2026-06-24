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
)

// gcpRelationshipMaterializationDomainDefinition returns the additive definition
// for GCP relationship edge projection. It is additive (not part of
// DefaultDomainDefinitions) because the handler requires an explicitly wired GCP
// edge writer and FactLoader; registering it without them would silently drop
// every intent. See docs/internal/gcp-cloud-relationship-edge-materialization-design.md.
func gcpRelationshipMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainGCPRelationshipMaterialization,
		Summary: "project gcp_cloud_relationship facts into canonical GCP relationship graph edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "gcp_relationship_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// gcpRelationshipEvidenceSource tags GCP relationship edges written by this
// reducer so the prior-generation retract path scopes its delete to
// reducer-owned GCP relationship edges and never touches edges owned by other
// writers. It is distinct from awsRelationshipEvidenceSource.
const gcpRelationshipEvidenceSource = "reducer/gcp-relationships"

// GCPRelationshipMaterializationHandler reduces one GCP relationship
// materialization follow-up into canonical GCP relationship edge writes. It
// gates on the GraphProjectionPhaseCanonicalNodesCommitted readiness phase that
// DomainGCPResourceMaterialization publishes on the CloudResource keyspace under
// the gcp_resource_materialization:<scope> acceptance unit, so edges never
// resolve against a generation whose GCP nodes have not committed. It then loads
// the scope generation's gcp_cloud_resource and gcp_cloud_relationship facts,
// resolves both endpoints to CloudResource uids through a bounded in-memory join
// index keyed by full resource name (no per-edge graph round trip), and hands
// the resolved batch to the edge writer. Unresolved, partial, and unsupported
// relationships are counted and logged, never written and never dropped
// silently.
//
// See docs/internal/gcp-cloud-relationship-edge-materialization-design.md.
type GCPRelationshipMaterializationHandler struct {
	FactLoader FactLoader
	EdgeWriter CloudResourceEdgeWriter
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

// Handle executes one GCP relationship materialization intent.
func (h GCPRelationshipMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainGCPRelationshipMaterialization {
		return Result{}, fmt.Errorf(
			"gcp relationship materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("gcp relationship materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("gcp relationship materialization edge writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerGCPRelationshipMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	// Readiness gate: edges may only resolve against nodes the same generation
	// already committed. If the canonical-nodes phase is not yet published, the
	// intent re-enters the durable queue (retryable) rather than writing edges
	// against a node set that does not exist yet.
	if !h.canonicalNodesReady(intent) {
		return Result{}, gcpRelationshipNodesNotReadyError{
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
		[]string{facts.GCPCloudResourceFactKind, facts.GCPCloudRelationshipFactKind},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for gcp relationship materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, relationshipEnvelopes := splitGCPFactEnvelopes(envelopes)

	extractStart := time.Now()
	rows, tally := ExtractGCPRelationshipEdgeRows(resourceEnvelopes, relationshipEnvelopes)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.EdgeWriter.RetractCloudResourceEdges(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			gcpRelationshipEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical gcp relationship edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.EdgeWriter.WriteCloudResourceEdges(ctx, rows, intent.ScopeID, intent.GenerationID, gcpRelationshipEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical gcp relationship edges: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	h.recordTally(ctx, tally)
	timing := gcpRelationshipMaterializationTiming{
		intent:            intent,
		resourceFactCount: len(resourceEnvelopes),
		relationshipCount: len(relationshipEnvelopes),
		edgeCount:         len(rows),
		tally:             tally,
		skipRetract:       skipRetract,
		loadDuration:      loadDuration,
		extractDuration:   extractDuration,
		retractDuration:   retractDuration,
		writeDuration:     writeDuration,
		totalDuration:     time.Since(totalStart),
	}
	logGCPRelationshipMaterializationCompleted(ctx, timing)
	h.recordMetrics(ctx, timing)

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainGCPRelationshipMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d gcp relationship edge(s) from %d relationship fact(s); %d skipped",
			len(rows),
			len(relationshipEnvelopes),
			tally.skippedCount(),
		),
		CanonicalWrites: len(rows),
	}, nil
}

func (h GCPRelationshipMaterializationHandler) recordMetrics(
	ctx context.Context,
	timing gcpRelationshipMaterializationTiming,
) {
	if h.Instruments == nil {
		return
	}
	recordGCPMaterializationFact(ctx, h.Instruments, DomainGCPRelationshipMaterialization, facts.GCPCloudResourceFactKind, timing.resourceFactCount)
	recordGCPMaterializationFact(ctx, h.Instruments, DomainGCPRelationshipMaterialization, facts.GCPCloudRelationshipFactKind, timing.relationshipCount)
	recordGCPMaterializationGraphWrites(ctx, h.Instruments, DomainGCPRelationshipMaterialization, "edge", timing.edgeCount)
	recordGCPMaterializationDuration(ctx, h.Instruments, DomainGCPRelationshipMaterialization, "load_facts", timing.loadDuration)
	recordGCPMaterializationDuration(ctx, h.Instruments, DomainGCPRelationshipMaterialization, "extract", timing.extractDuration)
	if !timing.skipRetract {
		recordGCPMaterializationDuration(ctx, h.Instruments, DomainGCPRelationshipMaterialization, "retract", timing.retractDuration)
	}
	if timing.edgeCount > 0 {
		recordGCPMaterializationDuration(ctx, h.Instruments, DomainGCPRelationshipMaterialization, "graph_write", timing.writeDuration)
	}
	recordGCPMaterializationDuration(ctx, h.Instruments, DomainGCPRelationshipMaterialization, "total", timing.totalDuration)
}

// canonicalNodesReady reports whether the GCP node materialization
// canonical-nodes-committed phase is published for this intent's scope
// generation. The phase key is derived the same way DomainGCPResourceMaterialization
// publishes it (the shared gcp_resource_materialization:<scope> acceptance unit
// carried on the intent's entity key), so the lookup matches the published row.
// A nil lookup keeps the gate open for test wiring.
func (h GCPRelationshipMaterializationHandler) canonicalNodesReady(intent Intent) bool {
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
func (h GCPRelationshipMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for gcp relationship retract: %w", err)
	}
	return !hasPrior, nil
}

// recordTally emits the GCP edge-projection counter dimensioned by
// (relationship_type, join_mode), matching the AWS materialization telemetry
// shape. Every relationship fact lands in exactly one bounded mode bucket
// (full_resource_name for materialized edges; unresolved / partial / unsupported
// / invalid_type / empty_type / unknown_state for the reasons an edge was not
// written). Invalid or missing relationship types are represented by bounded
// sentinels, so the metric cardinality stays bounded and an operator can alert
// on the resolution-failure rate.
func (h GCPRelationshipMaterializationHandler) recordTally(ctx context.Context, tally gcpRelationshipEdgeTally) {
	if h.Instruments == nil || h.Instruments.GCPRelationshipEdges == nil {
		return
	}
	for key, count := range tally.byRelTypeMode {
		h.Instruments.GCPRelationshipEdges.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrRelationshipType(key.relationshipType),
			telemetry.AttrJoinMode(key.mode),
		))
	}
}

// splitGCPFactEnvelopes partitions a mixed envelope slice into resource and
// relationship facts in one pass so the join index and edge facts are built from
// a single bounded load.
func splitGCPFactEnvelopes(envelopes []facts.Envelope) (resources, relationships []facts.Envelope) {
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resources = append(resources, env)
		case facts.GCPCloudRelationshipFactKind:
			relationships = append(relationships, env)
		}
	}
	return resources, relationships
}

// gcpRelationshipNodesNotReadyError marks the readiness-gate miss as retryable
// so the durable queue re-runs the intent once GCP nodes commit, instead of
// failing terminally or writing edges against absent nodes.
type gcpRelationshipNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e gcpRelationshipNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical gcp cloud resource nodes not committed for scope %s generation %s",
		e.scopeID,
		e.generationID,
	)
}

func (gcpRelationshipNodesNotReadyError) Retryable() bool { return true }

func (gcpRelationshipNodesNotReadyError) FailureClass() string {
	return "gcp_relationship_nodes_not_ready"
}

// gcpRelationshipMaterializationTiming groups stage durations and the resolution
// tally so the completion log identifies fact-load, extraction, retract, and
// graph-write time, plus why relationships did not materialize.
type gcpRelationshipMaterializationTiming struct {
	intent            Intent
	resourceFactCount int
	relationshipCount int
	edgeCount         int
	tally             gcpRelationshipEdgeTally
	skipRetract       bool
	loadDuration      time.Duration
	extractDuration   time.Duration
	retractDuration   time.Duration
	writeDuration     time.Duration
	totalDuration     time.Duration
}

func logGCPRelationshipMaterializationCompleted(
	ctx context.Context,
	timing gcpRelationshipMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "gcp relationship materialization completed",
		slog.String(telemetry.LogKeyScopeID, timing.intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, timing.intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(timing.intent.Domain)),
		slog.Int("gcp_resource_fact_count", timing.resourceFactCount),
		slog.Int("relationship_fact_count", timing.relationshipCount),
		slog.Int("edge_count", timing.edgeCount),
		slog.Int("resolved_count", timing.tally.resolvedCount()),
		slog.Int("skipped_count", timing.tally.skippedCount()),
		slog.String("by_mode", formatTally(timing.tally.byMode)),
		slog.String("unresolved_target_by_type", formatTally(timing.tally.unresolved)),
		slog.String("unresolved_source_by_type", formatTally(timing.tally.unresolvedSource)),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
