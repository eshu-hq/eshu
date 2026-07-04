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

func s3InternetExposureMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainS3InternetExposureMaterialization,
		Summary: "derive s3_bucket_posture internet exposure and set S3 CloudResource properties",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "s3_internet_exposure_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

const s3InternetExposureEvidenceSource = "reducer/s3-internet-exposure"

// S3InternetExposureNodeWriter persists and retracts reducer-owned S3 internet
// exposure properties on existing S3 CloudResource nodes. Implementations MUST
// match by uid and MUST NOT create CloudResource nodes.
type S3InternetExposureNodeWriter interface {
	WriteS3InternetExposureNodes(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractS3InternetExposureNodes(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// S3InternetExposureMaterializationHandler reduces one s3_bucket_posture follow-up
// into conservative internet-exposure properties on existing S3 CloudResource
// nodes. It gates on CloudResource canonical-node readiness, loads only
// aws_resource plus s3_bucket_posture facts, and never treats unknown posture as
// a safe false value.
type S3InternetExposureMaterializationHandler struct {
	FactLoader FactLoader
	NodeWriter S3InternetExposureNodeWriter
	// ReadinessLookup reports whether CloudResource nodes for this generation are
	// committed. A nil lookup keeps tests light; production wires Postgres.
	ReadinessLookup GraphProjectionReadinessLookup
	// PriorGenerationCheck reports whether a scope has prior rows to retract.
	// Nil keeps retract behavior conservative.
	PriorGenerationCheck PriorGenerationCheck
	Tracer               trace.Tracer
	Instruments          *telemetry.Instruments
}

func s3InternetExposureFactKinds() []string {
	return []string{facts.AWSResourceFactKind, facts.S3BucketPostureFactKind}
}

// Handle executes one S3 internet-exposure materialization intent.
func (h S3InternetExposureMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainS3InternetExposureMaterialization {
		return Result{}, fmt.Errorf("s3 internet exposure materialization handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("s3 internet exposure materialization fact loader is required")
	}
	if h.NodeWriter == nil {
		return Result{}, fmt.Errorf("s3 internet exposure materialization node writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerS3InternetExposureMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	if !h.canonicalNodesReady(intent) {
		return Result{}, s3InternetExposureNodesNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
		}
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, s3InternetExposureFactKinds())
	if err != nil {
		return Result{}, fmt.Errorf("load facts for s3 internet exposure materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, postureEnvelopes := splitS3InternetExposureEnvelopes(envelopes)
	extractStart := time.Now()
	rows, tally, quarantined, err := ExtractS3InternetExposureRows(resourceEnvelopes, postureEnvelopes)
	if err != nil {
		// A non-decode error (transient fact-load or other fatal condition
		// partitionDecodeFailures did NOT quarantine) fails the whole intent so
		// the durable queue triages it correctly.
		return Result{}, err
	}
	// Per-fact isolation: a malformed aws_resource/s3_bucket_posture fact (a
	// missing required identity field) is quarantined as a visible input_invalid
	// dead-letter — counter + structured error log — while the batch's valid
	// facts still materialize below and the readiness phase still publishes, so
	// one bad fact never stalls the scope generation's graph.
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainS3InternetExposureMaterialization, intent.ScopeID, intent.GenerationID, quarantined)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.NodeWriter.RetractS3InternetExposureNodes(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			s3InternetExposureEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical s3 internet exposure properties: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.NodeWriter.WriteS3InternetExposureNodes(
			ctx,
			rows,
			intent.ScopeID,
			intent.GenerationID,
			s3InternetExposureEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical s3 internet exposure properties: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	h.recordDecisionCounters(ctx, tally)
	logS3InternetExposureMaterializationCompleted(ctx, s3InternetExposureTiming{
		intent:          intent,
		resourceCount:   len(resourceEnvelopes),
		postureCount:    len(postureEnvelopes),
		rowCount:        len(rows),
		decisions:       tally.decisions,
		reasons:         tally.reasons,
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
		Domain:   DomainS3InternetExposureMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d S3 internet exposure row(s) from %d posture fact(s); %d posture fact(s) skipped; %d input_invalid fact(s) quarantined",
			len(rows),
			len(postureEnvelopes),
			tally.totalSkipped(),
			inputInvalidCount,
		),
		CanonicalWrites: len(rows),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

func (h S3InternetExposureMaterializationHandler) canonicalNodesReady(intent Intent) bool {
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

func (h S3InternetExposureMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for s3 internet exposure retract: %w", err)
	}
	return !hasPrior, nil
}

func (h S3InternetExposureMaterializationHandler) recordDecisionCounters(ctx context.Context, tally s3InternetExposureTally) {
	if h.Instruments == nil {
		return
	}
	if h.Instruments.S3InternetExposureDecisions != nil {
		for key, count := range tally.decisionReasons {
			h.Instruments.S3InternetExposureDecisions.Add(ctx, int64(count), metric.WithAttributes(
				telemetry.AttrOutcome(key.outcome),
				telemetry.AttrReason(key.reason),
			))
		}
	}
	if h.Instruments.S3InternetExposureSkipped != nil {
		for reason, count := range tally.skipped {
			h.Instruments.S3InternetExposureSkipped.Add(ctx, int64(count), metric.WithAttributes(
				telemetry.AttrSkipReason(reason),
			))
		}
	}
}

func splitS3InternetExposureEnvelopes(envelopes []facts.Envelope) (resources, postures []facts.Envelope) {
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

type s3InternetExposureNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e s3InternetExposureNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical cloud resource nodes not committed for scope %s generation %s",
		e.scopeID,
		e.generationID,
	)
}

func (s3InternetExposureNodesNotReadyError) Retryable() bool { return true }

func (s3InternetExposureNodesNotReadyError) FailureClass() string {
	return "s3_internet_exposure_nodes_not_ready"
}

type s3InternetExposureTiming struct {
	intent          Intent
	resourceCount   int
	postureCount    int
	rowCount        int
	decisions       map[string]int
	reasons         map[string]int
	skippedByReason map[string]int
	skipRetract     bool
	loadDuration    time.Duration
	extractDuration time.Duration
	retractDuration time.Duration
	writeDuration   time.Duration
	totalDuration   time.Duration
}

func logS3InternetExposureMaterializationCompleted(
	ctx context.Context,
	timing s3InternetExposureTiming,
) {
	slog.InfoContext(
		ctx, "s3 internet exposure materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.resourceCount),
		slog.Int("posture_fact_count", timing.postureCount),
		slog.Int("row_count", timing.rowCount),
		slog.String("decisions", formatTally(timing.decisions)),
		slog.String("reasons", formatTally(timing.reasons)),
		slog.String("skipped_by_reason", formatTally(timing.skippedByReason)),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("derive_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
