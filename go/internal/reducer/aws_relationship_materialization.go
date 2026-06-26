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

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// awsRelationshipMaterializationDomainDefinition returns the additive
// definition for AWS relationship edge projection. It is additive (not part of
// DefaultDomainDefinitions) because the handler requires an explicitly wired
// CloudResourceEdgeWriter and FactLoader; registering it without them would
// silently drop every intent. See
// docs/internal/aws-relationship-edge-materialization-design.md §5–§8.
func awsRelationshipMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainAWSRelationshipMaterialization,
		Summary: "project aws_relationship facts into canonical AWS relationship graph edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "aws_relationship_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// awsRelationshipEvidenceSource tags AWS relationship edges written by this
// reducer so the prior-generation retract path scopes its delete to
// reducer-owned AWS relationship edges and never touches edges owned by other
// writers.
const awsRelationshipEvidenceSource = "reducer/aws-relationships"

// CloudResourceEdgeWriter persists and retracts canonical AWS relationship
// edges between CloudResource nodes. Implementations MUST be idempotent by
// (source uid, relationship_type, target uid) so reducer retries and duplicate
// facts converge on one edge, and MUST NOT fabricate endpoint nodes: a row
// whose source or target node is absent is a no-op.
type CloudResourceEdgeWriter interface {
	WriteCloudResourceEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractCloudResourceEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// AWSRelationshipMaterializationHandler reduces one AWS relationship
// materialization follow-up into canonical AWS relationship edge writes. It
// gates on the GraphProjectionPhaseCanonicalNodesCommitted readiness phase that
// DomainAWSResourceMaterialization (PR 1) publishes on the CloudResource
// keyspace, so edges never resolve against a generation whose nodes have not
// committed. It then loads the scope generation's aws_resource and
// aws_relationship facts, resolves both endpoints to CloudResource uids through
// a bounded in-memory join index (no per-edge graph round trip), and hands the
// resolved batch to the edge writer. Unresolved endpoints are counted and
// logged, never written and never dropped silently.
//
// See docs/internal/aws-relationship-edge-materialization-design.md §5–§8.
type AWSRelationshipMaterializationHandler struct {
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

// Handle executes one AWS relationship materialization intent.
func (h AWSRelationshipMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainAWSRelationshipMaterialization {
		return Result{}, fmt.Errorf(
			"aws relationship materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("aws relationship materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("aws relationship materialization edge writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerAWSRelationshipMaterialization,
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
		return Result{}, awsRelationshipNodesNotReadyError{
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
		[]string{facts.AWSResourceFactKind, facts.AWSRelationshipFactKind},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for aws relationship materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, relationshipEnvelopes := splitAWSFactEnvelopes(envelopes)

	extractStart := time.Now()
	rows, tally := ExtractAWSRelationshipEdgeRows(resourceEnvelopes, relationshipEnvelopes)
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
			awsRelationshipEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical aws relationship edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.EdgeWriter.WriteCloudResourceEdges(ctx, rows, intent.ScopeID, intent.GenerationID, awsRelationshipEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical aws relationship edges: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	h.recordTally(ctx, tally)
	logAWSRelationshipMaterializationCompleted(ctx, awsRelationshipMaterializationTiming{
		intent:             intent,
		resourceFactCount:  len(resourceEnvelopes),
		relationshipCount:  len(relationshipEnvelopes),
		edgeCount:          len(rows),
		resolvedTally:      tally.resolved,
		unresolvedTally:    tally.unresolved,
		unresolvedSrcTally: tally.unresolvedSource,
		skipRetract:        skipRetract,
		loadDuration:       loadDuration,
		extractDuration:    extractDuration,
		retractDuration:    retractDuration,
		writeDuration:      writeDuration,
		totalDuration:      time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainAWSRelationshipMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d aws relationship edge(s) from %d relationship fact(s); %d target(s) unresolved",
			len(rows),
			len(relationshipEnvelopes),
			tally.totalUnresolved(),
		),
		CanonicalWrites: len(rows),
	}, nil
}

// canonicalNodesReady reports whether the PR-1 canonical-nodes-committed phase
// is published for this intent's scope generation. The phase key is derived the
// same way DomainAWSResourceMaterialization publishes it (publishIntentGraphPhase
// -> graphProjectionPhaseStateForIntent), so the lookup matches the published
// row. A nil lookup keeps the gate open for test wiring.
func (h AWSRelationshipMaterializationHandler) canonicalNodesReady(intent Intent) bool {
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

// shouldSkipRetract mirrors the SQL relationship domain: skip the prior-edge
// retract on the very first generation for a scope (no prior edges to remove)
// and only on the first attempt, so a retried attempt still cleans up a partial
// prior write.
func (h AWSRelationshipMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for aws relationship retract: %w", err)
	}
	return !hasPrior, nil
}

// recordTally emits the edge-projection counter dimensioned by
// (relationship_type, join_mode), the contract registered for
// eshu_dp_aws_relationship_edges_total. Each tally entry carries the real AWS
// relationship type (e.g. USES_KMS_KEY) and the resolution mode that produced
// it — arn / bare_id / correlation_anchor for materialized edges, or unresolved
// when an endpoint was not a materialized node in this generation. The
// per-target_type breakdown (which service was not scanned) is the completion
// log's job, not a metric label, so cardinality stays bounded.
func (h AWSRelationshipMaterializationHandler) recordTally(ctx context.Context, tally awsRelationshipEdgeTally) {
	if h.Instruments == nil || h.Instruments.AWSRelationshipEdges == nil {
		return
	}
	for key, count := range tally.byRelTypeMode {
		h.Instruments.AWSRelationshipEdges.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrRelationshipType(key.relationshipType),
			telemetry.AttrJoinMode(key.mode),
		))
	}
}

// totalUnresolved returns the count of relationship facts that did not
// materialize an edge because an endpoint was unresolved.
func (t awsRelationshipEdgeTally) totalUnresolved() int {
	total := 0
	for _, count := range t.unresolved {
		total += count
	}
	for _, count := range t.unresolvedSource {
		total += count
	}
	return total
}

// splitAWSFactEnvelopes partitions a mixed envelope slice into resource and
// relationship facts in one pass so the join index and edge facts are built
// from a single bounded load.
func splitAWSFactEnvelopes(envelopes []facts.Envelope) (resources, relationships []facts.Envelope) {
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources = append(resources, env)
		case facts.AWSRelationshipFactKind:
			relationships = append(relationships, env)
		}
	}
	return resources, relationships
}

// awsRelationshipNodesNotReadyError marks the readiness-gate miss as retryable
// so the durable queue re-runs the intent once PR-1 nodes commit, instead of
// failing terminally or writing edges against absent nodes.
type awsRelationshipNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e awsRelationshipNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical cloud resource nodes not committed for scope %s generation %s",
		e.scopeID,
		e.generationID,
	)
}

func (awsRelationshipNodesNotReadyError) Retryable() bool { return true }

func (awsRelationshipNodesNotReadyError) FailureClass() string {
	return "aws_relationship_nodes_not_ready"
}

// awsRelationshipMaterializationTiming groups stage durations and the
// resolution tally so the completion log identifies fact-load, extraction,
// retract, and graph-write time, plus which target types lost edges.
type awsRelationshipMaterializationTiming struct {
	intent             Intent
	resourceFactCount  int
	relationshipCount  int
	edgeCount          int
	resolvedTally      map[string]int
	unresolvedTally    map[string]int
	unresolvedSrcTally map[string]int
	skipRetract        bool
	loadDuration       time.Duration
	extractDuration    time.Duration
	retractDuration    time.Duration
	writeDuration      time.Duration
	totalDuration      time.Duration
}

func logAWSRelationshipMaterializationCompleted(
	ctx context.Context,
	timing awsRelationshipMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "aws relationship materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.resourceFactCount),
		slog.Int("relationship_fact_count", timing.relationshipCount),
		slog.Int("edge_count", timing.edgeCount),
		slog.String("resolved_by_mode", formatTally(timing.resolvedTally)),
		slog.String("unresolved_target_by_type", formatTally(timing.unresolvedTally)),
		slog.String("unresolved_source_by_type", formatTally(timing.unresolvedSrcTally)),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}

// formatTally renders a bounded count map as a deterministic key=value string
// for the structured completion log. Cardinality is bounded by the closed set
// of join modes / scanned target types.
func formatTally(counts map[string]int) string {
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
