// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// iamCanPerformEvidenceSource tags the CAN_PERFORM edges this reducer writes so
// the prior-generation retract scopes its delete to reducer-owned CAN_PERFORM
// edges and never touches edges or nodes owned by other writers.
const iamCanPerformEvidenceSource = "reducer/iam-can-perform"

// PerformMaterializationDomainDefinition returns the additive definition for
// the IAM CAN_PERFORM effective-permission edge projection. It is additive (not
// part of DefaultDomainDefinitions) because the handler requires an explicitly
// wired IAMCanPerformEdgeWriter and FactLoader; registering it without them would
// silently drop every CAN_PERFORM intent. See issue #1134 PR4a.
func PerformMaterializationDomainDefinition() reducercontract.DomainDefinition {
	return reducercontract.DomainDefinition{
		Domain:  reducercontract.DomainIAMCanPerformMaterialization,
		Summary: "project merged aws_iam_permission and aws_resource_policy_permission facts into conservative IAM CAN_PERFORM effective-permission edges",
		Ownership: reducercontract.OwnershipShape{
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
var iamCanPerformGateKeyspaces = []gpphase.Keyspace{
	gpphase.KeyspaceCloudResourceUID,
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
	FactLoader factload.FactLoader
	Writer     IAMCanPerformEdgeWriter
	// ReadinessLookup reports whether a canonical-nodes-committed phase has been
	// published for the intent's scope generation on a given keyspace. A nil lookup
	// keeps the gate open (test wiring); production wires the durable Postgres
	// lookup.
	ReadinessLookup gpphase.ReadinessLookup
	// PriorGenerationCheck reports whether the scope has any prior generation. Nil
	// keeps retract behavior conservative (always retract before write).
	PriorGenerationCheck reducercontract.PriorGenerationCheck
	// CrossScopeTargets resolves exact identity-policy target ARNs from the
	// sibling AWS service scopes of the same account, where the awscloud
	// collector emits every catalog target (#6785). Nil keeps resolution inside
	// the intent's own scope (test wiring).
	CrossScopeTargets CrossScopeTargetLoader
	// ReadinessWaits is the (scope, domain) readiness-wait ledger. It anchors
	// the cross-scope wait across superseding generations and records the last
	// partial commit so an unchanged poll writes nothing (#6785). Nil treats
	// every evaluation as the first of its queue cycle, anchored at the claim's
	// cycle start (test wiring); production wires the Postgres ledger.
	ReadinessWaits crossscope.ReadinessWaitLedger
	// ReadinessMaxWait bounds the wait since the first defer. Zero means
	// crossscope.ProducerReadinessMaxWait.
	ReadinessMaxWait time.Duration
	// Now is the handler clock. Nil means time.Now.
	Now         func() time.Time
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
}

// Handle executes one IAM CAN_PERFORM materialization intent.
func (h IAMCanPerformMaterializationHandler) Handle(
	ctx context.Context,
	intent reducercontract.Intent,
) (reducercontract.Result, error) {
	totalStart := time.Now()
	if intent.Domain != reducercontract.DomainIAMCanPerformMaterialization {
		return reducercontract.Result{}, fmt.Errorf(
			"iam can_perform materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return reducercontract.Result{}, fmt.Errorf("iam can_perform materialization fact loader is required")
	}
	if h.Writer == nil {
		return reducercontract.Result{}, fmt.Errorf("iam can_perform materialization writer is required")
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
		return reducercontract.Result{}, iamCanPerformNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
			keyspace:     notReady,
		}
	}

	// Commit first, then wait (#6785). The readiness-wait row is read before
	// the fact load so an unchanged poll can skip the load entirely.
	existingWait, waitFound, err := h.readCrossScopeWait(ctx, intent)
	if err != nil {
		return reducercontract.Result{}, err
	}
	if crossscope.PollEligible(existingWait, waitFound && h.ReadinessWaits != nil, intent.GenerationID, intent.CycleStartedAt) {
		handled, err := h.pollCrossScopeWait(ctx, intent, existingWait)
		if handled {
			if err != nil {
				return reducercontract.Result{}, err
			}
			return settledResult(intent), nil
		}
	}

	loadStart := time.Now()
	envelopes, err := factload.LoadFactsForKinds(
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
		return reducercontract.Result{}, fmt.Errorf("load facts for iam can_perform materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, permissionEnvelopes, permissionBoundaryEnvelopes, resourcePolicyEnvelopes := splitIAMCanPerformEnvelopes(envelopes)
	permissionInputs := append([]facts.Envelope{}, permissionEnvelopes...)
	permissionInputs = append(permissionInputs, permissionBoundaryEnvelopes...)

	crossScope, err := h.resolveCrossScopeTargets(ctx, intent, permissionEnvelopes)
	if err != nil {
		return reducercontract.Result{}, err
	}
	decision := crossscope.DecideWait(h.waitInput(intent, existingWait, waitFound, crossScope.missing))

	extractStart := time.Now()
	result, err := extractIAMCanPerformEdges(resourceEnvelopes, crossScope.resources, permissionInputs, resourcePolicyEnvelopes)
	if err != nil {
		// A non-decode error (transient fact-load, unsupported major, or other
		// fatal condition factdecode.PartitionDecodeFailures did NOT quarantine) fails the
		// whole intent so the durable queue triages it correctly.
		return reducercontract.Result{}, err
	}
	// Per-fact isolation: a malformed aws_resource/aws_iam_permission/
	// aws_resource_policy_permission fact (a missing required identity field) is
	// quarantined as a visible input_invalid dead-letter — counter + structured
	// error log — while the batch's valid facts still project below.
	inputInvalidCount := factdecode.RecordQuarantinedFacts(ctx, h.Instruments, reducercontract.DomainIAMCanPerformMaterialization, intent.ScopeID, intent.GenerationID, result.Quarantined)
	extractDuration := time.Since(extractStart)

	// The commit is the scope-wide retract plus rewrite, skipped only when the
	// ledger shows this generation, queue cycle, and missing set already
	// committed. Missing targets are left out of the rewrite (unresolved).
	var commit iamCanPerformCommit
	if decision.Commit {
		if commit, err = h.commitEdges(ctx, intent, result.Edges); err != nil {
			return reducercontract.Result{}, err
		}
		h.recordTally(ctx, result)
		if h.CrossScopeTargets != nil {
			h.recordCrossScopeOutcomes(ctx, settledOutcomes(crossScope.outcomes, decision))
		}
	}
	// The ledger is written only after the graph commit: a crash between the
	// two re-commits once, idempotently, and never skips a commit.
	if err := crossscope.ApplyWaitDecision(ctx, h.ReadinessWaits, decision, intent.ScopeID, reducercontract.DomainIAMCanPerformMaterialization); err != nil {
		return reducercontract.Result{}, err
	}
	h.reportWait(ctx, intent, decision, crossScope.missing, decision.Commit)

	logIAMCanPerformCompleted(ctx, iamCanPerformTiming{
		intent:                  intent,
		resourceCount:           len(resourceEnvelopes),
		permissionCount:         len(permissionEnvelopes),
		permissionBoundaryCount: len(permissionBoundaryEnvelopes),
		resourcePolicyCount:     len(resourcePolicyEnvelopes),
		edgeCount:               commit.writes,
		tally:                   result.Tally,
		skipRetract:             commit.skipRetract || !decision.Commit,
		loadDuration:            loadDuration,
		extractDuration:         extractDuration,
		retractDuration:         commit.retractDuration,
		writeDuration:           commit.writeDuration,
		totalDuration:           time.Since(totalStart),
	})
	if decision.Defer {
		return reducercontract.Result{}, iamCanPerformTargetNotReadyError{
			scopeID: intent.ScopeID, generationID: intent.GenerationID, notReady: len(crossScope.missing),
		}
	}

	return reducercontract.Result{
		IntentID: intent.IntentID,
		Domain:   reducercontract.DomainIAMCanPerformMaterialization,
		Status:   reducercontract.ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d CAN_PERFORM edge(s) from %d iam permission fact(s), %d permission boundary fact(s), and %d resource policy permission fact(s); %d skipped, %d conditioned provenance-only, %d input_invalid fact(s) quarantined, %d cross-scope target(s) still missing",
			commit.writes,
			len(permissionEnvelopes),
			len(permissionBoundaryEnvelopes),
			len(resourcePolicyEnvelopes),
			result.Tally.total(),
			result.Tally.conditionedProvenanceOnly,
			inputInvalidCount,
			len(crossScope.missing),
		),
		CanonicalWrites: commit.writes,
		SubSignals:      factdecode.InputInvalidSubSignals(inputInvalidCount),
	}, nil
}

// iamCanPerformCommit reports what one scope-wide commit did.
type iamCanPerformCommit struct {
	skipRetract     bool
	writes          int
	retractDuration time.Duration
	writeDuration   time.Duration
}

// commitEdges is the scope-wide retract plus rewrite. Missing cross-scope
// targets are simply absent from edges (unresolved). Within one generation
// the edge set only grows as the missing set shrinks, so skipping the retract
// on a first-generation re-commit (AttemptCount frozen at 1 by the
// non-counting class) is safe.
func (h IAMCanPerformMaterializationHandler) commitEdges(
	ctx context.Context,
	intent reducercontract.Intent,
	edges []map[string]any,
) (iamCanPerformCommit, error) {
	var commit iamCanPerformCommit
	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return commit, err
	}
	commit.skipRetract = skipRetract
	if !skipRetract {
		retractStart := time.Now()
		if err := h.Writer.RetractIAMCanPerformEdges(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			iamCanPerformEvidenceSource,
		); err != nil {
			return commit, fmt.Errorf("retract canonical iam can_perform edges: %w", err)
		}
		commit.retractDuration = time.Since(retractStart)
	}
	writeStart := time.Now()
	if len(edges) > 0 {
		if err := h.Writer.WriteIAMCanPerformEdges(ctx, edges, intent.ScopeID, intent.GenerationID, iamCanPerformEvidenceSource); err != nil {
			return commit, fmt.Errorf("write canonical iam can_perform edges: %w", err)
		}
	}
	commit.writeDuration = time.Since(writeStart)
	commit.writes = len(edges)
	return commit, nil
}

// firstNotReadyKeyspace returns the first gate keyspace whose
// canonical-nodes-committed phase is not yet published for this intent's scope
// generation, or "" when ready. A nil ReadinessLookup keeps the gate open for test
// wiring.
func (h IAMCanPerformMaterializationHandler) firstNotReadyKeyspace(intent reducercontract.Intent) gpphase.Keyspace {
	if h.ReadinessLookup == nil {
		return ""
	}
	for _, keyspace := range iamCanPerformGateKeyspaces {
		key, ok := gpphase.KeyFromScope(intent.ScopeID, intent.GenerationID, intent.EntityKeys, keyspace)
		if !ok {
			return keyspace
		}
		ready, found := h.ReadinessLookup(key, gpphase.PhaseCanonicalNodesCommitted)
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
func (h IAMCanPerformMaterializationHandler) shouldSkipRetract(ctx context.Context, intent reducercontract.Intent) (bool, error) {
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
	keyspace     gpphase.Keyspace
}

func (e iamCanPerformNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical nodes not committed on keyspace %s for scope %s generation %s",
		e.keyspace, e.scopeID, e.generationID,
	)
}

func (iamCanPerformNotReadyError) Retryable() bool { return true }

// IAMCanPerformNodesNotReadyFailureClass identifies an in-handler readiness-gate miss: the
// IAM can-perform edge intent ran before its upstream cloud-resource
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
const IAMCanPerformNodesNotReadyFailureClass = "iam_can_perform_nodes_not_ready"

func (iamCanPerformNotReadyError) FailureClass() string {
	return IAMCanPerformNodesNotReadyFailureClass
}
