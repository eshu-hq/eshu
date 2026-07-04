// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// rdsPostureMaterializationDomainDefinition returns the additive definition for
// RDS posture node-property projection. It is additive because the handler
// requires an explicitly wired RDSPostureNodeWriter and FactLoader; registering
// it without them would silently drop every posture intent.
func rdsPostureMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainRDSPostureMaterialization,
		Summary: "project rds_instance_posture facts onto canonical CloudResource node properties",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "rds_posture_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// rdsPostureEvidenceSource tags CloudResource properties written by this
// reducer so retraction removes only reducer-owned RDS posture fields.
const rdsPostureEvidenceSource = "reducer/rds-posture"

// RDSPostureNodeWriter persists and retracts RDS posture properties on existing
// CloudResource nodes. Implementations MUST be idempotent by CloudResource uid
// and MUST NOT fabricate nodes when a row's uid is absent.
type RDSPostureNodeWriter interface {
	WriteRDSPostureNodes(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractRDSPostureNodes(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// RDSPostureMaterializationHandler reduces one RDS posture materialization
// intent into CloudResource property updates. It gates on the
// cloud_resource_uid canonical-nodes phase published by
// DomainAWSResourceMaterialization, loads aws_resource and rds_instance_posture
// facts for the same scope generation, and writes only rows whose source RDS
// DB instance or Aurora cluster was scanned as a CloudResource in that
// generation.
type RDSPostureMaterializationHandler struct {
	FactLoader FactLoader
	NodeWriter RDSPostureNodeWriter
	// ReadinessLookup reports whether the canonical CloudResource nodes have
	// committed. A nil lookup keeps the gate open for test wiring.
	ReadinessLookup GraphProjectionReadinessLookup
	// PriorGenerationCheck reports whether the scope has any prior generation.
	// Nil keeps retract behavior conservative.
	PriorGenerationCheck PriorGenerationCheck
	Tracer               trace.Tracer
	// Instruments records the eshu_dp_reducer_input_invalid_facts_total counter
	// when an aws_resource join fact is quarantined as input_invalid during the
	// posture resource index build. Optional: a nil pointer skips the counter
	// (the structured per-fact error log still emits).
	Instruments *telemetry.Instruments
}

// Handle executes one RDS posture materialization intent.
func (h RDSPostureMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainRDSPostureMaterialization {
		return Result{}, fmt.Errorf(
			"rds posture materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("rds posture materialization fact loader is required")
	}
	if h.NodeWriter == nil {
		return Result{}, fmt.Errorf("rds posture materialization node writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerRDSPostureMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	if !h.canonicalNodesReady(intent) {
		return Result{}, rdsPostureNodesNotReadyError{
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
		rdsPostureFactKinds(),
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for rds posture materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, postureEnvelopes := splitRDSPostureEnvelopes(envelopes)

	extractStart := time.Now()
	rows, tally, quarantined, err := ExtractRDSPostureRows(resourceEnvelopes, postureEnvelopes)
	if err != nil {
		// A non-decode error (transient fact-load or other fatal condition
		// partitionDecodeFailures did NOT quarantine) fails the whole intent so
		// the durable queue triages it correctly.
		return Result{}, err
	}
	// Per-fact isolation: a malformed aws_resource join fact (a missing required
	// identity field) is quarantined as a visible input_invalid dead-letter —
	// counter + structured error log — while valid posture still materializes.
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainRDSPostureMaterialization, intent.ScopeID, intent.GenerationID, quarantined)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.NodeWriter.RetractRDSPostureNodes(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			rdsPostureEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical rds posture properties: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.NodeWriter.WriteRDSPostureNodes(
			ctx,
			rows,
			intent.ScopeID,
			intent.GenerationID,
			rdsPostureEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical rds posture properties: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	logRDSPostureMaterializationCompleted(ctx, rdsPostureMaterializationTiming{
		intent:          intent,
		resourceCount:   len(resourceEnvelopes),
		postureCount:    len(postureEnvelopes),
		rowCount:        len(rows),
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
		Domain:   DomainRDSPostureMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d rds posture node update(s) from %d posture fact(s); %d posture fact(s) skipped; %d input_invalid fact(s) quarantined",
			len(rows),
			len(postureEnvelopes),
			tally.totalSkipped(),
			inputInvalidCount,
		),
		CanonicalWrites: len(rows),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

func rdsPostureFactKinds() []string {
	return []string{facts.AWSResourceFactKind, facts.RDSInstancePostureFactKind}
}

func (h RDSPostureMaterializationHandler) canonicalNodesReady(intent Intent) bool {
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

func (h RDSPostureMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for rds posture retract: %w", err)
	}
	return !hasPrior, nil
}

func splitRDSPostureEnvelopes(envelopes []facts.Envelope) (resources, postures []facts.Envelope) {
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources = append(resources, env)
		case facts.RDSInstancePostureFactKind:
			postures = append(postures, env)
		}
	}
	return resources, postures
}

type rdsPostureNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e rdsPostureNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical cloud resource nodes not committed for rds posture scope %s generation %s",
		e.scopeID,
		e.generationID,
	)
}

func (rdsPostureNodesNotReadyError) Retryable() bool { return true }

func (rdsPostureNodesNotReadyError) FailureClass() string {
	return "rds_posture_nodes_not_ready"
}

type rdsPostureMaterializationTiming struct {
	intent          Intent
	resourceCount   int
	postureCount    int
	rowCount        int
	skippedByReason map[string]int
	skipRetract     bool
	loadDuration    time.Duration
	extractDuration time.Duration
	retractDuration time.Duration
	writeDuration   time.Duration
	totalDuration   time.Duration
}

func logRDSPostureMaterializationCompleted(
	ctx context.Context,
	timing rdsPostureMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "rds posture materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.resourceCount),
		slog.Int("posture_fact_count", timing.postureCount),
		slog.Int("node_update_count", timing.rowCount),
		slog.String("skipped_by_reason", formatTally(timing.skippedByReason)),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
