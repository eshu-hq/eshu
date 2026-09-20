// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// awsCloudRuntimeDriftStatePendingMissingKey is the ledger missing set for a
// state-pending defer. It is a constant, matching the checker's deliberate
// coarseness: HasPendingStateSnapshotEvidence reports whether ANY
// state_snapshot scope is mid-ingestion, not which backend a given ARN needs,
// so the wait cannot name a narrower key.
const awsCloudRuntimeDriftStatePendingMissingKey = "state_snapshot_pending"

// checkAWSCloudRuntimeDriftReadinessBeforeLoadWithLedger captures the pre-load
// pending signal for the ledger-anchored gate. With a nil ledger it delegates
// to checkAWSCloudRuntimeDriftReadinessBeforeLoad on the real intent,
// preserving the unwired row-anchored bound by construction — including the
// terminal fallback, which commits without probing the backend at all. With a
// wired ledger the elapsed-time bound is NOT enforced here: it is enforced
// post-load against the (scope, domain) readiness-wait ledger, whose
// first-defer anchor survives supersession (#6814), so the row-anchored
// early-out is bypassed by evaluating an unanchored copy of the intent.
func (h AWSCloudRuntimeDriftHandler) checkAWSCloudRuntimeDriftReadinessBeforeLoadWithLedger(
	ctx context.Context,
	intent reducercontract.Intent,
	now time.Time,
) (awsCloudRuntimeDriftReadinessSignal, error) {
	if h.ReadinessWaits == nil {
		return h.checkAWSCloudRuntimeDriftReadinessBeforeLoad(ctx, intent, now)
	}
	unanchored := intent
	unanchored.CycleStartedAt = time.Time{}
	unanchored.EnqueuedAt = time.Time{}
	return h.checkAWSCloudRuntimeDriftReadinessBeforeLoad(ctx, unanchored, now)
}

// applyStatePendingWaitPostLoad combines the pre-load signal with the
// post-load admitted candidates and the ledger, then does exactly one of:
// proceed (clearing a stood wait), defer (recording the wait and returning
// the non-counting state-pending error), or settle past the ledger-anchored
// bound (recording the settle and proceeding to the writer). It returns the
// ledger decision alongside the error so Handle logs the ledger-anchored
// elapsed, never the superseded row's fresh cycle start. A nil ledger keeps
// the pre-ledger per-row behavior.
func (h AWSCloudRuntimeDriftHandler) applyStatePendingWaitPostLoad(
	ctx context.Context,
	signal awsCloudRuntimeDriftReadinessSignal,
	admitted []model.Candidate,
	intent reducercontract.Intent,
	now time.Time,
) (crossscope.WaitDecision, error) {
	if signal.gateDisabled {
		return crossscope.WaitDecision{}, nil
	}
	var missing []string
	if shouldDeferForLoadedEvidence(signal, admitted) {
		missing = []string{awsCloudRuntimeDriftStatePendingMissingKey}
	}
	existing, found, err := crossscope.ReadWait(ctx, h.ReadinessWaits, intent.ScopeID, intent.Domain, awsCloudRuntimeDriftCycleAnchor(intent))
	if err != nil {
		// A store that cannot answer is a real failure, classified like a
		// readiness-probe error: retryable and counting, so it surfaces
		// instead of deferring forever on a wait that was never recorded.
		return crossscope.WaitDecision{}, factload.ClassifyFactLoadError(err)
	}
	decision := crossscope.DecideWait(crossscope.WaitInput{
		Existing: existing, Found: found,
		ScopeID: intent.ScopeID, Domain: intent.Domain,
		GenerationID: intent.GenerationID, CycleStartedAt: intent.CycleStartedAt,
		Missing: missing, Now: now, MaxWait: awsCloudRuntimeDriftStatePendingMaxWait,
	})
	// Every ledger mutation below is classified like the read path:
	// ClassifyFactLoadError is nil-safe, so success still returns nil, while
	// a transient upsert/clear failure retries counting instead of dying
	// silently or terminally failing the row on an unrecorded wait.
	if len(missing) == 0 {
		return decision, factload.ClassifyFactLoadError(crossscope.ApplyWaitDecision(ctx, h.ReadinessWaits, decision, intent.ScopeID, intent.Domain))
	}
	if err := crossscope.ApplyWaitDecision(ctx, h.ReadinessWaits, decision, intent.ScopeID, intent.Domain); err != nil {
		return decision, factload.ClassifyFactLoadError(err)
	}
	return decision, nil
}
