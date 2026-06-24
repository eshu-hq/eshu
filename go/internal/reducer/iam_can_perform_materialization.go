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

// iamCanPerformEvidenceSource tags the CAN_PERFORM edges this reducer writes so
// the prior-generation retract scopes its delete to reducer-owned CAN_PERFORM
// edges and never touches edges or nodes owned by other writers.
const iamCanPerformEvidenceSource = "reducer/iam-can-perform"

// iamCanPerformMaterializationDomainDefinition returns the additive definition for
// the IAM CAN_PERFORM effective-permission edge projection. It is additive (not
// part of DefaultDomainDefinitions) because the handler requires an explicitly
// wired IAMCanPerformEdgeWriter and FactLoader; registering it without them would
// silently drop every CAN_PERFORM intent. See issue #1134 PR4a.
func iamCanPerformMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainIAMCanPerformMaterialization,
		Summary: "project merged aws_iam_permission and aws_resource_policy_permission facts into conservative IAM CAN_PERFORM effective-permission edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "iam_can_perform_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// IAMCanPerformEdgeWriter persists and retracts the IAM CAN_PERFORM
// effective-permission graph: idempotent CAN_PERFORM edges between an IAM
// principal :CloudResource node and the resource :CloudResource node an identity
// policy grants a catalogued sensitive action on. Implementations MUST be
// idempotent by (principal uid, CAN_PERFORM, resource uid) so reducer retries and
// duplicate facts converge, and MUST NOT fabricate endpoint nodes: a row whose
// principal or resource node is absent is a no-op.
type IAMCanPerformEdgeWriter interface {
	WriteIAMCanPerformEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractIAMCanPerformEdges(ctx context.Context, scopeIDs []string, generationID, evidenceSource string) error
}

// iamCanPerformGateKeyspaces are the canonical-nodes keyspaces the edge domain
// gates on. A CAN_PERFORM edge may only resolve once both the IAM principal and
// the target resource CloudResource nodes have committed for this scope
// generation. Both endpoints live in the shared cloud_resource_uid keyspace, so a
// single gate covers both — CAN_PERFORM is edge-only and introduces no new
// keyspace.
var iamCanPerformGateKeyspaces = []GraphProjectionKeyspace{
	GraphProjectionKeyspaceCloudResourceUID,
}

// IAMCanPerformMaterializationHandler reduces one IAM CAN_PERFORM follow-up into
// the CAN_PERFORM graph. It gates on the cloud_resource_uid canonical-nodes phase,
// loads the scope generation's aws_resource, aws_iam_permission,
// aws_iam_permission_boundary, and aws_resource_policy_permission facts, resolves
// trusted-Allow identity statements and exact resource-policy grantees against
// the closed CAN_PERFORM catalog through a bounded in-memory ARN join index (no
// per-edge graph round trip), intersects identity grants with permissions-boundary
// evidence when present, writes the resolved edges, and counts skipped
// evaluations instead of dropping them silently. Every edge carries grant_sources
// and an evaluation_scope honesty label that distinguishes identity-policy,
// boundary-evaluated identity-policy, resource-policy, and both-source grants.
type IAMCanPerformMaterializationHandler struct {
	FactLoader FactLoader
	Writer     IAMCanPerformEdgeWriter
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

// Handle executes one IAM CAN_PERFORM materialization intent.
func (h IAMCanPerformMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainIAMCanPerformMaterialization {
		return Result{}, fmt.Errorf(
			"iam can_perform materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("iam can_perform materialization fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("iam can_perform materialization writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerIAMCanPerformMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	// Readiness gate: CAN_PERFORM edges may only resolve against CloudResource nodes
	// the same generation already committed. If the cloud_resource_uid
	// canonical-nodes phase is not yet published, the intent re-enters the durable
	// queue (retryable) rather than writing edges against a node set that does not
	// exist yet.
	if notReady := h.firstNotReadyKeyspace(intent); notReady != "" {
		return Result{}, iamCanPerformNotReadyError{
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
		[]string{
			facts.AWSResourceFactKind,
			facts.AWSIAMPermissionFactKind,
			facts.AWSIAMPermissionBoundaryFactKind,
			facts.AWSResourcePolicyPermissionFactKind,
		},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for iam can_perform materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, permissionEnvelopes, permissionBoundaryEnvelopes, resourcePolicyEnvelopes := splitIAMCanPerformEnvelopes(envelopes)
	permissionInputs := append([]facts.Envelope{}, permissionEnvelopes...)
	permissionInputs = append(permissionInputs, permissionBoundaryEnvelopes...)

	extractStart := time.Now()
	result := ExtractIAMCanPerformEdges(resourceEnvelopes, permissionInputs, resourcePolicyEnvelopes)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.Writer.RetractIAMCanPerformEdges(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			iamCanPerformEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical iam can_perform edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	writeStart := time.Now()
	if len(result.Edges) > 0 {
		if err := h.Writer.WriteIAMCanPerformEdges(ctx, result.Edges, intent.ScopeID, intent.GenerationID, iamCanPerformEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical iam can_perform edges: %w", err)
		}
	}
	writeDuration := time.Since(writeStart)

	h.recordTally(ctx, result)
	logIAMCanPerformCompleted(ctx, iamCanPerformTiming{
		intent:                  intent,
		resourceCount:           len(resourceEnvelopes),
		permissionCount:         len(permissionEnvelopes),
		permissionBoundaryCount: len(permissionBoundaryEnvelopes),
		resourcePolicyCount:     len(resourcePolicyEnvelopes),
		edgeCount:               len(result.Edges),
		tally:                   result.Tally,
		skipRetract:             skipRetract,
		loadDuration:            loadDuration,
		extractDuration:         extractDuration,
		retractDuration:         retractDuration,
		writeDuration:           writeDuration,
		totalDuration:           time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainIAMCanPerformMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d CAN_PERFORM edge(s) from %d iam permission fact(s), %d permission boundary fact(s), and %d resource policy permission fact(s); %d skipped, %d conditioned provenance-only",
			len(result.Edges),
			len(permissionEnvelopes),
			len(permissionBoundaryEnvelopes),
			len(resourcePolicyEnvelopes),
			result.Tally.total(),
			result.Tally.conditionedProvenanceOnly,
		),
		CanonicalWrites: len(result.Edges),
	}, nil
}

// firstNotReadyKeyspace returns the first gate keyspace whose
// canonical-nodes-committed phase is not yet published for this intent's scope
// generation, or "" when ready. A nil ReadinessLookup keeps the gate open for test
// wiring.
func (h IAMCanPerformMaterializationHandler) firstNotReadyKeyspace(intent Intent) GraphProjectionKeyspace {
	if h.ReadinessLookup == nil {
		return ""
	}
	now := time.Now().UTC()
	for _, keyspace := range iamCanPerformGateKeyspaces {
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

// shouldSkipRetract mirrors the IAM escalation and AWS relationship edge domains:
// skip the prior-edge retract on the very first generation for a scope (no prior
// edges to remove) and only on the first attempt, so a retried attempt still cleans
// up a partial prior write.
func (h IAMCanPerformMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for iam can_perform retract: %w", err)
	}
	return !hasPrior, nil
}

// recordTally emits the CAN_PERFORM edge counters: edges committed keyed by
// resolution_mode, skipped evaluations split by skip_reason, and condition-gated
// evidence split by bounded confidence. Each reason/confidence is recorded even
// at zero so the time series exists and an operator can chart a rising rate from
// zero.
func (h IAMCanPerformMaterializationHandler) recordTally(ctx context.Context, result IAMCanPerformResult) {
	if h.Instruments == nil {
		return
	}
	if h.Instruments.IAMCanPerformEdges != nil {
		h.recordEdgeMode(ctx, iamCanPerformResolutionExactARN, result.EdgesByMode[iamCanPerformResolutionExactARN])
		h.recordEdgeMode(ctx, iamCanPerformResolutionSingleGlob, result.EdgesByMode[iamCanPerformResolutionSingleGlob])
	}
	if h.Instruments.IAMCanPerformSkipped != nil {
		h.recordSkip(ctx, iamCanPerformSkipUncatalogued, result.Tally.skippedUncatalogued)
		h.recordSkip(ctx, iamCanPerformSkipAmbiguous, result.Tally.skippedAmbiguous)
		h.recordSkip(ctx, iamCanPerformSkipUnresolved, result.Tally.skippedUnresolved)
		h.recordSkip(ctx, iamCanPerformSkipDeny, result.Tally.skippedDeny)
		h.recordSkip(ctx, iamCanPerformSkipConditioned, result.Tally.skippedConditioned)
		h.recordSkip(ctx, iamCanPerformSkipNotActionResource, result.Tally.skippedNotActionResource)
		h.recordSkip(ctx, iamCanPerformSkipSelfLoop, result.Tally.skippedSelfLoop)
		h.recordSkip(ctx, iamCanPerformSkipPermissionBoundary, result.Tally.skippedPermissionBoundary)
	}
	if h.Instruments.IAMCanPerformConditioned != nil {
		h.recordConditionConfidence(
			ctx,
			iamCanPerformConditionConfidenceProvenanceOnly,
			result.Tally.conditionedProvenanceOnly,
		)
	}
}

// recordEdgeMode emits one resolution_mode edge data point. A zero count is still
// recorded so the time series exists for both modes.
func (h IAMCanPerformMaterializationHandler) recordEdgeMode(ctx context.Context, mode string, count int) {
	h.Instruments.IAMCanPerformEdges.Add(ctx, int64(count), metric.WithAttributes(
		telemetry.AttrResolutionMode(mode),
	))
}

// recordSkip emits one skip-reason data point. A zero count is still recorded so
// the time series exists and an operator can chart a rising skip rate from zero.
func (h IAMCanPerformMaterializationHandler) recordSkip(ctx context.Context, reason string, count int) {
	h.Instruments.IAMCanPerformSkipped.Add(ctx, int64(count), metric.WithAttributes(
		telemetry.AttrSkipReason(reason),
	))
}

// recordConditionConfidence emits one condition-confidence data point. A zero
// count is still recorded so the provenance-only time series exists.
func (h IAMCanPerformMaterializationHandler) recordConditionConfidence(ctx context.Context, confidence string, count int) {
	h.Instruments.IAMCanPerformConditioned.Add(ctx, int64(count), metric.WithAttributes(
		telemetry.AttrConfidence(confidence),
	))
}

// splitIAMCanPerformEnvelopes partitions a mixed envelope slice in one pass so
// the join index, identity/boundary permission facts, boundary attachment facts,
// and resource-policy facts are built from a single bounded load.
func splitIAMCanPerformEnvelopes(envelopes []facts.Envelope) (resources, permissions, permissionBoundaries, resourcePolicies []facts.Envelope) {
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources = append(resources, env)
		case facts.AWSIAMPermissionFactKind:
			permissions = append(permissions, env)
		case facts.AWSIAMPermissionBoundaryFactKind:
			permissionBoundaries = append(permissionBoundaries, env)
		case facts.AWSResourcePolicyPermissionFactKind:
			resourcePolicies = append(resourcePolicies, env)
		}
	}
	return resources, permissions, permissionBoundaries, resourcePolicies
}

// iamCanPerformNotReadyError marks a readiness-gate miss as retryable so the
// durable queue re-runs the intent once the CloudResource nodes commit, instead of
// failing terminally or writing edges against absent nodes.
type iamCanPerformNotReadyError struct {
	scopeID      string
	generationID string
	keyspace     GraphProjectionKeyspace
}

func (e iamCanPerformNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical nodes not committed on keyspace %s for scope %s generation %s",
		e.keyspace, e.scopeID, e.generationID,
	)
}

func (iamCanPerformNotReadyError) Retryable() bool { return true }

func (iamCanPerformNotReadyError) FailureClass() string {
	return "iam_can_perform_nodes_not_ready"
}

// iamCanPerformTiming groups stage durations and the resolution tally so the
// completion log identifies fact-load, extraction, retract, and graph-write time,
// plus why catalog-action evaluations lost edges.
type iamCanPerformTiming struct {
	intent                  Intent
	resourceCount           int
	permissionCount         int
	permissionBoundaryCount int
	resourcePolicyCount     int
	edgeCount               int
	tally                   iamCanPerformTally
	skipRetract             bool
	loadDuration            time.Duration
	extractDuration         time.Duration
	retractDuration         time.Duration
	writeDuration           time.Duration
	totalDuration           time.Duration
}

func logIAMCanPerformCompleted(ctx context.Context, timing iamCanPerformTiming) {
	slog.InfoContext(
		ctx, "iam can_perform materialization completed",
		slog.String(telemetry.LogKeyScopeID, timing.intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, timing.intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.resourceCount),
		slog.Int("iam_permission_fact_count", timing.permissionCount),
		slog.Int("permission_boundary_fact_count", timing.permissionBoundaryCount),
		slog.Int("resource_policy_permission_fact_count", timing.resourcePolicyCount),
		slog.Int("can_perform_edge_count", timing.edgeCount),
		slog.Int("skipped_uncatalogued_action", timing.tally.skippedUncatalogued),
		slog.Int("skipped_ambiguous", timing.tally.skippedAmbiguous),
		slog.Int("skipped_unresolved", timing.tally.skippedUnresolved),
		slog.Int("skipped_deny", timing.tally.skippedDeny),
		slog.Int("skipped_conditioned", timing.tally.skippedConditioned),
		slog.Int("conditioned_provenance_only", timing.tally.conditionedProvenanceOnly),
		slog.Int("skipped_not_action_resource", timing.tally.skippedNotActionResource),
		slog.Int("skipped_self_loop", timing.tally.skippedSelfLoop),
		slog.Int("skipped_permission_boundary", timing.tally.skippedPermissionBoundary),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
