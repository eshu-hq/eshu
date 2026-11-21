// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Per-source reducer backpressure env contract (#10882 decision 3). Every
// threshold is operator-set: unset disables the leg, because the design
// record's numbers stay Proposed until S0/S1 measurement confirms them and
// no enforcement default ships unmeasured.
const (
	sourceBackpressureEngageAgeEnv     = "ESHU_SOURCE_BACKPRESSURE_ENGAGE_AGE"
	sourceBackpressureReleaseAgeEnv    = "ESHU_SOURCE_BACKPRESSURE_RELEASE_AGE"
	sourceBackpressureDepthFloorEnv    = "ESHU_SOURCE_BACKPRESSURE_DEPTH_FLOOR"
	sourceBackpressureGrantDelayEnv    = "ESHU_SOURCE_BACKPRESSURE_GRANT_DELAY"
	sourceBackpressureMaxEngagementEnv = "ESHU_SOURCE_BACKPRESSURE_MAX_ENGAGEMENT"
	sourceBackpressureSignalCacheEnv   = "ESHU_SOURCE_BACKPRESSURE_SIGNAL_CACHE_TTL"
)

// reducerAdmissionSourceChecker is the per-source slowed-state consult the
// admission writer's source leg defers through. The Postgres workflow
// control store implements it; scripted fakes serve the writer proofs.
type reducerAdmissionSourceChecker interface {
	CheckSourceBackpressureForSource(context.Context, string, workflow.SourceBackpressureConfig, time.Time) (workflow.SourceBackpressureDecision, error)
}

// loadSourceBackpressureConfig parses the per-source backpressure env
// contract. All thresholds unset is the disabled zero value; a partial set
// fails startup through the config validation, because a half-configured
// throttle is a misconfiguration, not a conservative default.
func loadSourceBackpressureConfig(getenv func(string) string) (workflow.SourceBackpressureConfig, error) {
	var config workflow.SourceBackpressureConfig
	var err error
	if config.EngageAge, err = parseSourceBackpressureDuration(getenv, sourceBackpressureEngageAgeEnv); err != nil {
		return workflow.SourceBackpressureConfig{}, err
	}
	if config.ReleaseAge, err = parseSourceBackpressureDuration(getenv, sourceBackpressureReleaseAgeEnv); err != nil {
		return workflow.SourceBackpressureConfig{}, err
	}
	if config.GrantDelay, err = parseSourceBackpressureDuration(getenv, sourceBackpressureGrantDelayEnv); err != nil {
		return workflow.SourceBackpressureConfig{}, err
	}
	if config.MaxEngagement, err = parseSourceBackpressureDuration(getenv, sourceBackpressureMaxEngagementEnv); err != nil {
		return workflow.SourceBackpressureConfig{}, err
	}
	if config.SignalCacheTTL, err = parseSourceBackpressureDuration(getenv, sourceBackpressureSignalCacheEnv); err != nil {
		return workflow.SourceBackpressureConfig{}, err
	}
	rawFloor := strings.TrimSpace(getenv(sourceBackpressureDepthFloorEnv))
	if rawFloor != "" {
		floor, parseErr := strconv.ParseInt(rawFloor, 10, 64)
		if parseErr != nil {
			return workflow.SourceBackpressureConfig{}, fmt.Errorf("parse %s: %w", sourceBackpressureDepthFloorEnv, parseErr)
		}
		config.DepthFloor = floor
	}
	if err := config.Validate(); err != nil {
		return workflow.SourceBackpressureConfig{}, err
	}
	return config, nil
}

// parseSourceBackpressureDuration parses one optional duration env var; an
// unset value is zero and the whole-config validation decides whether the
// set is complete.
func parseSourceBackpressureDuration(getenv func(string) string, name string) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return parsed, nil
}

// sourceLegEnabled reports whether the per-source admission leg is wired
// and configured.
func (w reducerAdmissionWriter) sourceLegEnabled() bool {
	return w.sourceChecker != nil && w.sourceConfig.Enabled()
}

// admissionSourceGroup is one source's slice of an intent batch. Batches
// are per-generation in production (single source), but the writer
// partitions defensively so a mixed batch can never whole-batch throttle
// a healthy source behind a slowed one (#10882 decision 3).
type admissionSourceGroup struct {
	source  string
	intents []runtime.ReducerIntent
}

// partitionIntentsBySource groups intents by source system in first-seen
// order, preserving intent order inside each group.
func partitionIntentsBySource(intents []runtime.ReducerIntent) []admissionSourceGroup {
	var groups []admissionSourceGroup
	index := make(map[string]int)
	for _, intent := range intents {
		source := strings.TrimSpace(intent.SourceSystem)
		slot, ok := index[source]
		if !ok {
			slot = len(groups)
			index[source] = slot
			groups = append(groups, admissionSourceGroup{source: source})
		}
		groups[slot].intents = append(groups[slot].intents, intent)
	}
	return groups
}

// enqueueBySource admits a partitioned batch: each round consults every
// remaining group once, enqueues the clear ones, and polls once while any
// group stays deferred. Healthy sources therefore land in the first round
// while a slowed source waits, and every group lands by the final round —
// deferral, never drop. A timeout after partial admission returns the
// retryable admission error with the admitted count applied; intent
// enqueue is idempotent on the reducer queue conflict key, so the retry
// re-offers the admitted intents without duplicating reducer work.
func (w reducerAdmissionWriter) enqueueBySource(
	ctx context.Context,
	intents []runtime.ReducerIntent,
) (runtime.IntentResult, error) {
	pending := partitionIntentsBySource(intents)
	waitStart := time.Now()
	total := 0
	for len(pending) > 0 {
		var deferred []admissionSourceGroup
		for _, group := range pending {
			reason, failureClass, depth, err := w.admissionDecisionFor(ctx, group.source)
			if err != nil {
				return runtime.IntentResult{}, err
			}
			if reason == "" {
				result, err := w.inner.Enqueue(ctx, group.intents)
				if err != nil {
					return runtime.IntentResult{}, err
				}
				total += result.Count
				continue
			}
			w.recordDeferral(ctx, reason, failureClass, group.source, depth, len(group.intents))
			if timeoutErr := w.admissionTimeoutExceeded(ctx, waitStart, reason, depth, len(group.intents)); timeoutErr != nil {
				return runtime.IntentResult{}, timeoutErr
			}
			deferred = append(deferred, group)
		}
		pending = deferred
		if len(pending) > 0 {
			if err := w.wait(ctx); err != nil {
				return runtime.IntentResult{}, err
			}
		}
	}
	return runtime.IntentResult{Count: total}, nil
}
