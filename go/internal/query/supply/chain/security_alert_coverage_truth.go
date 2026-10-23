// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// securityAlertCollectionStateStage names the stage event for the
// collection-state read behind an empty security-alert answer.
const securityAlertCollectionStateStage = "security_alert_collection_state"

// securityAlertCollectionStateTimeout bounds the whole collection-state probe
// behind one empty answer. The measured reads take about 1 ms for one target
// and about 36 ms for 50 targets with 2,000 work items each
// (docs/internal/evidence/8803-security-alert-empty-answer-truth.md); past
// this deadline the answer fails closed to collector_state_unavailable rather
// than holding the request.
const securityAlertCollectionStateTimeout = time.Second

// securityAlertEmptyAnswerVerdict classifies an empty security-alert answer
// from the collector's configuration and per-target state (#8803). It runs only
// for an empty answer and only when the collection-state store is wired; the
// boolean is false when no verdict was produced, so the caller keeps the
// row-derived coverage unchanged.
//
// The whole probe runs under securityAlertCollectionStateTimeout. A store
// error or an expired deadline does not fail the request: it fails closed to a
// collector_state_unavailable reason, fallback truth, unavailable freshness,
// and a readiness_unavailable envelope, so the empty answer can never read as
// complete or fresh and a valid read never answers 5xx. The stage completion event carries coverage_state,
// coverage_reason, and collector_state (all bounded tokens), and the empty
// answer counter records one observation per classified answer.
//
// repositoryScopeIDs are the provider scope ids the repository selector
// resolved. A scoped caller reaches here only for a repository inside its
// grants (the out-of-grant check answers 404 first), so membership is computed
// only for a repository the caller may read; the verdict carries no target
// scope id, repository name, or allowlist.
func (h *Handler) securityAlertEmptyAnswerVerdict(
	r *http.Request,
	operation string,
	repositoryID string,
	repositoryScopeIDs []string,
) (securityAlertCoverageVerdict, bool) {
	if h == nil || h.SecurityAlertCollectionState == nil {
		return securityAlertCoverageVerdict{}, false
	}
	ctx := r.Context()
	timer := startSupplyChainQueryStage(ctx, h.Logger, operation, repositoryID, securityAlertCollectionStateStage)
	probeCtx, cancel := context.WithTimeout(ctx, securityAlertCollectionStateTimeout)
	verdict, err := h.readSecurityAlertCollectionVerdict(probeCtx, repositoryID != "", repositoryScopeIDs)
	cancel()
	if err != nil {
		verdict = securityAlertUnavailableVerdict()
	}
	reason := securityAlertCoverageReasonLabel(verdict)
	timer.Done(
		ctx,
		slog.Bool("error", err != nil),
		slog.Bool("deadline_exceeded", errors.Is(err, context.DeadlineExceeded)),
		slog.String("coverage_state", verdict.coverageState),
		slog.String("coverage_reason", reason),
		slog.String("collector_state", verdict.collectorState),
	)
	if h.Instruments != nil && h.Instruments.QuerySecurityAlertEmptyAnswers != nil {
		h.Instruments.QuerySecurityAlertEmptyAnswers.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrCoverageState(verdict.coverageState),
			telemetry.AttrReason(reason),
		))
	}
	return verdict, true
}

// securityAlertCoverageReasonLabel is the single bounded reason used for the
// log attribute and the metric label: the first reason, or "none" for a
// healthy empty answer.
func securityAlertCoverageReasonLabel(verdict securityAlertCoverageVerdict) string {
	if len(verdict.reasons) == 0 {
		return "none"
	}
	return verdict.reasons[0]
}

// readSecurityAlertCollectionVerdict performs at most two bounded reads: the
// enabled instances' configuration, then the latest work item and committed
// generation existence for the covering targets.
func (h *Handler) readSecurityAlertCollectionVerdict(
	ctx context.Context,
	anchored bool,
	repositoryScopeIDs []string,
) (securityAlertCoverageVerdict, error) {
	configs, err := h.SecurityAlertCollectionState.SecurityAlertCollectorConfigurations(ctx)
	if err != nil {
		return securityAlertCoverageVerdict{}, err
	}
	targets := decodeSecurityAlertTargets(configs)
	if len(targets) == 0 {
		return securityAlertNotConfiguredVerdict(
			securityAlertReasonCollectorNotConfigured, securityAlertCollectorStateNotConfigured,
		), nil
	}
	scopeIDs := securityAlertTargetScopeIDs(targets, securityAlertRequestedSlugs(repositoryScopeIDs), anchored)
	if len(scopeIDs) == 0 {
		return securityAlertNotConfiguredVerdict(
			securityAlertReasonRepositoryOutsideTargets, securityAlertCollectorStateOutsideTargets,
		), nil
	}
	states, err := h.SecurityAlertCollectionState.SecurityAlertTargetStates(ctx, scopeIDs)
	if err != nil {
		return securityAlertCoverageVerdict{}, err
	}
	return classifySecurityAlertCollection(scopeIDs, states, anchored), nil
}

// applySecurityAlertCoverageVerdict folds a verdict into an empty answer. A
// healthy verdict keeps the row-derived coverage and the exact, fresh truth and
// adds the ready_zero_results readiness envelope. Any other verdict replaces
// the coverage state, adds the reasons, lowers the truth level, sets the
// freshness state, and attaches the missing_collector_completion cause when the
// verdict proves it.
func applySecurityAlertCoverageVerdict(
	body map[string]any,
	coverage securityAlertReconciliationCoverageSummary,
	truth *querycontract.TruthEnvelope,
	verdict securityAlertCoverageVerdict,
) {
	if !verdict.healthy() {
		coverage.State = verdict.coverageState
		coverage.Reasons = append([]string(nil), verdict.reasons...)
		if truth != nil {
			truth.Level = querycontract.MinTruthLevel(truth.Level, verdict.level)
			truth.Freshness.State = verdict.freshness
			if verdict.cause {
				querycontract.WithFreshnessCause(truth, querycontract.FreshnessCauseMissingCollectorCompletion)
			}
		}
	}
	body["coverage"] = coverage
	switch verdict.readiness {
	case querycontract.CollectorListReadinessStateNotConfigured:
		body["collector_readiness"] = querycontract.BuildCollectorListReadiness(scope.CollectorSecurityAlert, 0, false, false)
	case querycontract.CollectorListReadinessStateReadyZeroResults:
		body["collector_readiness"] = querycontract.BuildCollectorListReadiness(scope.CollectorSecurityAlert, 0, false, true)
	case querycontract.CollectorListReadinessStateReadinessUnavailable:
		body["collector_readiness"] = querycontract.BuildCollectorListReadinessUnavailable(scope.CollectorSecurityAlert, 0, false)
	case querycontract.CollectorListReadinessStateReadyWithResults:
		// Unreachable: a verdict exists only for an empty answer. A
		// target_incomplete verdict leaves readiness empty and attaches no
		// envelope, because its coverage already says why the page is not a
		// configured-but-empty result.
	}
}
