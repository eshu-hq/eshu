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
)

const s3ExternalPrincipalGrantEvidenceSource = "reducer/s3-external-principal-grant"

func s3ExternalPrincipalGrantMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainS3ExternalPrincipalGrantMaterialization,
		Summary: "project metadata-only S3 external-principal grants into canonical GRANTS_ACCESS_TO graph edges",
		Ownership: OwnershipShape{
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
	FactLoader           FactLoader
	GrantWriter          S3ExternalPrincipalGrantWriter
	ReadinessLookup      GraphProjectionReadinessLookup
	PriorGenerationCheck PriorGenerationCheck
	Tracer               trace.Tracer
}

func s3ExternalPrincipalGrantFactKinds() []string {
	return []string{facts.AWSResourceFactKind, facts.S3ExternalPrincipalGrantFactKind}
}

// Handle executes one S3 external-principal grant materialization intent.
func (h S3ExternalPrincipalGrantMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainS3ExternalPrincipalGrantMaterialization {
		return Result{}, fmt.Errorf("s3 external-principal grant materialization handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("s3 external-principal grant materialization fact loader is required")
	}
	if h.GrantWriter == nil {
		return Result{}, fmt.Errorf("s3 external-principal grant materialization writer is required")
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
		return Result{}, s3ExternalPrincipalGrantNodesNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
		}
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, s3ExternalPrincipalGrantFactKinds())
	if err != nil {
		return Result{}, fmt.Errorf("load facts for s3 external-principal grant materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, grantEnvelopes := splitS3ExternalPrincipalGrantEnvelopes(envelopes)
	extractStart := time.Now()
	rows, tally := ExtractS3ExternalPrincipalGrantRows(resourceEnvelopes, grantEnvelopes)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.GrantWriter.RetractS3ExternalPrincipalGrants(ctx, []string{intent.ScopeID}, intent.GenerationID, s3ExternalPrincipalGrantEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("retract canonical s3 external-principal grant edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.GrantWriter.WriteS3ExternalPrincipalGrants(ctx, rows, intent.ScopeID, intent.GenerationID, s3ExternalPrincipalGrantEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical s3 external-principal grant edges: %w", err)
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

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainS3ExternalPrincipalGrantMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d S3 external-principal grant edge(s) from %d grant fact(s); %d grant fact(s) skipped",
			len(rows),
			len(grantEnvelopes),
			tally.totalSkipped(),
		),
		CanonicalWrites: len(rows),
	}, nil
}

func (h S3ExternalPrincipalGrantMaterializationHandler) canonicalNodesReady(intent Intent) bool {
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

func (h S3ExternalPrincipalGrantMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
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

func (s3ExternalPrincipalGrantNodesNotReadyError) FailureClass() string {
	return "s3_external_principal_grant_nodes_not_ready"
}

type s3ExternalPrincipalGrantTiming struct {
	intent            Intent
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
		slog.String(telemetry.LogKeyScopeID, timing.intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, timing.intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.resourceCount),
		slog.Int("grant_fact_count", timing.grantCount),
		slog.Int("edge_count", timing.rowCount),
		slog.String("resolved_by_outcome", formatTally(timing.resolvedByOutcome)),
		slog.String("skipped_by_reason", formatTally(timing.skippedByReason)),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
