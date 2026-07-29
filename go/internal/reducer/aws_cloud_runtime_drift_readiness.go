// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// awsCloudRuntimeDriftStatePendingMaxAttempts bounds the readiness defer
// (#5848, #5837's root cause). Below the bound, an orphaned_cloud_resource
// candidate is held back while a Terraform state_snapshot scope is still
// mid-ingestion, because it might supply the state that would reclassify the
// finding. At or past the bound, Handle commits its best-available
// classification instead of deferring forever — the terminal fallback a
// genuine orphan (no Terraform anywhere, ever) needs, since a permanently
// absent state scope would otherwise starve the intent. Mirrors
// pendingImpactSecurityAlertReconciliationMaxAttempts's shape.
const awsCloudRuntimeDriftStatePendingMaxAttempts = 3

// AWSCloudRuntimeDriftReadinessChecker reports whether a Terraform
// state_snapshot scope is still mid-ingestion anywhere, so the handler can
// tell "not ready" apart from "verdict" for an orphaned_cloud_resource
// classification.
//
// This is coarse by design: it does not attempt to prove that a SPECIFIC
// pending scope would resolve a SPECIFIC ARN, because which backend/locator
// owns a given ARN cannot be known until state activates and the ARN join
// resolves (the same chicken-and-egg the config-owner resolution already
// lives with, see aws_cloud_runtime_drift_evidence.go's
// AWSCloudRuntimeDriftConfigResolver). The checker only tells Handle that AT
// LEAST ONE state_snapshot scope has not yet finished, which can only ever
// cause an UNNECESSARY defer (bounded by awsCloudRuntimeDriftStatePendingMaxAttempts,
// never an incorrect verdict.
//
// This is deliberately its OWN mechanism rather than an entry in
// crossScopeDependencyCatalog (cross_scope_dependencies.go): that catalog's
// CrossScopeDependency.ProducerDomains names REDUCER DOMAINS, and the producer
// here is raw Terraform-state collector evidence in ANY state_snapshot:* scope
// -- not a reducer domain's canonical output. Generalizing the catalog to
// express a scope-shaped producer is the #5709 follow-up the issue asked to be
// scoped with that surface's owner; this checker does not attempt it.
type AWSCloudRuntimeDriftReadinessChecker interface {
	// HasPendingStateSnapshotEvidence reports whether any state_snapshot:*
	// ingestion scope has a generation still in status 'pending' -- registered
	// and mid-ingestion, neither active nor failed.
	HasPendingStateSnapshotEvidence(ctx context.Context) (bool, error)
}

// AWSCloudRuntimeDriftStatePendingFailureClass classifies a Handle call
// deferred because a Terraform state_snapshot scope has not finished
// ingesting. The reducer queue treats it as a non-counting retry class
// (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go) so the defer
// never erodes the retry budget while the intent waits for state to activate.
const AWSCloudRuntimeDriftStatePendingFailureClass = "aws_cloud_runtime_drift_state_pending"

// awsCloudRuntimeDriftStatePendingError marks a readiness-gate defer as
// retryable so the durable queue re-runs the intent once the pending
// state_snapshot scope activates (or the bound is reached and Handle commits
// its best-available classification instead).
type awsCloudRuntimeDriftStatePendingError struct {
	scopeID      string
	generationID string
}

func newAWSCloudRuntimeDriftStatePendingError(scopeID, generationID string) error {
	return awsCloudRuntimeDriftStatePendingError{scopeID: scopeID, generationID: generationID}
}

func (e awsCloudRuntimeDriftStatePendingError) Error() string {
	return fmt.Sprintf(
		"aws cloud runtime drift deferred for scope %s generation %s: a terraform state_snapshot scope is still mid-ingestion",
		e.scopeID, e.generationID,
	)
}

func (awsCloudRuntimeDriftStatePendingError) Retryable() bool { return true }

func (awsCloudRuntimeDriftStatePendingError) FailureClass() string {
	return AWSCloudRuntimeDriftStatePendingFailureClass
}

// shouldDeferForStatePending reports whether Handle must defer instead of
// writing, per awsCloudRuntimeDriftStatePendingMaxAttempts. It returns false
// (never defer) when the checker is nil (gate disabled, matches pre-#5848
// behavior), when the attempt bound is reached (terminal fallback), or when no
// admitted candidate is actually an orphaned_cloud_resource (a pending state
// scope cannot improve any other finding kind: unmanaged/ambiguous/unknown/
// image_version_drift all already require state to be present).
func (h AWSCloudRuntimeDriftHandler) shouldDeferForStatePending(
	ctx context.Context,
	intent Intent,
	admitted []model.Candidate,
) (bool, error) {
	if h.ReadinessChecker == nil || intent.AttemptCount >= awsCloudRuntimeDriftStatePendingMaxAttempts {
		return false, nil
	}
	if !hasOrphanedAWSCloudRuntimeDriftCandidate(admitted) {
		return false, nil
	}
	pending, err := h.ReadinessChecker.HasPendingStateSnapshotEvidence(ctx)
	if err != nil {
		return false, err
	}
	return pending, nil
}

// hasOrphanedAWSCloudRuntimeDriftCandidate reports whether any admitted
// candidate classified as orphaned_cloud_resource -- the only finding kind a
// not-yet-activated Terraform state scope could still improve.
func hasOrphanedAWSCloudRuntimeDriftCandidate(admitted []model.Candidate) bool {
	for _, candidate := range admitted {
		if cloudruntime.FindingKind(awsCloudRuntimeFindingKind(candidate)) == cloudruntime.FindingKindOrphanedCloudResource {
			return true
		}
	}
	return false
}

// evaluatedAWSCloudRuntimeDriftARNs returns every ARN this pass's evidence
// load covered, deduplicated and independent of whether Classify produced an
// admitted candidate for it. The generation-authoritative retire uses this to
// bound its blast radius (#5848).
func evaluatedAWSCloudRuntimeDriftARNs(rows []cloudruntime.AddressedRow) []string {
	seen := make(map[string]struct{}, len(rows))
	arns := make([]string, 0, len(rows))
	for _, row := range rows {
		arn := strings.TrimSpace(row.ARN)
		if arn == "" {
			continue
		}
		if _, ok := seen[arn]; ok {
			continue
		}
		seen[arn] = struct{}{}
		arns = append(arns, arn)
	}
	return arns
}

// isAWSCloudRuntimeDriftWriteSuperseded reports whether err is (or wraps) the
// insert-admission rejection, so Handle can log it distinctly from a handler
// error.
func isAWSCloudRuntimeDriftWriteSuperseded(err error) bool {
	var superseded awsCloudRuntimeDriftWriteSupersededError
	return errors.As(err, &superseded)
}

// logStatePendingDefer records a readiness-gate defer as its own structured
// log line, distinct from an admitted-findings log or a handler error, so an
// operator can tell the three apart (#5848 acceptance criterion).
func (h AWSCloudRuntimeDriftHandler) logStatePendingDefer(
	ctx context.Context,
	intent Intent,
	admitted []model.Candidate,
) {
	if h.Logger == nil {
		return
	}
	h.Logger.LogAttrs(
		ctx, slog.LevelInfo, "aws cloud runtime drift deferred for pending terraform state",
		log.Domain(string(intent.Domain)),
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		slog.Int("attempt_count", intent.AttemptCount),
		slog.Int("admitted_candidates", len(admitted)),
	)
}

// logWriteSuperseded records an insert-admission rejection as its own
// structured log line, distinct from a readiness defer or a handler error.
func (h AWSCloudRuntimeDriftHandler) logWriteSuperseded(ctx context.Context, intent Intent) {
	if h.Logger == nil {
		return
	}
	h.Logger.LogAttrs(
		ctx, slog.LevelInfo, "aws cloud runtime drift write superseded by a fresher pass",
		log.Domain(string(intent.Domain)),
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		slog.Int("attempt_count", intent.AttemptCount),
	)
}
