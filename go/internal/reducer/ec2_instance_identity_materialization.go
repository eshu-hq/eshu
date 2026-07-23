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

// ec2InstanceIdentityMaterializationDomainDefinition returns the additive
// definition for EC2 instance identity node-property projection (#5448). It is
// additive because the handler requires an explicitly wired
// EC2InstanceIdentityNodeWriter and FactLoader; registering it without them
// would silently drop every intent.
func ec2InstanceIdentityMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainEC2InstanceIdentityMaterialization,
		Summary: "project aws_ec2_instance aws_resource facts' ami_id onto canonical EC2 instance CloudResource node properties",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "ec2_instance_identity_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// ec2InstanceIdentityEvidenceSource tags CloudResource properties written by
// this reducer so retraction removes only reducer-owned EC2 identity fields —
// never the base identity/posture fields DomainEC2InstanceNodeMaterialization
// owns on the same node.
const ec2InstanceIdentityEvidenceSource = "reducer/ec2-instance-identity"

// EC2InstanceIdentityNodeWriter persists and retracts the #5448 ami_id
// property on existing EC2 instance CloudResource nodes. Implementations MUST
// be idempotent by CloudResource uid and MUST NOT fabricate nodes: a row whose
// uid is not already a materialized CloudResource node is a no-op (mirrors
// RDSPostureNodeWriter's never-create contract).
type EC2InstanceIdentityNodeWriter interface {
	WriteEC2InstanceIdentityNodes(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractEC2InstanceIdentityNodes(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// EC2InstanceIdentityMaterializationHandler reduces one EC2 instance identity
// materialization intent into CloudResource ami_id property updates. It gates
// on the cloud_resource_uid canonical-nodes phase DomainEC2InstanceNodeMaterialization
// publishes, loads the scope generation's aws_ec2_instance aws_resource facts,
// and writes only rows whose source instance was materialized as a
// CloudResource node in that generation (enforced by the writer's
// never-create contract, not by this handler).
type EC2InstanceIdentityMaterializationHandler struct {
	FactLoader FactLoader
	NodeWriter EC2InstanceIdentityNodeWriter
	// ReadinessLookup reports whether the EC2 instance node canonical-nodes
	// phase has committed. A nil lookup keeps the gate open for test wiring.
	ReadinessLookup GraphProjectionReadinessLookup
	// PriorGenerationCheck reports whether the scope has any prior generation.
	// Nil keeps retract behavior conservative (always retract before write).
	PriorGenerationCheck PriorGenerationCheck
	Tracer               trace.Tracer
	// Instruments records the eshu_dp_reducer_input_invalid_facts_total
	// counter when an aws_resource fact is quarantined as input_invalid.
	// Optional: a nil pointer skips the counter (the structured per-fact
	// error log still emits).
	Instruments *telemetry.Instruments
}

// Handle executes one EC2 instance identity materialization intent.
func (h EC2InstanceIdentityMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainEC2InstanceIdentityMaterialization {
		return Result{}, fmt.Errorf(
			"ec2 instance identity materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("ec2 instance identity materialization fact loader is required")
	}
	if h.NodeWriter == nil {
		return Result{}, fmt.Errorf("ec2 instance identity materialization node writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerEC2InstanceIdentityMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	if !h.instanceNodesReady(intent) {
		return Result{}, ec2InstanceIdentityNodesNotReadyError{
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
		[]string{facts.AWSResourceFactKind},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for ec2 instance identity materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	rows, quarantined, err := ExtractEC2InstanceIdentityNodeRows(envelopes)
	if err != nil {
		return Result{}, err
	}
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainEC2InstanceIdentityMaterialization, intent.ScopeID, intent.GenerationID, quarantined)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.NodeWriter.RetractEC2InstanceIdentityNodes(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			ec2InstanceIdentityEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical ec2 instance identity properties: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.NodeWriter.WriteEC2InstanceIdentityNodes(
			ctx,
			rows,
			intent.ScopeID,
			intent.GenerationID,
			ec2InstanceIdentityEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical ec2 instance identity properties: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	logEC2InstanceIdentityMaterializationCompleted(ctx, ec2InstanceIdentityMaterializationTiming{
		intent:          intent,
		factCount:       len(envelopes),
		rowCount:        len(rows),
		skipRetract:     skipRetract,
		loadDuration:    loadDuration,
		extractDuration: extractDuration,
		retractDuration: retractDuration,
		writeDuration:   writeDuration,
		totalDuration:   time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainEC2InstanceIdentityMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d ec2 instance identity node update(s) from %d aws resource fact(s); %d input_invalid fact(s) quarantined",
			len(rows),
			len(envelopes),
			inputInvalidCount,
		),
		CanonicalWrites: len(rows),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

// instanceNodesReady reports whether the EC2 instance node canonical-nodes
// phase has committed for this intent's scope generation. A nil ReadinessLookup
// keeps the gate open for test wiring; the durable Postgres claim gate is the
// load-bearing fence in production.
func (h EC2InstanceIdentityMaterializationHandler) instanceNodesReady(intent Intent) bool {
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

// shouldSkipRetract mirrors RDSPostureMaterializationHandler: skip the
// prior-property retract on the very first generation for a scope (no prior
// properties to remove) and only on the first attempt, so a retried attempt
// still cleans up a partial prior write.
func (h EC2InstanceIdentityMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for ec2 instance identity retract: %w", err)
	}
	return !hasPrior, nil
}

// ec2InstanceIdentityNodesNotReadyError marks the readiness-gate miss as
// retryable so the durable queue re-runs the intent once the EC2 instance node
// phase commits, instead of writing a property onto a node set that may not
// exist yet.
type ec2InstanceIdentityNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e ec2InstanceIdentityNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical ec2 instance nodes not committed for identity scope %s generation %s",
		e.scopeID,
		e.generationID,
	)
}

func (ec2InstanceIdentityNodesNotReadyError) Retryable() bool { return true }

func (ec2InstanceIdentityNodesNotReadyError) FailureClass() string {
	return "ec2_instance_identity_nodes_not_ready"
}

type ec2InstanceIdentityMaterializationTiming struct {
	intent          Intent
	factCount       int
	rowCount        int
	skipRetract     bool
	loadDuration    time.Duration
	extractDuration time.Duration
	retractDuration time.Duration
	writeDuration   time.Duration
	totalDuration   time.Duration
}

func logEC2InstanceIdentityMaterializationCompleted(
	ctx context.Context,
	timing ec2InstanceIdentityMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "ec2 instance identity materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.factCount),
		slog.Int("node_update_count", timing.rowCount),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
