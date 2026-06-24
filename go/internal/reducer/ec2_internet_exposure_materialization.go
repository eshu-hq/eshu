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

func ec2InternetExposureMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainEC2InternetExposureMaterialization,
		Summary: "derive ec2_instance_posture internet exposure and set EC2 CloudResource properties",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "ec2_internet_exposure_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

const ec2InternetExposureEvidenceSource = "reducer/ec2-internet-exposure"

// EC2InternetExposureNodeWriter persists and retracts reducer-owned EC2 internet
// exposure properties on existing EC2 CloudResource nodes. Implementations MUST
// match by uid and MUST NOT create CloudResource nodes.
type EC2InternetExposureNodeWriter interface {
	WriteEC2InternetExposureNodes(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractEC2InternetExposureNodes(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// EC2InternetExposureMaterializationHandler reduces one ec2_instance_posture
// follow-up into conservative internet-exposure properties on existing EC2
// CloudResource nodes. It gates on EC2 canonical-node readiness, loads only EC2
// posture, AWS relationship, and security-group rule facts, and never treats
// missing ENI/SG/reachability evidence as a safe false value.
type EC2InternetExposureMaterializationHandler struct {
	FactLoader FactLoader
	NodeWriter EC2InternetExposureNodeWriter
	// ReadinessLookup reports whether EC2 CloudResource nodes for this generation
	// are committed. A nil lookup keeps tests light; production wires Postgres.
	ReadinessLookup GraphProjectionReadinessLookup
	// PriorGenerationCheck reports whether a scope has prior rows to retract.
	// Nil keeps retract behavior conservative.
	PriorGenerationCheck PriorGenerationCheck
	Tracer               trace.Tracer
	Instruments          *telemetry.Instruments
}

func ec2InternetExposureFactKinds() []string {
	return []string{
		facts.EC2InstancePostureFactKind,
		facts.AWSRelationshipFactKind,
		facts.AWSSecurityGroupRuleFactKind,
	}
}

// Handle executes one EC2 internet-exposure materialization intent.
func (h EC2InternetExposureMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainEC2InternetExposureMaterialization {
		return Result{}, fmt.Errorf("ec2 internet exposure materialization handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("ec2 internet exposure materialization fact loader is required")
	}
	if h.NodeWriter == nil {
		return Result{}, fmt.Errorf("ec2 internet exposure materialization node writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerEC2InternetExposureMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	if !h.canonicalNodesReady(intent) {
		return Result{}, ec2InternetExposureNodesNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
		}
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, ec2InternetExposureFactKinds())
	if err != nil {
		return Result{}, fmt.Errorf("load facts for ec2 internet exposure materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	postureEnvelopes, relationshipEnvelopes, ruleEnvelopes := splitEC2InternetExposureEnvelopes(envelopes)
	extractStart := time.Now()
	rows, tally := ExtractEC2InternetExposureRows(postureEnvelopes, relationshipEnvelopes, ruleEnvelopes)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.NodeWriter.RetractEC2InternetExposureNodes(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			ec2InternetExposureEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical ec2 internet exposure properties: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.NodeWriter.WriteEC2InternetExposureNodes(
			ctx,
			rows,
			intent.ScopeID,
			intent.GenerationID,
			ec2InternetExposureEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical ec2 internet exposure properties: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	h.recordDecisionCounters(ctx, tally)
	logEC2InternetExposureMaterializationCompleted(ctx, ec2InternetExposureTiming{
		intent:            intent,
		postureCount:      len(postureEnvelopes),
		relationshipCount: len(relationshipEnvelopes),
		ruleCount:         len(ruleEnvelopes),
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
		Domain:   DomainEC2InternetExposureMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d EC2 internet exposure row(s) from %d posture fact(s); %d posture fact(s) skipped",
			len(rows),
			len(postureEnvelopes),
			tally.totalSkipped(),
		),
		CanonicalWrites: len(rows),
	}, nil
}

func (h EC2InternetExposureMaterializationHandler) canonicalNodesReady(intent Intent) bool {
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

func (h EC2InternetExposureMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for ec2 internet exposure retract: %w", err)
	}
	return !hasPrior, nil
}

func (h EC2InternetExposureMaterializationHandler) recordDecisionCounters(ctx context.Context, tally ec2InternetExposureTally) {
	if h.Instruments == nil {
		return
	}
	if h.Instruments.EC2InternetExposureDecisions != nil {
		for key, count := range tally.decisionReasons {
			h.Instruments.EC2InternetExposureDecisions.Add(ctx, int64(count), metric.WithAttributes(
				telemetry.AttrOutcome(key.outcome),
				telemetry.AttrReason(key.reason),
			))
		}
	}
	if h.Instruments.EC2InternetExposureSkipped != nil {
		for reason, count := range tally.skipped {
			h.Instruments.EC2InternetExposureSkipped.Add(ctx, int64(count), metric.WithAttributes(
				telemetry.AttrSkipReason(reason),
			))
		}
	}
}

func splitEC2InternetExposureEnvelopes(envelopes []facts.Envelope) (postures, relationships, rules []facts.Envelope) {
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.EC2InstancePostureFactKind:
			postures = append(postures, env)
		case facts.AWSRelationshipFactKind:
			relationships = append(relationships, env)
		case facts.AWSSecurityGroupRuleFactKind:
			rules = append(rules, env)
		}
	}
	return postures, relationships, rules
}

type ec2InternetExposureNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e ec2InternetExposureNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical ec2 cloud resource nodes not committed for scope %s generation %s",
		e.scopeID,
		e.generationID,
	)
}

func (ec2InternetExposureNodesNotReadyError) Retryable() bool { return true }

func (ec2InternetExposureNodesNotReadyError) FailureClass() string {
	return "ec2_internet_exposure_nodes_not_ready"
}

type ec2InternetExposureTiming struct {
	intent            Intent
	postureCount      int
	relationshipCount int
	ruleCount         int
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

func logEC2InternetExposureMaterializationCompleted(
	ctx context.Context,
	timing ec2InternetExposureTiming,
) {
	slog.InfoContext(
		ctx, "ec2 internet exposure materialization completed",
		slog.String(telemetry.LogKeyScopeID, timing.intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, timing.intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(timing.intent.Domain)),
		slog.Int("posture_fact_count", timing.postureCount),
		slog.Int("relationship_fact_count", timing.relationshipCount),
		slog.Int("security_group_rule_fact_count", timing.ruleCount),
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
