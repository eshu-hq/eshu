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

// iamEscalationEvidenceSource tags the CAN_ESCALATE_TO edges this reducer writes so
// the prior-generation retract scopes its delete to reducer-owned escalation edges
// and never touches edges or nodes owned by other writers.
const iamEscalationEvidenceSource = "reducer/iam-escalation"

// iamEscalationMaterializationDomainDefinition returns the additive definition for
// the IAM privilege-escalation edge projection. It is additive (not part of
// DefaultDomainDefinitions) because the handler requires an explicitly wired
// IAMEscalationEdgeWriter and FactLoader; registering it without them would
// silently drop every escalation intent. See issue #1134.
func iamEscalationMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainIAMEscalationMaterialization,
		Summary: "project merged aws_iam_permission facts into conservative IAM CAN_ESCALATE_TO privilege-escalation edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "iam_escalation_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// IAMEscalationEdgeWriter persists and retracts the IAM privilege-escalation
// graph: idempotent CAN_ESCALATE_TO edges between IAM principal and target
// CloudResource nodes. Implementations MUST be idempotent by
// (principal uid, CAN_ESCALATE_TO, target uid) so reducer retries and duplicate
// facts converge, and MUST NOT fabricate endpoint nodes: a row whose principal or
// target node is absent is a no-op.
type IAMEscalationEdgeWriter interface {
	WriteIAMEscalationEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractIAMEscalationEdges(ctx context.Context, scopeIDs []string, generationID, evidenceSource string) error
}

// iamEscalationGateKeyspaces are the canonical-nodes keyspaces the edge domain
// gates on. An escalation edge may only resolve once the IAM principal/role/user/
// group/policy CloudResource nodes have committed for this scope generation.
var iamEscalationGateKeyspaces = []GraphProjectionKeyspace{
	GraphProjectionKeyspaceCloudResourceUID,
}

// IAMEscalationMaterializationHandler reduces one IAM privilege-escalation
// follow-up into the CAN_ESCALATE_TO graph. It gates on the cloud_resource_uid
// canonical-nodes phase, loads the scope generation's aws_resource and
// aws_iam_permission facts, evaluates each principal's escalation primitives
// against the curated catalog through a bounded in-memory ARN join index (no
// per-edge graph round trip), writes the resolved edges, and counts skipped /
// deferred primitives instead of dropping them silently.
type IAMEscalationMaterializationHandler struct {
	FactLoader FactLoader
	Writer     IAMEscalationEdgeWriter
	// ReadinessLookup reports whether a canonical-nodes-committed phase has been
	// published for the intent's scope generation on a given keyspace. A nil lookup
	// keeps the gate open (test wiring); production wires the durable Postgres
	// lookup.
	ReadinessLookup GraphProjectionReadinessLookup
	// PriorGenerationCheck reports whether the scope has any prior generation. Nil
	// keeps retract behavior conservative (always retract before write).
	PriorGenerationCheck PriorGenerationCheck
	Tracer               trace.Tracer
	Instruments          *telemetry.Instruments
}

// Handle executes one IAM escalation materialization intent.
func (h IAMEscalationMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainIAMEscalationMaterialization {
		return Result{}, fmt.Errorf(
			"iam escalation materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("iam escalation materialization fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("iam escalation materialization writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerIAMEscalationMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	// Readiness gate: escalation edges may only resolve against IAM nodes the same
	// generation already committed. If the cloud_resource_uid canonical-nodes phase
	// is not yet published, the intent re-enters the durable queue (retryable)
	// rather than writing edges against a node set that does not exist yet.
	if notReady := h.firstNotReadyKeyspace(intent); notReady != "" {
		return Result{}, iamEscalationNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
			keyspace:     notReady,
		}
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{facts.AWSResourceFactKind, facts.AWSIAMPermissionFactKind},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for iam escalation materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, permissionEnvelopes := splitIAMEscalationEnvelopes(envelopes)

	extractStart := time.Now()
	result, err := ExtractIAMEscalationEdges(resourceEnvelopes, permissionEnvelopes)
	if err != nil {
		// A malformed aws_resource payload (a missing required identity field)
		// is a classified input_invalid decode failure; dead-letter the intent
		// instead of resolving edges against an empty-string node identity.
		return Result{}, err
	}
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.Writer.RetractIAMEscalationEdges(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			iamEscalationEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical iam escalation edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	writeStart := time.Now()
	if len(result.Edges) > 0 {
		if err := h.Writer.WriteIAMEscalationEdges(ctx, result.Edges, intent.ScopeID, intent.GenerationID, iamEscalationEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical iam escalation edges: %w", err)
		}
	}
	writeDuration := time.Since(writeStart)

	h.recordTally(ctx, result)
	logIAMEscalationCompleted(ctx, iamEscalationTiming{
		intent:          intent,
		resourceCount:   len(resourceEnvelopes),
		permissionCount: len(permissionEnvelopes),
		edgeCount:       len(result.Edges),
		tally:           result.Tally,
		skipRetract:     skipRetract,
		loadDuration:    loadDuration,
		extractDuration: extractDuration,
		retractDuration: retractDuration,
		writeDuration:   writeDuration,
		totalDuration:   time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainIAMEscalationMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d CAN_ESCALATE_TO edge(s) from %d iam permission fact(s); %d skipped/deferred",
			len(result.Edges),
			len(permissionEnvelopes),
			result.Tally.total(),
		),
		CanonicalWrites: len(result.Edges),
	}, nil
}

// firstNotReadyKeyspace returns the first gate keyspace whose
// canonical-nodes-committed phase is not yet published for this intent's scope
// generation, or "" when ready. A nil ReadinessLookup keeps the gate open for test
// wiring.
func (h IAMEscalationMaterializationHandler) firstNotReadyKeyspace(intent Intent) GraphProjectionKeyspace {
	if h.ReadinessLookup == nil {
		return ""
	}
	now := time.Now().UTC()
	for _, keyspace := range iamEscalationGateKeyspaces {
		state, ok := graphProjectionPhaseStateForIntent(intent, keyspace, GraphProjectionPhaseCanonicalNodesCommitted, now)
		if !ok {
			return keyspace
		}
		ready, found := h.ReadinessLookup(state.Key, GraphProjectionPhaseCanonicalNodesCommitted)
		if !found || !ready {
			return keyspace
		}
	}
	return ""
}

// shouldSkipRetract mirrors the AWS relationship and reachability edge domains:
// skip the prior-edge retract on the very first generation for a scope (no prior
// edges to remove) and only on the first attempt, so a retried attempt still cleans
// up a partial prior write.
func (h IAMEscalationMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for iam escalation retract: %w", err)
	}
	return !hasPrior, nil
}

// recordTally emits the escalation edge counters: edges committed, and skipped /
// deferred primitives split by skip_reason. Each reason is recorded even at zero so
// the time series exists and an operator can chart a rising skip rate from zero.
func (h IAMEscalationMaterializationHandler) recordTally(ctx context.Context, result IAMEscalationResult) {
	if h.Instruments == nil {
		return
	}
	if h.Instruments.IAMEscalationEdges != nil {
		h.Instruments.IAMEscalationEdges.Add(ctx, int64(len(result.Edges)))
	}
	if h.Instruments.IAMEscalationSkipped == nil {
		return
	}
	h.recordSkip(ctx, iamEscalationSkipAmbiguous, result.Tally.skippedAmbiguous)
	h.recordSkip(ctx, iamEscalationSkipUnresolved, result.Tally.skippedUnresolved)
	h.recordSkip(ctx, iamEscalationSkipDeny, result.Tally.skippedDeny)
	h.recordSkip(ctx, iamEscalationSkipConditioned, result.Tally.skippedConditioned)
	h.recordSkip(ctx, iamEscalationSkipNotActionResource, result.Tally.skippedNotActionResource)
	h.recordSkip(ctx, iamEscalationSkipIncomplete, result.Tally.skippedIncomplete)
	h.recordSkip(ctx, iamEscalationDeferredCanAssume, result.Tally.deferredCanAssume)
}

// recordSkip emits one skip-reason data point. A zero count is still recorded so
// the time series exists and an operator can chart a rising skip rate from zero.
func (h IAMEscalationMaterializationHandler) recordSkip(ctx context.Context, reason string, count int) {
	h.Instruments.IAMEscalationSkipped.Add(ctx, int64(count), metric.WithAttributes(
		telemetry.AttrSkipReason(reason),
	))
}

// splitIAMEscalationEnvelopes partitions a mixed envelope slice into resource and
// iam-permission facts in one pass so the join index and the permission facts are
// built from a single bounded load.
func splitIAMEscalationEnvelopes(envelopes []facts.Envelope) (resources, permissions []facts.Envelope) {
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources = append(resources, env)
		case facts.AWSIAMPermissionFactKind:
			permissions = append(permissions, env)
		}
	}
	return resources, permissions
}

// iamEscalationNotReadyError marks a readiness-gate miss as retryable so the
// durable queue re-runs the intent once the IAM CloudResource nodes commit, instead
// of failing terminally or writing edges against absent nodes.
type iamEscalationNotReadyError struct {
	scopeID      string
	generationID string
	keyspace     GraphProjectionKeyspace
}

func (e iamEscalationNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical nodes not committed on keyspace %s for scope %s generation %s",
		e.keyspace, e.scopeID, e.generationID,
	)
}

func (iamEscalationNotReadyError) Retryable() bool { return true }

func (iamEscalationNotReadyError) FailureClass() string {
	return "iam_escalation_nodes_not_ready"
}

// iamEscalationTiming groups stage durations and the resolution tally so the
// completion log identifies fact-load, extraction, retract, and graph-write time,
// plus why primitives lost edges.
type iamEscalationTiming struct {
	intent          Intent
	resourceCount   int
	permissionCount int
	edgeCount       int
	tally           iamEscalationTally
	skipRetract     bool
	loadDuration    time.Duration
	extractDuration time.Duration
	retractDuration time.Duration
	writeDuration   time.Duration
	totalDuration   time.Duration
}

func logIAMEscalationCompleted(ctx context.Context, timing iamEscalationTiming) {
	slog.InfoContext(
		ctx, "iam escalation materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.resourceCount),
		slog.Int("iam_permission_fact_count", timing.permissionCount),
		slog.Int("escalation_edge_count", timing.edgeCount),
		slog.Int("skipped_ambiguous", timing.tally.skippedAmbiguous),
		slog.Int("skipped_unresolved", timing.tally.skippedUnresolved),
		slog.Int("skipped_deny", timing.tally.skippedDeny),
		slog.Int("skipped_conditioned", timing.tally.skippedConditioned),
		slog.Int("skipped_not_action_resource", timing.tally.skippedNotActionResource),
		slog.Int("skipped_incomplete", timing.tally.skippedIncomplete),
		slog.Int("deferred_can_assume", timing.tally.deferredCanAssume),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
