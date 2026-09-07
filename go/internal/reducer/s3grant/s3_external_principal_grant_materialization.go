// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3grant

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const s3ExternalPrincipalGrantEvidenceSource = "reducer/s3-external-principal-grant"

// MaterializationDomainDefinition returns the additive definition for the S3
// external-principal grant projection. It is additive (not part of
// DefaultDomainDefinitions) because the handler requires an explicitly wired
// S3ExternalPrincipalGrantWriter and FactLoader; registering it without them
// would silently drop every intent.
func MaterializationDomainDefinition() reducercontract.DomainDefinition {
	return reducercontract.DomainDefinition{
		Domain:  reducercontract.DomainS3ExternalPrincipalGrantMaterialization,
		Summary: "project metadata-only S3 external-principal grants into canonical GRANTS_ACCESS_TO graph edges",
		Ownership: reducercontract.OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "s3_external_principal_grant_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// S3ExternalPrincipalGrantWriter persists and retracts canonical
// GRANTS_ACCESS_TO edges from S3 CloudResource nodes to ExternalPrincipal nodes.
// Implementations MUST match the S3 source bucket by uid, MUST NOT create
// CloudResource nodes, and MUST keep the relationship type static.
type S3ExternalPrincipalGrantWriter interface {
	WriteS3ExternalPrincipalGrants(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractS3ExternalPrincipalGrants(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// S3ExternalPrincipalGrantMaterializationHandler reduces one S3 external
// principal grant follow-up into ExternalPrincipal nodes and GRANTS_ACCESS_TO
// edges. It gates on the source S3 CloudResource canonical-node phase, loads
// only aws_resource and s3_external_principal_grant facts, and never persists raw
// policy material.
type S3ExternalPrincipalGrantMaterializationHandler struct {
	FactLoader           factload.FactLoader
	GrantWriter          S3ExternalPrincipalGrantWriter
	ReadinessLookup      gpphase.ReadinessLookup
	PriorGenerationCheck reducercontract.PriorGenerationCheck
	Tracer               trace.Tracer
	// Instruments records the eshu_dp_reducer_input_invalid_facts_total counter
	// when an aws_resource join fact is quarantined as input_invalid during the
	// bucket index build. Optional: a nil pointer skips the counter (the
	// structured per-fact error log still emits).
	Instruments *telemetry.Instruments
}

func s3ExternalPrincipalGrantFactKinds() []string {
	return []string{facts.AWSResourceFactKind, facts.S3ExternalPrincipalGrantFactKind}
}

// Handle executes one S3 external-principal grant materialization intent.
func (h S3ExternalPrincipalGrantMaterializationHandler) Handle(
	ctx context.Context,
	intent reducercontract.Intent,
) (reducercontract.Result, error) {
	totalStart := time.Now()
	if intent.Domain != reducercontract.DomainS3ExternalPrincipalGrantMaterialization {
		return reducercontract.Result{}, fmt.Errorf("s3 external-principal grant materialization handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return reducercontract.Result{}, fmt.Errorf("s3 external-principal grant materialization fact loader is required")
	}
	if h.GrantWriter == nil {
		return reducercontract.Result{}, fmt.Errorf("s3 external-principal grant materialization writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerS3ExternalPrincipalGrantMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	if !h.canonicalNodesReady(intent) {
		return reducercontract.Result{}, s3ExternalPrincipalGrantNodesNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
		}
	}

	loadStart := time.Now()
	envelopes, err := factload.LoadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, s3ExternalPrincipalGrantFactKinds())
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("load facts for s3 external-principal grant materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, grantEnvelopes := splitS3ExternalPrincipalGrantEnvelopes(envelopes)
	extractStart := time.Now()
	rows, tally, quarantined, err := ExtractS3ExternalPrincipalGrantRows(resourceEnvelopes, grantEnvelopes)
	if err != nil {
		// A non-decode error (transient fact-load or other fatal condition
		// partitionDecodeFailures did NOT quarantine) fails the whole intent so
		// the durable queue triages it correctly.
		return reducercontract.Result{}, err
	}
	// Per-fact isolation: a malformed aws_resource join fact (a missing required
	// identity field) is quarantined as a visible input_invalid dead-letter —
	// counter + structured error log — while the batch's valid grants still
	// materialize below and the readiness phase still publishes.
	inputInvalidCount := factdecode.RecordQuarantinedFacts(ctx, h.Instruments, reducercontract.DomainS3ExternalPrincipalGrantMaterialization, intent.ScopeID, intent.GenerationID, quarantined)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return reducercontract.Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.GrantWriter.RetractS3ExternalPrincipalGrants(ctx, []string{intent.ScopeID}, intent.GenerationID, s3ExternalPrincipalGrantEvidenceSource); err != nil {
			return reducercontract.Result{}, fmt.Errorf("retract canonical s3 external-principal grant edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.GrantWriter.WriteS3ExternalPrincipalGrants(ctx, rows, intent.ScopeID, intent.GenerationID, s3ExternalPrincipalGrantEvidenceSource); err != nil {
			return reducercontract.Result{}, fmt.Errorf("write canonical s3 external-principal grant edges: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	logS3ExternalPrincipalGrantMaterializationCompleted(ctx, s3ExternalPrincipalGrantTiming{
		intent:            intent,
		resourceCount:     len(resourceEnvelopes),
		grantCount:        len(grantEnvelopes),
		rowCount:          len(rows),
		resolvedByOutcome: tally.resolved,
		skippedByReason:   tally.skipped,
		skipRetract:       skipRetract,
		loadDuration:      loadDuration,
		extractDuration:   extractDuration,
		retractDuration:   retractDuration,
		writeDuration:     writeDuration,
		totalDuration:     time.Since(totalStart),
	})

	return reducercontract.Result{
		IntentID: intent.IntentID,
		Domain:   reducercontract.DomainS3ExternalPrincipalGrantMaterialization,
		Status:   reducercontract.ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d S3 external-principal grant edge(s) from %d grant fact(s); %d grant fact(s) skipped; %d input_invalid fact(s) quarantined",
			len(rows),
			len(grantEnvelopes),
			tally.totalSkipped(),
			inputInvalidCount,
		),
		CanonicalWrites: len(rows),
		SubSignals:      factdecode.InputInvalidSubSignals(inputInvalidCount),
	}, nil
}

func (h S3ExternalPrincipalGrantMaterializationHandler) canonicalNodesReady(intent reducercontract.Intent) bool {
	if h.ReadinessLookup == nil {
		return true
	}
	state, ok := gpphase.StateForIntentValue(
		intent,
		gpphase.KeyspaceCloudResourceUID,
		gpphase.PhaseCanonicalNodesCommitted,
		time.Now().UTC(),
	)
	if !ok {
		return false
	}
	ready, found := h.ReadinessLookup(state.Key, gpphase.PhaseCanonicalNodesCommitted)
	return found && ready
}

func (h S3ExternalPrincipalGrantMaterializationHandler) shouldSkipRetract(ctx context.Context, intent reducercontract.Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for s3 external-principal grant retract: %w", err)
	}
	return !hasPrior, nil
}

func splitS3ExternalPrincipalGrantEnvelopes(envelopes []facts.Envelope) (resources, grants []facts.Envelope) {
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources = append(resources, env)
		case facts.S3ExternalPrincipalGrantFactKind:
			grants = append(grants, env)
		}
	}
	return resources, grants
}

type s3ExternalPrincipalGrantNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e s3ExternalPrincipalGrantNodesNotReadyError) Error() string {
	return fmt.Sprintf("canonical cloud resource nodes not committed for scope %s generation %s", e.scopeID, e.generationID)
}

func (s3ExternalPrincipalGrantNodesNotReadyError) Retryable() bool { return true }

// S3ExternalPrincipalGrantNodesNotReadyFailureClass identifies an in-handler readiness-gate miss: the
// S3 external-principal-grant edge intent ran before its upstream cloud-resource
// canonical-nodes-committed phase published for the same acceptance unit. The
// durable claim gate (reducerClaimReadinessRequirementsSQL) normally prevents
// that, so this fires only in the claim/handle race window where the handler's
// own ReadinessLookup disagrees with the claim-time gate.
//
// Enrolled in nonCountingReducerRetryFailureClasses (#5046) so the miss never
// erodes the retry budget and dead-letters a still-pending intent that the
// succeeded-only reopen path would never reopen. Declaring the constant is not
// what enrolls it -- see that list's doc comment, and the go/ast completeness
// test that now checks every readiness class is registered.
const S3ExternalPrincipalGrantNodesNotReadyFailureClass = "s3_external_principal_grant_nodes_not_ready"

func (s3ExternalPrincipalGrantNodesNotReadyError) FailureClass() string {
	return S3ExternalPrincipalGrantNodesNotReadyFailureClass
}

type s3ExternalPrincipalGrantTiming struct {
	intent            reducercontract.Intent
	resourceCount     int
	grantCount        int
	rowCount          int
	resolvedByOutcome map[string]int
	skippedByReason   map[string]int
	skipRetract       bool
	loadDuration      time.Duration
	extractDuration   time.Duration
	retractDuration   time.Duration
	writeDuration     time.Duration
	totalDuration     time.Duration
}

func logS3ExternalPrincipalGrantMaterializationCompleted(
	ctx context.Context,
	timing s3ExternalPrincipalGrantTiming,
) {
	slog.InfoContext(
		ctx, "s3 external-principal grant materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.resourceCount),
		slog.Int("grant_fact_count", timing.grantCount),
		slog.Int("edge_count", timing.rowCount),
		slog.String("resolved_by_outcome", payloadcore.FormatTally(timing.resolvedByOutcome)),
		slog.String("skipped_by_reason", payloadcore.FormatTally(timing.skippedByReason)),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
