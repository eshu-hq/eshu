// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"
	"time"

	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// defaultConfigStateDriftRedriveDelay is the fallback gap between a
// "no config repo owns this backend" rejection and its ledger row's first
// redrive eligibility when TerraformConfigStateDriftHandler.RedriveDelay is
// unset.
const defaultConfigStateDriftRedriveDelay = 5 * time.Minute

// ConfigStateDriftRedriveScheduler is the narrow surface
// TerraformConfigStateDriftHandler needs to schedule a bounded redrive
// attempt after observing a "no config repo owns this backend" rejection
// (issue #5593 P1-A). postgres.ConfigStateDriftRedriveStore implements it.
type ConfigStateDriftRedriveScheduler interface {
	EnsureScheduled(ctx context.Context, scopeID, generationID string, firstAttemptAt time.Time) error
}

func (h TerraformConfigStateDriftHandler) redriveNow() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h TerraformConfigStateDriftHandler) redriveDelay() time.Duration {
	if h.RedriveDelay > 0 {
		return h.RedriveDelay
	}
	return defaultConfigStateDriftRedriveDelay
}

// scheduleRedrive schedules a bounded catch-up attempt for intent right
// after Handle observes tfstatebackend.ErrNoConfigRepoOwnsBackend (issue
// #5593 P1-A). Handle() ALWAYS returns Result{Status: ResultStatusSucceeded}
// for this rejection -- an operator-actionable, non-fatal outcome by design
// -- so the reducer queue's per-(scope,generation,domain) ON CONFLICT DO
// NOTHING fence would otherwise make it permanently terminal: nothing else
// would ever revisit it, even if the config-side repo that owns this
// backend syncs and activates its own terraform_backends fact moments
// later. Scheduling here, rather than unconditionally at enqueue time
// (rejected design; see postgres.ConfigStateDriftRuntimeTrigger's doc
// comment), means only the generations that actually hit this exact
// rejection pay for a redrive attempt -- not every runtime-triggered
// state_snapshot:* activation.
//
// Nil-safe: Redrive unwired is a no-op, matching every caller before this
// fix landed. A scheduling failure is logged, not propagated -- the "no
// owner" rejection itself is a valid, already-recorded Handle() outcome; a
// best-effort recovery aid failing to schedule must not turn that into a
// runtime error that would dead-letter the intent.
func (h TerraformConfigStateDriftHandler) scheduleRedrive(ctx context.Context, intent Intent) {
	if h.Redrive == nil {
		return
	}
	firstAttemptAt := h.redriveNow().Add(h.redriveDelay())
	if err := h.Redrive.EnsureScheduled(ctx, intent.ScopeID, intent.GenerationID, firstAttemptAt); err != nil {
		if h.Logger != nil {
			h.Logger.LogAttrs(
				ctx, slog.LevelWarn, "config state drift redrive scheduling failed",
				log.ScopeID(intent.ScopeID),
				log.GenerationID(intent.GenerationID),
				slog.String("error", err.Error()),
			)
		}
	}
}
