// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// QueuedTriggerProbe reports whether an accepted webhook trigger is
// queued for the handoff (#8123). The sweep consults it at repository
// boundaries so a push that arrives mid-sweep is claimed at the next
// collector cycle instead of after the sweep tail and its batch drain.
// The probe is read-only: claiming stays in
// WebhookTriggerRepositorySelector under the existing claim lease and
// fencing token, so the sweep never races the claim.
type QueuedTriggerProbe interface {
	HasQueuedAcceptedTriggers(ctx context.Context) (bool, error)
}

const (
	// defaultSweepYieldMaxPerSweep bounds how many times one logical
	// sweep may yield to queued triggers before it must run to
	// completion (#8123). With the default check interval a yielded
	// sweep still advances at least 8 chunks before the completing
	// walk, so a trigger storm delays coverage by extra cycle
	// boundaries, never indefinitely.
	defaultSweepYieldMaxPerSweep = 8
	// defaultSweepYieldCheckEvery is how many repositories the sweep
	// visits between trigger probes. On the measured 784-repository
	// fleet (sweep median ~220s) one repository costs ~0.28s, so a
	// queued trigger is noticed within ~4.5s plus the current
	// repository's sync, at under 50 indexed probe reads per sweep.
	defaultSweepYieldCheckEvery = 16
)

// sweepYieldPolicy carries one cycle's sweep-yield inputs into the git
// sync. A nil Probe disables yielding (the pre-#8123 full walk).
type sweepYieldPolicy struct {
	Probe QueuedTriggerProbe
	// CheckEvery is the visited-repository count between probes;
	// non-positive takes defaultSweepYieldCheckEvery.
	CheckEvery int
}

func (p sweepYieldPolicy) checkEvery() int {
	if p.CheckEvery > 0 {
		return p.CheckEvery
	}
	return defaultSweepYieldCheckEvery
}

// shouldYield reports whether the walk stops before its next repository
// because an accepted trigger is queued. It probes only every
// checkEvery visited repositories, never before the first one, so every
// cycle makes progress. A probe failure degrades to the legacy full
// walk with a WARN: the yield is a latency optimization and must never
// fail selection.
func (p sweepYieldPolicy) shouldYield(ctx context.Context, logger *slog.Logger, visited int) bool {
	if p.Probe == nil || visited == 0 || visited%p.checkEvery() != 0 {
		return false
	}
	queued, err := p.Probe.HasQueuedAcceptedTriggers(ctx)
	if err != nil {
		if logger != nil {
			logger.WarnContext(ctx, "git sweep webhook trigger probe failed; completing sweep",
				slog.Any("error", err))
		}
		return false
	}
	return queued
}

// finishSweepWalk ends one sweep walk: a completed walk finishes the
// rotation tracker as before #8123; a yielded walk records the yield
// signal and the resume point an operator needs to see where the
// sweep stopped and where it will continue.
func finishSweepWalk(
	ctx context.Context,
	tracker *reconcileWalkTracker,
	baseline gitDeltaBaseline,
	logger *slog.Logger,
	yielded bool,
	visited, total, selectedCount int,
) (nextStart, remaining int) {
	if !yielded {
		return tracker.finish(ctx, baseline), 0
	}
	nextStart, remaining = tracker.finishYielded(ctx, baseline)
	baseline.recordSweepYield(ctx)
	if logger != nil {
		logger.InfoContext(ctx, "git repository sweep yielded to queued webhook trigger",
			slog.Int("visited_repositories", visited),
			slog.Int("repository_count", total),
			slog.Int("selected_repositories", selectedCount),
			slog.Int("resume_offset", nextStart),
			slog.Int("remaining_repositories", remaining))
	}
	return nextStart, remaining
}

// recordSweepYield counts one sweep yield (#8123). It is a no-op
// without instruments: metric collection must never fail selection.
func (b gitDeltaBaseline) recordSweepYield(ctx context.Context) {
	if b.Instruments == nil || b.Instruments.CollectorSweepYields == nil {
		return
	}
	b.Instruments.CollectorSweepYields.Add(ctx, 1)
}

// sweepYieldPolicy builds this cycle's yield inputs: the probe is
// consulted only while the logical sweep still has yield budget
// (#8123). Withholding the probe is what makes the completing walk
// unstoppable, so a steady trigger stream cannot starve the sweep.
func (s *NativeRepositorySelector) sweepYieldPolicy() sweepYieldPolicy {
	policy := sweepYieldPolicy{CheckEvery: s.SweepYieldCheckEvery}
	if s.sweepYieldsUsed < s.sweepYieldMaxPerSweep() {
		policy.Probe = s.SweepYieldProbe
	}
	return policy
}

// sweepYieldMaxPerSweep returns the configured yield bound, or the
// default when unset. A value below 1 keeps the default so a
// misconfigured selector cannot disable sweep completion.
func (s NativeRepositorySelector) sweepYieldMaxPerSweep() int {
	if s.SweepYieldMaxPerSweep > 0 {
		return s.SweepYieldMaxPerSweep
	}
	return defaultSweepYieldMaxPerSweep
}

// recordClaimWait records, for each accepted trigger in the claim
// batch, how long it sat queued between receipt and this claim
// (#8123): received_at is insert-only in the store upsert, so the wait
// spans any earlier lease stalls honestly. Non-accepted rows owe no
// sync and stay out of the histogram the acceptance criterion reads.
// A clock-skewed negative wait clamps to zero. It is a no-op without
// instruments: metric collection must never fail selection.
func (s WebhookTriggerRepositorySelector) recordClaimWait(ctx context.Context, triggers []webhook.StoredTrigger, claimedAt time.Time) {
	if s.Instruments == nil || s.Instruments.WebhookTriggerClaimWait == nil {
		return
	}
	for _, trigger := range triggers {
		if trigger.Decision != webhook.DecisionAccepted || trigger.ReceivedAt.IsZero() {
			continue
		}
		wait := claimedAt.Sub(trigger.ReceivedAt).Seconds()
		if wait < 0 {
			wait = 0
		}
		s.Instruments.WebhookTriggerClaimWait.Record(ctx, wait)
	}
}

// noteSweepYieldResult folds one git cycle's outcome into the logical
// sweep's yield state (#8123): a yielded walk spends one yield and
// owes its unvisited tail to the next cycle; a completed walk closes
// the sweep and re-arms the budget. The caller invokes it only after
// a successful sync, so a failed cycle retries the same segment.
func (s *NativeRepositorySelector) noteSweepYieldResult(synced GitSyncSelection) {
	if synced.YieldedToWebhook {
		s.sweepYieldsUsed++
		s.sweepWalkRemaining = synced.SweepRemainingRepositories
		return
	}
	s.sweepYieldsUsed = 0
	s.sweepWalkRemaining = 0
}
