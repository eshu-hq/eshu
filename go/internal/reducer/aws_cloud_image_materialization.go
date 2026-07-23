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

// awsCloudImageMaterializationDomainDefinition returns the additive
// definition for the AWS cloud-image edge projection. It is additive (not
// part of DefaultDomainDefinitions) because the handler requires an
// explicitly wired CloudResourceContainerImageEdgeWriter and FactLoader;
// registering it without them would silently drop every intent. It mirrors
// awsRelationshipMaterializationDomainDefinition (#805 PR2) as an ADDITIVE
// SIBLING truth contract (see DomainAWSCloudImageMaterialization's doc
// comment in intent.go for why this is a distinct domain rather than an
// extension of DomainAWSRelationshipMaterialization). See issue #5450 and
// docs/internal/aws-relationship-edge-materialization-design.md.
func awsCloudImageMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainAWSCloudImageMaterialization,
		Summary: "project lambda_function_uses_image aws_relationship facts into canonical CloudResource -> ContainerImage graph edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "aws_cloud_image_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// awsCloudImageEvidenceSource tags CloudResource -> ContainerImage edges
// written by this reducer so the prior-generation retract path scopes its
// delete to reducer-owned AWS cloud-image edges and never touches edges owned
// by other writers (including the sibling DomainAWSRelationshipMaterialization,
// which uses the distinct "reducer/aws-relationships" evidence_source).
const awsCloudImageEvidenceSource = "reducer/aws-cloud-image"

// CloudResourceContainerImageEdgeWriter persists and retracts canonical
// CloudResource -> ContainerImage edges. Implementations MUST be idempotent
// by (source uid, target uid) so reducer retries and duplicate facts converge
// on one edge, and MUST NOT fabricate endpoint nodes: a row whose source
// CloudResource or target ContainerImage node is absent is a no-op.
type CloudResourceContainerImageEdgeWriter interface {
	WriteCloudResourceContainerImageEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractCloudResourceContainerImageEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// AWSCloudImageMaterializationHandler reduces one AWS cloud-image
// materialization follow-up into canonical CloudResource -> ContainerImage
// edge writes (issue #5450). It gates on the
// GraphProjectionPhaseCanonicalNodesCommitted readiness phase that
// DomainAWSResourceMaterialization publishes on the CloudResource keyspace —
// the source endpoint — so edges never resolve against a source that has not
// committed. The target ContainerImage endpoint is NOT gated on a readiness
// phase: OCI registry canonical nodes materialize through the source-local
// projector path (internal/projector/oci_registry_canonical.go), independent
// of the reducer's scope-generation phases, so an unscanned image is simply a
// no-op MATCH miss (graceful degradation), exactly like every other AWS edge
// writer's forward-looking-target case.
//
// It then loads the scope generation's aws_resource and aws_relationship
// facts, resolves the source to a CloudResource uid through the same bounded
// in-memory join index DomainAWSRelationshipMaterialization uses, computes the
// target ContainerImage uid directly from the relationship's own
// resolved_image_uri attribute, and hands the resolved batch to the edge
// writer. Every non-materializing relationship fact is counted and logged,
// never dropped silently.
type AWSCloudImageMaterializationHandler struct {
	FactLoader FactLoader
	EdgeWriter CloudResourceContainerImageEdgeWriter
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

// Handle executes one AWS cloud-image materialization intent.
func (h AWSCloudImageMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainAWSCloudImageMaterialization {
		return Result{}, fmt.Errorf(
			"aws cloud image materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("aws cloud image materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("aws cloud image materialization edge writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerAWSCloudImageMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	// Readiness gate: edges may only resolve their SOURCE against nodes the
	// same generation already committed.
	if !h.sourceNodesReady(intent) {
		return Result{}, awsCloudImageNodesNotReadyError{
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
		return Result{}, fmt.Errorf("load facts for aws cloud image materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, relationshipEnvelopes := splitAWSFactEnvelopes(envelopes)

	extractStart := time.Now()
	rows, tally, quarantined, err := ExtractAWSCloudImageEdgeRows(resourceEnvelopes, relationshipEnvelopes)
	if err != nil {
		// A non-decode error (transient fact-load or other fatal condition
		// partitionDecodeFailures did NOT quarantine) fails the whole intent so
		// the durable queue triages it correctly.
		return Result{}, err
	}
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainAWSCloudImageMaterialization, intent.ScopeID, intent.GenerationID, quarantined)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.EdgeWriter.RetractCloudResourceContainerImageEdges(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			awsCloudImageEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical aws cloud image edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.EdgeWriter.WriteCloudResourceContainerImageEdges(
			ctx, rows, intent.ScopeID, intent.GenerationID, awsCloudImageEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical aws cloud image edges: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	h.recordEdgeCounter(ctx, tally)
	logAWSCloudImageMaterializationCompleted(ctx, awsCloudImageMaterializationTiming{
		intent:            intent,
		resourceFactCount: len(resourceEnvelopes),
		relationshipCount: len(relationshipEnvelopes),
		edgeCount:         len(rows),
		skippedByReason:   tally.skipped,
		skipRetract:       skipRetract,
		loadDuration:      loadDuration,
		extractDuration:   extractDuration,
		retractDuration:   retractDuration,
		writeDuration:     writeDuration,
		totalDuration:     time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainAWSCloudImageMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d aws cloud image edge(s) from %d relationship fact(s); %d skipped (policy/unresolved); %d input_invalid fact(s) quarantined",
			len(rows),
			len(relationshipEnvelopes),
			tally.totalSkipped(),
			inputInvalidCount,
		),
		CanonicalWrites: len(rows),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

// sourceNodesReady reports whether the canonical-nodes-committed phase is
// published for this intent's scope generation on the CloudResource keyspace
// (the source endpoint). The phase key is derived the same way
// DomainAWSResourceMaterialization publishes it, so the lookup matches the
// published row. A nil lookup keeps the gate open for test wiring.
func (h AWSCloudImageMaterializationHandler) sourceNodesReady(intent Intent) bool {
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

// shouldSkipRetract mirrors the sibling AWS relationship domain: skip the
// prior-edge retract on the very first generation for a scope (no prior edges
// to remove) and only on the first attempt, so a retried attempt still cleans
// up a partial prior write.
func (h AWSCloudImageMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for aws cloud image retract: %w", err)
	}
	return !hasPrior, nil
}

// recordEdgeCounter emits eshu_dp_aws_cloud_image_edges_total, the
// materialized-only counter dimensioned by resolution_mode (always
// container_image_digest today, kept as a dimension for forward
// compatibility). Skipped counts go to the completion log's skippedByReason
// tally, not this metric, so cardinality stays bounded.
func (h AWSCloudImageMaterializationHandler) recordEdgeCounter(ctx context.Context, tally awsCloudImageEdgeTally) {
	if h.Instruments == nil || h.Instruments.AWSCloudImageEdges == nil || tally.resolved == 0 {
		return
	}
	h.Instruments.AWSCloudImageEdges.Add(ctx, int64(tally.resolved), metric.WithAttributes(
		telemetry.AttrResolutionMode(awsCloudImageResolutionMode),
	))
}

// awsCloudImageNodesNotReadyError marks the readiness-gate miss as retryable
// so the durable queue re-runs the intent once the source CloudResource nodes
// commit, instead of failing terminally or writing edges against an absent
// source.
type awsCloudImageNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e awsCloudImageNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical cloud resource nodes not committed for scope %s generation %s",
		e.scopeID,
		e.generationID,
	)
}

func (awsCloudImageNodesNotReadyError) Retryable() bool { return true }

func (awsCloudImageNodesNotReadyError) FailureClass() string {
	return "aws_cloud_image_nodes_not_ready"
}

// awsCloudImageMaterializationTiming groups stage durations and the skip
// tally so the completion log identifies fact-load, extraction, retract, and
// graph-write time, plus which policy/resolution disposition kept a
// relationship fact from materializing an edge.
type awsCloudImageMaterializationTiming struct {
	intent            Intent
	resourceFactCount int
	relationshipCount int
	edgeCount         int
	skippedByReason   map[awsCloudImageEdgeSkipReason]int
	skipRetract       bool
	loadDuration      time.Duration
	extractDuration   time.Duration
	retractDuration   time.Duration
	writeDuration     time.Duration
	totalDuration     time.Duration
}

func logAWSCloudImageMaterializationCompleted(
	ctx context.Context,
	timing awsCloudImageMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "aws cloud image materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.resourceFactCount),
		slog.Int("relationship_fact_count", timing.relationshipCount),
		slog.Int("edge_count", timing.edgeCount),
		slog.String("skipped_by_reason", formatAWSCloudImageSkipTally(timing.skippedByReason)),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}

// formatAWSCloudImageSkipTally renders the bounded skip-reason count map as a
// deterministic key=value string for the structured completion log, mirroring
// formatTally but keyed by the typed awsCloudImageEdgeSkipReason enum instead
// of a raw string map.
func formatAWSCloudImageSkipTally(counts map[awsCloudImageEdgeSkipReason]int) string {
	generic := make(map[string]int, len(counts))
	for reason, count := range counts {
		generic[string(reason)] = count
	}
	return formatTally(generic)
}
