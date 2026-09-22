// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package refresh

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/value"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// FixpointProjector re-runs the global value-flow fixpoint. It is satisfied
// by value.FixpointEvidenceProjector; declared here so the handler does not
// depend on the summary handler's identical declaration.
type FixpointProjector interface {
	ProjectValueFlowFixpointEvidence(ctx context.Context, scopeID, generationID string) (value.FixpointProjectionResult, error)
}

// InputsLiveness reports the #6923 value-flow refresh fence: every
// active-generation work item and shared-projection intent that still writes
// a link in the cloud-sink chain (code_function_summary,
// code_call_materialization, workload_materialization,
// workload_cloud_relationship_materialization,
// iam_can_perform_materialization, aws_resource_materialization, plus the
// runs_in/invokes_cloud_action shared intents). An empty, nil-error result
// means the chain has drained and the singleton may solve. It is satisfied
// by postgres.ValueFlowInputsLivenessStore.
type InputsLiveness interface {
	PendingValueFlowInputs(ctx context.Context) ([]string, error)
}

// Handler re-runs the global value-flow fixpoint for one refresh intent. It
// persists nothing itself: summaries, sources, and graph ids are unchanged by
// the producers whose completion enqueues the refresh (workload, USES,
// CAN_PERFORM, and aws_resource materialization), and the fixpoint reloads
// them globally before solving. The solve is idempotent (global retract then
// MERGE on evidence_uid), so re-runs converge.
//
// When InputsLiveness is wired, Handle runs its fence BEFORE any load
// (#6923): while the chain the fixpoint reads has an active-generation writer
// still nonterminal, solving would read partial graph state and produce a
// pre-convergence answer that a later re-trigger corrects — the row-set
// wobble #6923 reports. The handler refuses instead
// (value_flow_inputs_not_ready, Retryable, non-counting) until either the
// fence clears or elapsed time since the singleton's own cycle anchor
// reaches crossscope.ProducerReadinessMaxWait, at which point it solves
// anyway so a stuck producer degrades to bounded staleness rather than an
// eternal defer. Collapsing every trigger onto this one fenced singleton also
// removes the concurrent summary-vs-refresh solve race (issue #6880).
type Handler struct {
	Fixpoint       FixpointProjector
	InputsLiveness InputsLiveness
	// Now returns the handler's clock, defaulting to time.Now when unset.
	// Overridden in tests so the starvation bound is deterministic.
	Now         func() time.Time
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
}

// Handle executes one value-flow refresh intent.
func (h Handler) Handle(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	if intent.Domain != reducercontract.DomainCodeValueFlowRefresh {
		return reducercontract.Result{}, fmt.Errorf("value-flow refresh handler does not accept domain %q", intent.Domain)
	}
	if h.Fixpoint == nil {
		return reducercontract.Result{}, fmt.Errorf("value-flow refresh fixpoint projector is required")
	}
	if err := h.checkInputsLiveness(ctx, intent); err != nil {
		return reducercontract.Result{}, err
	}
	fixpoint, err := h.Fixpoint.ProjectValueFlowFixpointEvidence(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("project value-flow fixpoint evidence: %w", err)
	}
	slog.Info(
		"value-flow refresh completed",
		"scope_id", intent.ScopeID,
		"generation_id", intent.GenerationID,
		"fixpoint_finding_count", fixpoint.FindingCount,
		"fixpoint_graph_rows", fixpoint.GraphRows,
		"fixpoint_unresolved_endpoint_count", fixpoint.UnresolvedEndpointCount,
	)
	return reducercontract.Result{
		IntentID: intent.IntentID,
		Domain:   reducercontract.DomainCodeValueFlowRefresh,
		Status:   reducercontract.ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"refreshed value-flow fixpoint, projected %d fixpoint edge(s)",
			fixpoint.GraphRows,
		),
		CanonicalWrites: fixpoint.GraphRows,
	}, nil
}

// now returns the handler clock, defaulting to time.Now when unset.
func (h Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now().UTC()
}

// checkInputsLiveness runs the #6923 input fence BEFORE the fixpoint load. A
// nil InputsLiveness is a no-op (unwired seam behaves exactly as before this
// fence existed). A non-empty pending set either defers (Retryable,
// non-counting value_flow_inputs_not_ready) or, once elapsed time since the
// singleton's own cycle anchor reaches crossscope.ProducerReadinessMaxWait,
// logs the abandonment and lets Handle proceed to the solve.
func (h Handler) checkInputsLiveness(ctx context.Context, intent reducercontract.Intent) error {
	if h.InputsLiveness == nil {
		return nil
	}
	fenceCtx, span := h.beginFenceSpan(ctx)
	outcome := fenceOutcomeProceed
	pending, err := h.InputsLiveness.PendingValueFlowInputs(fenceCtx)
	defer func() {
		if span == nil {
			return
		}
		span.SetAttributes(
			telemetry.AttrOutcome(outcome),
			attribute.Int("pending_input_count", len(pending)),
		)
		span.End()
	}()
	if err != nil {
		outcome = fenceOutcomeError
		return fmt.Errorf("check value-flow inputs liveness: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	now := h.now()
	anchor := crossscope.ReadinessCycleAnchor(intent)
	if anchor.IsZero() || now.Sub(anchor) < crossscope.ProducerReadinessMaxWait {
		outcome = crossscope.ReadinessWaitDeferred
		h.recordReadinessWait(ctx, outcome)
		slog.Info(
			"value-flow refresh deferred: cloud-sink chain inputs not drained",
			"scope_id", intent.ScopeID,
			"generation_id", intent.GenerationID,
			"failure_class", crossscope.ValueFlowInputsNotReadyFailureClass,
			"readiness_wait_outcome", outcome,
			"pending_input_count", len(pending),
			"pending_input_sample", pending,
			"elapsed_since_cycle_anchor", now.Sub(anchor),
			"max_wait", crossscope.ProducerReadinessMaxWait,
		)
		return crossscope.WrapValueFlowInputsUndrained(pending)
	}

	outcome = crossscope.ReadinessWaitAbandoned
	h.recordReadinessWait(ctx, outcome)
	slog.Warn(
		"value-flow refresh solving with undrained cloud-sink chain inputs: starvation bound reached",
		"scope_id", intent.ScopeID,
		"generation_id", intent.GenerationID,
		"failure_class", crossscope.ValueFlowInputsNotReadyFailureClass,
		"readiness_wait_outcome", outcome,
		"pending_input_count", len(pending),
		"pending_input_sample", pending,
		"elapsed_since_cycle_anchor", now.Sub(anchor),
		"max_wait", crossscope.ProducerReadinessMaxWait,
	)
	return nil
}

// Fence span outcomes that are not readiness-wait outcomes: the fence found
// nothing pending and the solve proceeds, or the fence read itself failed.
// The wait outcomes reuse crossscope.ReadinessWaitDeferred and
// ReadinessWaitAbandoned so the span, the counter, and the log agree.
const (
	fenceOutcomeProceed = "proceed"
	fenceOutcomeError   = "error"
)

// beginFenceSpan starts the fence-read span when a tracer is wired; a nil
// Tracer (as in every unit test) skips tracing entirely. checkInputsLiveness
// ends the span after the decision so it carries the outcome attribute
// (proceed, deferred, abandoned, or error) and the pending row count.
func (h Handler) beginFenceSpan(ctx context.Context) (context.Context, trace.Span) {
	if h.Tracer == nil {
		return ctx, nil
	}
	return h.Tracer.Start(ctx, telemetry.SpanReducerValueFlowInputsFence)
}

// recordReadinessWait emits one eshu_dp_reducer_readiness_waits_total point
// for the #6923 fence, reusing the #6785 counter with
// domain=code_value_flow_refresh so both readiness signals for this domain
// land on one metric. Nil Instruments (unit tests) is a no-op.
func (h Handler) recordReadinessWait(ctx context.Context, outcome string) {
	if h.Instruments == nil || h.Instruments.ReducerReadinessWaits == nil {
		return
	}
	h.Instruments.ReducerReadinessWaits.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrDomain(string(reducercontract.DomainCodeValueFlowRefresh)),
		telemetry.AttrOutcome(outcome),
	))
}

// Definition returns the additive domain definition for the value-flow
// refresh: it re-runs the global fixpoint (rewriting cloud-sink edges) after
// late producers land, reading the cross-scope chain the fixpoint loads.
func Definition() reducercontract.DomainDefinition {
	return reducercontract.DomainDefinition{
		Domain:  reducercontract.DomainCodeValueFlowRefresh,
		Summary: "re-run the global value-flow fixpoint after late producers land",
		Ownership: reducercontract.OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "code_value_flow_refresh",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
			},
		},
	}
}
