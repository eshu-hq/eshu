// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// missingTargetLogSample bounds the missing-target ARNs one log line carries.
const missingTargetLogSample = 10

// now is the handler clock.
func (h IAMCanPerformMaterializationHandler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// readCrossScopeWait reads the (scope, domain) readiness-wait row. It returns
// no row when cross-scope resolution is not wired, so the ledger is only read
// when a wait is possible.
func (h IAMCanPerformMaterializationHandler) readCrossScopeWait(
	ctx context.Context,
	intent reducercontract.Intent,
) (crossscope.ReadinessWait, bool, error) {
	if h.CrossScopeTargets == nil {
		return crossscope.ReadinessWait{}, false, nil
	}
	return crossscope.ReadWait(ctx, h.ReadinessWaits, intent.ScopeID,
		reducercontract.DomainIAMCanPerformMaterialization, crossscope.ReadinessCycleAnchor(intent))
}

// waitInput builds the DecideWait input for this evaluation.
func (h IAMCanPerformMaterializationHandler) waitInput(
	intent reducercontract.Intent,
	existing crossscope.ReadinessWait,
	found bool,
	missing []string,
) crossscope.WaitInput {
	return crossscope.WaitInput{
		Existing:       existing,
		Found:          found,
		ScopeID:        intent.ScopeID,
		Domain:         reducercontract.DomainIAMCanPerformMaterialization,
		GenerationID:   intent.GenerationID,
		CycleStartedAt: intent.CycleStartedAt,
		Missing:        missing,
		Now:            h.now(),
		MaxWait:        h.ReadinessMaxWait,
	}
}

// pollCrossScopeWait is the cheap poll path. This generation's ready edges
// already committed at the ledger's missing set, so it asks the loader only
// about the missing ARNs, with no fact load and no extraction. handled is
// false when a key resolved (or the answer cannot be mapped back to the
// ledger), and the caller then runs the full evaluation, which re-commits.
func (h IAMCanPerformMaterializationHandler) pollCrossScopeWait(
	ctx context.Context,
	intent reducercontract.Intent,
	existing crossscope.ReadinessWait,
) (bool, error) {
	request, ok := pollRequest(intent.ScopeID, existing.MissingKeys)
	if !ok {
		return false, nil
	}
	decided, err := h.loadAndDecideCrossScopeTargets(ctx, request)
	if err != nil {
		return true, err
	}
	if !crossscope.SameMissingSet(existing, decided.missing) {
		return false, nil
	}
	decision := crossscope.DecideWait(h.waitInput(intent, existing, true, decided.missing))
	if decision.Commit {
		// Unreachable for a poll-eligible row with an unchanged set; fall back
		// rather than skip a commit the decision asked for.
		return false, nil
	}
	if err := crossscope.ApplyWaitDecision(ctx, h.ReadinessWaits, decision, intent.ScopeID, reducercontract.DomainIAMCanPerformMaterialization); err != nil {
		return true, err
	}
	h.reportWait(ctx, intent, decision, decided.missing, false)
	if decision.Defer {
		return true, iamCanPerformTargetNotReadyError{scopeID: intent.ScopeID, generationID: intent.GenerationID, notReady: len(decided.missing)}
	}
	return true, nil
}

// pollRequest rebuilds the bounded loader request from the ledger's missing
// ARNs. The account comes from the aws:<account>:<region>:iam scope id, which
// is the account every requested ARN was restricted to.
func pollRequest(scopeID string, arns []string) (CrossScopeTargetRequest, bool) {
	parts := strings.Split(scopeID, ":")
	if len(parts) != 4 || parts[0] != "aws" || parts[1] == "" {
		return CrossScopeTargetRequest{}, false
	}
	request := CrossScopeTargetRequest{AccountID: parts[1], ExcludeScopeID: scopeID}
	for _, arn := range arns {
		target, _, ok := crossScopeTargetForARN(arn, parts[1])
		if !ok {
			return CrossScopeTargetRequest{}, false
		}
		request.Targets = append(request.Targets, target)
	}
	return request, len(request.Targets) > 0
}

// settledOutcomes rewrites not-ready target outcomes as abandoned when a
// commit writes them as unresolved because their wait settled.
func settledOutcomes(outcomes map[string]int, decision crossscope.WaitDecision) map[string]int {
	if decision.Defer || decision.Outcome == "" {
		return outcomes
	}
	outcomes[crossScopeTargetAbandoned] += outcomes[crossScopeTargetNotReady] + outcomes[crossScopeTargetScopeUnregistered]
	outcomes[crossScopeTargetNotReady] = 0
	outcomes[crossScopeTargetScopeUnregistered] = 0
	return outcomes
}

// reportWait emits the readiness-wait counter and the matching log line for
// one evaluation that has a missing set.
func (h IAMCanPerformMaterializationHandler) reportWait(
	ctx context.Context,
	intent reducercontract.Intent,
	decision crossscope.WaitDecision,
	missing []string,
	committed bool,
) {
	if decision.Outcome == "" {
		return
	}
	h.recordReadinessWait(ctx, decision.Outcome)
	maxWait := h.ReadinessMaxWait
	if maxWait <= 0 {
		maxWait = crossscope.ProducerReadinessMaxWait
	}
	attrs := []any{
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.FailureClass(IAMCanPerformTargetNotReadyFailureClass),
		slog.String("readiness_wait_outcome", decision.Outcome),
		slog.Int("missing_target_count", len(missing)),
		slog.Bool("committed", committed),
		slog.Duration("elapsed_since_first_defer", decision.Elapsed),
		slog.Duration("max_wait", maxWait),
	}
	switch decision.Outcome {
	case crossscope.ReadinessWaitDeferred:
		slog.InfoContext(ctx, "iam can_perform waiting on cross-scope targets", attrs...)
	case crossscope.ReadinessWaitAbandoned:
		slog.WarnContext(ctx, "iam can_perform cross-scope readiness bound expired; missing targets stay unresolved",
			append(attrs, slog.Any("missing_target_sample", crossscope.MissingSample(missing, missingTargetLogSample)))...)
	default:
		slog.InfoContext(ctx, "iam can_perform committed with a settled missing target set", attrs...)
	}
}

// recordCrossScopeOutcomes emits one data point per outcome, including zeros,
// so each series exists from the first committing evaluation. Only
// evaluations that commit call it, so not_ready does not scale with polls.
func (h IAMCanPerformMaterializationHandler) recordCrossScopeOutcomes(ctx context.Context, outcomes map[string]int) {
	if h.Instruments == nil || h.Instruments.IAMCanPerformCrossScopeTargets == nil {
		return
	}
	for _, outcome := range []string{
		crossScopeTargetResolved, crossScopeTargetUnresolved, crossScopeTargetNotReady,
		crossScopeTargetScopeUnregistered, crossScopeTargetAbandoned, crossScopeTargetGlobLocalOnly,
	} {
		h.Instruments.IAMCanPerformCrossScopeTargets.Add(ctx, int64(outcomes[outcome]), metric.WithAttributes(
			telemetry.AttrOutcome(outcome),
		))
	}
}

// recordReadinessWait emits one readiness-wait data point for this domain.
func (h IAMCanPerformMaterializationHandler) recordReadinessWait(ctx context.Context, outcome string) {
	if h.Instruments == nil || h.Instruments.ReducerReadinessWaits == nil {
		return
	}
	h.Instruments.ReducerReadinessWaits.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrDomain(string(reducercontract.DomainIAMCanPerformMaterialization)),
		telemetry.AttrOutcome(outcome),
	))
}

// settledResult is the success result of a poll that settled a wait whose
// edges already committed earlier in this generation.
func settledResult(intent reducercontract.Intent) reducercontract.Result {
	return reducercontract.Result{
		IntentID:        intent.IntentID,
		Domain:          reducercontract.DomainIAMCanPerformMaterialization,
		Status:          reducercontract.ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf("cross-scope readiness wait settled for scope %s; CAN_PERFORM edges were committed earlier in generation %s", intent.ScopeID, intent.GenerationID),
	}
}
