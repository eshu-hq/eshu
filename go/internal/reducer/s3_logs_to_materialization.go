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

// s3LogsToMaterializationDomainDefinition returns the additive definition for S3
// LOGS_TO server-access-log edge projection. It is additive (not part of
// DefaultDomainDefinitions) because the handler requires an explicitly wired
// S3LogsToEdgeWriter and FactLoader; registering it without them would silently
// drop every intent. It mirrors iamCanAssumeMaterializationDomainDefinition
// (#1134 PR2). See issue #1144 PR2 and
// docs/internal/design/1144-s3-logs-to-edge.md.
func s3LogsToMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainS3LogsToMaterialization,
		Summary: "project s3_bucket_posture logging_target_bucket into canonical LOGS_TO graph edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "s3_logs_to_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// s3LogsToEvidenceSource tags LOGS_TO edges written by this reducer so the
// prior-generation retract path scopes its delete to reducer-owned log-delivery
// edges and never touches edges owned by other writers.
const s3LogsToEvidenceSource = "reducer/s3-logs-to"

// S3LogsToEdgeWriter persists and retracts canonical LOGS_TO edges between a
// source S3 bucket CloudResource node and the target log-bucket CloudResource it
// delivers server-access logs to. Implementations MUST be idempotent by
// (source uid, LOGS_TO, target uid) so reducer retries and duplicate facts
// converge on one edge, and MUST NOT fabricate endpoint nodes: a row whose
// source or target node is absent is a no-op.
type S3LogsToEdgeWriter interface {
	WriteS3LogsToEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractS3LogsToEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// S3LogsToMaterializationHandler reduces one S3 LOGS_TO materialization follow-up
// into canonical log-delivery edge writes. It gates on the
// GraphProjectionPhaseCanonicalNodesCommitted readiness phase that
// DomainAWSResourceMaterialization (#805 PR1) publishes on the cloud_resource_uid
// keyspace, so LOGS_TO edges never resolve against a generation whose S3 bucket
// nodes have not committed. It then loads the scope generation's aws_resource and
// s3_bucket_posture facts, resolves the source bucket and each
// logging_target_bucket to CloudResource uids through a bounded in-memory
// bucket-name join index (no per-edge graph round trip), and hands the resolved
// batch to the edge writer. Cross-account, out-of-scope, and unscanned log
// targets are counted and logged, never written and never dropped silently.
//
// See issue #1144 PR2 and docs/internal/design/1144-s3-logs-to-edge.md.
type S3LogsToMaterializationHandler struct {
	FactLoader FactLoader
	EdgeWriter S3LogsToEdgeWriter
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

// s3LogsToFactKinds is the bounded fact-kind allowlist the handler loads: the
// aws_resource S3 bucket node substrate for the join index and the
// s3_bucket_posture facts that drive the edges.
func s3LogsToFactKinds() []string {
	return []string{facts.AWSResourceFactKind, facts.S3BucketPostureFactKind}
}

// Handle executes one S3 LOGS_TO materialization intent.
func (h S3LogsToMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainS3LogsToMaterialization {
		return Result{}, fmt.Errorf(
			"s3 logs-to materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("s3 logs-to materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("s3 logs-to materialization edge writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerS3LogsToMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	// Readiness gate: LOGS_TO edges may only resolve against nodes the same
	// generation already committed. If the canonical-nodes phase is not yet
	// published, the intent re-enters the durable queue (retryable) rather than
	// writing edges against a node set that does not exist yet.
	if !h.canonicalNodesReady(intent) {
		return Result{}, s3LogsToNodesNotReadyError{
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
		s3LogsToFactKinds(),
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for s3 logs-to materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, postureEnvelopes := splitS3LogsToEnvelopes(envelopes)

	extractStart := time.Now()
	rows, tally, quarantined, err := ExtractS3LogsToEdgeRows(resourceEnvelopes, postureEnvelopes)
	if err != nil {
		// A non-decode error (transient fact-load, unsupported major, or other
		// fatal condition partitionDecodeFailures did NOT quarantine) fails the
		// whole intent so the durable queue triages it correctly.
		return Result{}, err
	}
	// Per-fact isolation: a malformed aws_resource/s3_bucket_posture fact (a
	// missing required identity field) is quarantined as a visible
	// input_invalid dead-letter — counter + structured error log — while the
	// batch's valid facts still project below.
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainS3LogsToMaterialization, intent.ScopeID, intent.GenerationID, quarantined)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.EdgeWriter.RetractS3LogsToEdges(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			s3LogsToEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical s3 logs-to edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.EdgeWriter.WriteS3LogsToEdges(
			ctx,
			rows,
			intent.ScopeID,
			intent.GenerationID,
			s3LogsToEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical s3 logs-to edges: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	h.recordEdgeCounter(ctx, rows)
	h.recordSkipCounter(ctx, tally)
	logS3LogsToMaterializationCompleted(ctx, s3LogsToMaterializationTiming{
		intent:          intent,
		resourceCount:   len(resourceEnvelopes),
		postureCount:    len(postureEnvelopes),
		edgeCount:       len(rows),
		resolvedByMode:  tally.resolved,
		skippedByReason: tally.skipped,
		skipRetract:     skipRetract,
		loadDuration:    loadDuration,
		extractDuration: extractDuration,
		retractDuration: retractDuration,
		writeDuration:   writeDuration,
		totalDuration:   time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainS3LogsToMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d LOGS_TO edge(s) from %d posture fact(s); %d log target(s) skipped (source/target unscanned); %d input_invalid fact(s) quarantined",
			len(rows),
			len(postureEnvelopes),
			tally.totalSkipped(),
			inputInvalidCount,
		),
		CanonicalWrites: len(rows),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

// canonicalNodesReady reports whether the #805 PR1 canonical-nodes-committed
// phase is published for this intent's scope generation. The phase key is
// derived the same way DomainAWSResourceMaterialization publishes it, so the
// lookup matches the published row. A nil lookup keeps the gate open for test
// wiring.
func (h S3LogsToMaterializationHandler) canonicalNodesReady(intent Intent) bool {
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
func (h S3LogsToMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for s3 logs-to retract: %w", err)
	}
	return !hasPrior, nil
}

// recordEdgeCounter emits the LOGS_TO edge-projection counter dimensioned by
// resolution_mode (name), the contract registered for
// eshu_dp_s3_logs_to_edges_total. Each materialized edge row carries its
// resolution mode; skipped log targets go to the skip counter and completion
// log, not this metric, so cardinality stays bounded.
func (h S3LogsToMaterializationHandler) recordEdgeCounter(
	ctx context.Context,
	rows []map[string]any,
) {
	if h.Instruments == nil || h.Instruments.S3LogsToEdges == nil {
		return
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[anyToString(row["resolution_mode"])]++
	}
	for mode, count := range counts {
		h.Instruments.S3LogsToEdges.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrResolutionMode(mode),
		))
	}
}

// recordSkipCounter emits the LOGS_TO skip counter dimensioned by the bounded
// skip_reason set (source_unresolved / target_unresolved), the contract
// registered for eshu_dp_s3_logs_to_skipped_total. It lets an operator see how
// many posture facts named a log target that produced no edge because an
// endpoint was not scanned, without a per-fact log line. Logging-disabled facts
// (blank target) are not counted here.
func (h S3LogsToMaterializationHandler) recordSkipCounter(
	ctx context.Context,
	tally s3LogsToEdgeTally,
) {
	if h.Instruments == nil || h.Instruments.S3LogsToSkipped == nil {
		return
	}
	for reason, count := range tally.skipped {
		if count == 0 {
			continue
		}
		h.Instruments.S3LogsToSkipped.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrSkipReason(reason),
		))
	}
}

// splitS3LogsToEnvelopes partitions a mixed envelope slice into aws_resource and
// s3_bucket_posture facts in one pass so the join index and posture facts are
// built from a single bounded load.
func splitS3LogsToEnvelopes(envelopes []facts.Envelope) (resources, postures []facts.Envelope) {
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources = append(resources, env)
		case facts.S3BucketPostureFactKind:
			postures = append(postures, env)
		}
	}
	return resources, postures
}

// s3LogsToNodesNotReadyError marks the readiness-gate miss as retryable so the
// durable queue re-runs the intent once #805 PR1 nodes commit, instead of
// failing terminally or writing edges against absent nodes.
type s3LogsToNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e s3LogsToNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical cloud resource nodes not committed for scope %s generation %s",
		e.scopeID,
		e.generationID,
	)
}

func (s3LogsToNodesNotReadyError) Retryable() bool { return true }

func (s3LogsToNodesNotReadyError) FailureClass() string {
	return "s3_logs_to_nodes_not_ready"
}

// s3LogsToMaterializationTiming groups stage durations and the edge tally so the
// completion log identifies fact-load, resolve, retract, and graph-write time,
// plus how many LOGS_TO edges materialized and which log targets were skipped.
type s3LogsToMaterializationTiming struct {
	intent          Intent
	resourceCount   int
	postureCount    int
	edgeCount       int
	resolvedByMode  map[string]int
	skippedByReason map[string]int
	skipRetract     bool
	loadDuration    time.Duration
	extractDuration time.Duration
	retractDuration time.Duration
	writeDuration   time.Duration
	totalDuration   time.Duration
}

func logS3LogsToMaterializationCompleted(
	ctx context.Context,
	timing s3LogsToMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "s3 logs-to materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.resourceCount),
		slog.Int("posture_fact_count", timing.postureCount),
		slog.Int("edge_count", timing.edgeCount),
		slog.String("resolved_by_mode", formatTally(timing.resolvedByMode)),
		slog.String("skipped_by_reason", formatTally(timing.skippedByReason)),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("resolve_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
