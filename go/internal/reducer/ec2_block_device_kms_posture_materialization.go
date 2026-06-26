// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
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

func ec2BlockDeviceKMSPostureMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainEC2BlockDeviceKMSPostureMaterialization,
		Summary: "derive EC2 block-device KMS posture and set EC2 CloudResource properties",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "ec2_block_device_kms_posture_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

const (
	ec2BlockDeviceKMSPostureEvidenceSource             = "reducer/ec2-block-device-kms-posture"
	ec2BlockDeviceKMSPostureAWSResourceEntityKeyPrefix = "aws_resource_materialization:"
	ec2BlockDeviceKMSPostureInstanceEntityKeyPrefix    = "ec2_instance_node_materialization:"
)

// EC2BlockDeviceKMSPostureNodeWriter persists and retracts reducer-owned EC2
// block-device KMS posture properties on existing EC2 CloudResource nodes.
// Implementations MUST match by uid and MUST NOT create CloudResource nodes.
type EC2BlockDeviceKMSPostureNodeWriter interface {
	WriteEC2BlockDeviceKMSPostureNodes(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractEC2BlockDeviceKMSPostureNodes(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// EC2BlockDeviceKMSPostureMaterializationHandler reduces one EC2 block-device
// KMS posture follow-up into conservative node properties on existing EC2
// CloudResource nodes. It gates on both EC2 instance-node readiness and
// EBS/KMS CloudResource readiness before loading facts, so missing node
// substrates retry instead of producing missed or fabricated posture truth.
type EC2BlockDeviceKMSPostureMaterializationHandler struct {
	FactLoader FactLoader
	NodeWriter EC2BlockDeviceKMSPostureNodeWriter
	// ReadinessLookup reports whether the EC2 instance node phase and EBS/KMS
	// CloudResource node phase are committed. A nil lookup keeps tests light;
	// production wires the durable Postgres gate.
	ReadinessLookup GraphProjectionReadinessLookup
	// PriorGenerationCheck reports whether a scope has prior rows to retract.
	// Nil keeps retract behavior conservative.
	PriorGenerationCheck PriorGenerationCheck
	Tracer               trace.Tracer
	Instruments          *telemetry.Instruments
}

func ec2BlockDeviceKMSPostureFactKinds() []string {
	return []string{
		facts.AWSResourceFactKind,
		facts.AWSRelationshipFactKind,
		facts.EC2InstancePostureFactKind,
	}
}

// Handle executes one EC2 block-device KMS posture materialization intent.
func (h EC2BlockDeviceKMSPostureMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainEC2BlockDeviceKMSPostureMaterialization {
		return Result{}, fmt.Errorf("ec2 block-device KMS posture materialization handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("ec2 block-device KMS posture materialization fact loader is required")
	}
	if h.NodeWriter == nil {
		return Result{}, fmt.Errorf("ec2 block-device KMS posture materialization node writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerEC2BlockDeviceKMSPostureMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	if missing, ok := h.firstMissingNodePhase(intent); !ok {
		return Result{}, ec2BlockDeviceKMSPostureNodesNotReadyError{
			scopeID:         intent.ScopeID,
			generationID:    intent.GenerationID,
			missingPhaseFor: missing,
		}
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, ec2BlockDeviceKMSPostureFactKinds())
	if err != nil {
		return Result{}, fmt.Errorf("load facts for ec2 block-device KMS posture materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, relationshipEnvelopes, postureEnvelopes := splitEC2BlockDeviceKMSPostureEnvelopes(envelopes)
	extractStart := time.Now()
	rows, tally := ExtractEC2BlockDeviceKMSPostureRows(resourceEnvelopes, relationshipEnvelopes, postureEnvelopes)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.NodeWriter.RetractEC2BlockDeviceKMSPostureNodes(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			ec2BlockDeviceKMSPostureEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical ec2 block-device KMS posture properties: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.NodeWriter.WriteEC2BlockDeviceKMSPostureNodes(
			ctx,
			rows,
			intent.ScopeID,
			intent.GenerationID,
			ec2BlockDeviceKMSPostureEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical ec2 block-device KMS posture properties: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	h.recordDecisionCounters(ctx, tally)
	logEC2BlockDeviceKMSPostureMaterializationCompleted(ctx, ec2BlockDeviceKMSPostureTiming{
		intent:            intent,
		resourceCount:     len(resourceEnvelopes),
		relationshipCount: len(relationshipEnvelopes),
		postureCount:      len(postureEnvelopes),
		rowCount:          len(rows),
		decisions:         tally.decisions,
		reasons:           tally.reasons,
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
		Domain:   DomainEC2BlockDeviceKMSPostureMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d EC2 block-device KMS posture row(s) from %d posture fact(s); %d posture fact(s) skipped",
			len(rows),
			len(postureEnvelopes),
			tally.totalSkipped(),
		),
		CanonicalWrites: len(rows),
	}, nil
}

func (h EC2BlockDeviceKMSPostureMaterializationHandler) firstMissingNodePhase(intent Intent) (string, bool) {
	if h.ReadinessLookup == nil {
		return "", true
	}
	scopeID := strings.TrimSpace(intent.ScopeID)
	generationID := strings.TrimSpace(intent.GenerationID)
	if scopeID == "" || generationID == "" {
		return "", false
	}
	checks := []struct {
		label     string
		entityKey string
	}{
		{label: "ec2_instance_node", entityKey: ec2BlockDeviceKMSPostureInstanceEntityKeyPrefix + scopeID},
		{label: "ebs_kms_resource_node", entityKey: ec2BlockDeviceKMSPostureAWSResourceEntityKeyPrefix + scopeID},
	}
	for _, check := range checks {
		key := GraphProjectionPhaseKey{
			ScopeID:          scopeID,
			AcceptanceUnitID: check.entityKey,
			SourceRunID:      generationID,
			GenerationID:     generationID,
			Keyspace:         GraphProjectionKeyspaceCloudResourceUID,
		}
		ready, found := h.ReadinessLookup(key, GraphProjectionPhaseCanonicalNodesCommitted)
		if !found || !ready {
			return check.label, false
		}
	}
	return "", true
}

func (h EC2BlockDeviceKMSPostureMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for ec2 block-device KMS posture retract: %w", err)
	}
	return !hasPrior, nil
}

func (h EC2BlockDeviceKMSPostureMaterializationHandler) recordDecisionCounters(
	ctx context.Context,
	tally ec2BlockDeviceKMSPostureTally,
) {
	if h.Instruments == nil {
		return
	}
	if h.Instruments.EC2BlockDeviceKMSPostureDecisions != nil {
		for key, count := range tally.decisionReasons {
			h.Instruments.EC2BlockDeviceKMSPostureDecisions.Add(ctx, int64(count), metric.WithAttributes(
				telemetry.AttrOutcome(key.outcome),
				telemetry.AttrReason(key.reason),
			))
		}
	}
	if h.Instruments.EC2BlockDeviceKMSPostureSkipped != nil {
		for reason, count := range tally.skipped {
			h.Instruments.EC2BlockDeviceKMSPostureSkipped.Add(ctx, int64(count), metric.WithAttributes(
				telemetry.AttrSkipReason(reason),
			))
		}
	}
}

func splitEC2BlockDeviceKMSPostureEnvelopes(envelopes []facts.Envelope) (resources, relationships, postures []facts.Envelope) {
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources = append(resources, env)
		case facts.AWSRelationshipFactKind:
			relationships = append(relationships, env)
		case facts.EC2InstancePostureFactKind:
			postures = append(postures, env)
		}
	}
	return resources, relationships, postures
}

type ec2BlockDeviceKMSPostureNodesNotReadyError struct {
	scopeID         string
	generationID    string
	missingPhaseFor string
}

func (e ec2BlockDeviceKMSPostureNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical %s nodes not committed for scope %s generation %s",
		e.missingPhaseFor,
		e.scopeID,
		e.generationID,
	)
}

func (ec2BlockDeviceKMSPostureNodesNotReadyError) Retryable() bool { return true }

func (ec2BlockDeviceKMSPostureNodesNotReadyError) FailureClass() string {
	return "ec2_block_device_kms_posture_nodes_not_ready"
}

type ec2BlockDeviceKMSPostureTiming struct {
	intent            Intent
	resourceCount     int
	relationshipCount int
	postureCount      int
	rowCount          int
	decisions         map[string]int
	reasons           map[string]int
	skippedByReason   map[string]int
	skipRetract       bool
	loadDuration      time.Duration
	extractDuration   time.Duration
	retractDuration   time.Duration
	writeDuration     time.Duration
	totalDuration     time.Duration
}

func logEC2BlockDeviceKMSPostureMaterializationCompleted(
	ctx context.Context,
	timing ec2BlockDeviceKMSPostureTiming,
) {
	slog.InfoContext(
		ctx, "ec2 block-device KMS posture materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.resourceCount),
		slog.Int("relationship_fact_count", timing.relationshipCount),
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
