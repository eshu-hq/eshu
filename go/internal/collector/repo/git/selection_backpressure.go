// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Per-repo git selection backpressure env contract (#10882 decision 3,
// schedule leg). While the git source is latched slowed on the reducer
// queue, the repo-selection collector stretches its per-repo minimum
// resync interval by the configured multiplier, so intake degrades
// gracefully instead of cliff-edging. Selection is deferred, never
// dropped: skipped repos keep their visit stamp and are visited once
// the interval elapses or the latch releases.
const (
	// GitSelectionMinResyncIntervalEnv bounds how often one repository
	// may be selected for sync. Unset disables the leg: without it the
	// selector keeps every-cycle selection bit-for-bit.
	GitSelectionMinResyncIntervalEnv = "ESHU_GIT_SELECTION_MIN_RESYNC_INTERVAL"
	// GitSelectionStretchMultiplierEnv multiplies the minimum interval
	// while the git source is slowed. Unset takes the proposed default.
	GitSelectionStretchMultiplierEnv = "ESHU_GIT_SELECTION_STRETCH_MULTIPLIER"
	// defaultSelectionStretchMultiplier is the proposed x4 from the
	// #10882 design record. It stays Proposed until S3 measurement
	// confirms it.
	defaultSelectionStretchMultiplier = 4
	// gitSelectionBackpressureSource is the slowed source the schedule
	// leg paces for: the git intake path attributes reducer rows to it
	// through the scope fallback.
	gitSelectionBackpressureSource = "git"
)

// errTestSelectionBackpressure scripts consult failures in tests.
var errTestSelectionBackpressure = errors.New("test selection backpressure consult failure")

// SelectionBackpressureConfig carries the git schedule-stretch
// thresholds (#10882 decision 3). The zero value disables the leg.
type SelectionBackpressureConfig struct {
	// MinResyncInterval bounds how often one repository may be
	// selected for sync, healthy or slowed. Zero disables the leg.
	MinResyncInterval time.Duration
	// StretchMultiplier multiplies the minimum interval while the git
	// source is slowed. The loader defaults it to the proposed x4.
	// Whole multiples only: fractional stretches add no scheduling
	// meaning beyond the next whole multiple.
	StretchMultiplier int
}

// Enabled reports whether schedule stretching is configured.
func (c SelectionBackpressureConfig) Enabled() bool {
	return c.MinResyncInterval > 0
}

// LoadSelectionBackpressureConfig parses the schedule-stretch env
// contract. A set multiplier without a base interval, or a sub-unity
// multiplier, fails startup: the former schedules nothing, the latter
// would visit faster while slowed.
func LoadSelectionBackpressureConfig(getenv func(string) string) (SelectionBackpressureConfig, error) {
	var config SelectionBackpressureConfig
	rawBase := strings.TrimSpace(getenv(GitSelectionMinResyncIntervalEnv))
	rawMult := strings.TrimSpace(getenv(GitSelectionStretchMultiplierEnv))
	if rawBase == "" {
		if rawMult != "" {
			return SelectionBackpressureConfig{}, fmt.Errorf(
				"%s is set without %s: refusing startup rather than scheduling against an undefined interval",
				GitSelectionStretchMultiplierEnv, GitSelectionMinResyncIntervalEnv)
		}
		return SelectionBackpressureConfig{}, nil
	}
	base, err := time.ParseDuration(rawBase)
	if err != nil {
		return SelectionBackpressureConfig{}, fmt.Errorf("parse %s: %w", GitSelectionMinResyncIntervalEnv, err)
	}
	if base <= 0 {
		return SelectionBackpressureConfig{}, fmt.Errorf(
			"%s must be greater than zero", GitSelectionMinResyncIntervalEnv)
	}
	config.MinResyncInterval = base
	config.StretchMultiplier = defaultSelectionStretchMultiplier
	if rawMult != "" {
		mult, err := strconv.Atoi(rawMult)
		if err != nil {
			return SelectionBackpressureConfig{}, fmt.Errorf("parse %s: %w", GitSelectionStretchMultiplierEnv, err)
		}
		if mult < 1 {
			return SelectionBackpressureConfig{}, fmt.Errorf(
				"%s must be at least 1, got %d", GitSelectionStretchMultiplierEnv, mult)
		}
		config.StretchMultiplier = mult
	}
	return config, nil
}

// stretchedInterval reports the effective per-repo minimum interval for
// one selection cycle: the base times the multiplier while slowed, the
// base while healthy, zero when the leg is off or forced reindex runs.
// A consult error reads as healthy (fail open): selection delay must
// never wedge on an unreadable latch.
func (s *NativeRepositorySelector) stretchedInterval(ctx context.Context) time.Duration {
	if s == nil || !s.SelectionBackpressure.Enabled() || s.Config.ForceReindex {
		return 0
	}
	interval := s.SelectionBackpressure.MinResyncInterval
	if s.SelectionSlowed == nil {
		return interval
	}
	slowed, err := s.SelectionSlowed(ctx)
	if err != nil || !slowed {
		if err != nil && s.Logger != nil {
			s.Logger.DebugContext(ctx, "selection backpressure consult failed open",
				"error", err.Error(),
			)
		}
		return interval
	}
	return interval * time.Duration(s.SelectionBackpressure.StretchMultiplier)
}

// SelectionSlowedStore is the slowed-state consult the schedule leg
// paces through. WorkflowControlStore implements it; scripted fakes
// serve the wiring proofs.
type SelectionSlowedStore interface {
	CheckSourceBackpressureForSource(context.Context, string, workflow.SourceBackpressureConfig, time.Time) (workflow.SourceBackpressureDecision, error)
}

// WireSelectionBackpressure arms the schedule leg on a git selector
// from the operator env (#10882 decision 3). A set stretch without the
// slowed-verdict thresholds is a startup error, never a silent no-op:
// the stretch consult cannot slow what it cannot evaluate. Disabled
// leaves the selector untouched. Callers pass the deployment's shared
// workflow control store, which serves the consult.
func (s *NativeRepositorySelector) WireSelectionBackpressure(
	store SelectionSlowedStore,
	getenv func(string) string,
) error {
	selectionBP, err := LoadSelectionBackpressureConfig(getenv)
	if err != nil {
		return err
	}
	if !selectionBP.Enabled() {
		return nil
	}
	sourceBP, err := collector.LoadSourceBackpressureConfig(getenv)
	if err != nil {
		return err
	}
	if !sourceBP.Enabled() {
		return errors.New(
			"git selection stretch is set without per-source backpressure thresholds: refusing startup rather than scheduling against unevaluable state")
	}
	if store == nil {
		return errors.New("git selection stretch requires a slowed-state consult store")
	}
	s.SelectionBackpressure = selectionBP
	s.SelectionSlowed = func(ctx context.Context) (bool, error) {
		decision, err := store.CheckSourceBackpressureForSource(ctx, gitSelectionBackpressureSource, sourceBP, time.Now())
		if err != nil {
			return false, err
		}
		return decision.Slowed, nil
	}
	return nil
}

// applySelectionBackpressure drops recently-visited repositories from
// one selection cycle under the effective interval and stamps the repos
// it keeps. Skipped repos keep their last stamp, so they select again
// once the interval elapses or the latch releases: deferred, never
// dropped. Webhook-triggered selection flows through a separate
// selector and never passes this gate.
func (s *NativeRepositorySelector) applySelectionBackpressure(
	ctx context.Context,
	repositoryIDs []string,
	now time.Time,
) []string {
	interval := s.stretchedInterval(ctx)
	if interval <= 0 {
		return repositoryIDs
	}
	if s.selectionLastVisit == nil {
		s.selectionLastVisit = make(map[string]time.Time, len(repositoryIDs))
	}
	kept := repositoryIDs[:0]
	skipped := 0
	for _, id := range repositoryIDs {
		if last, ok := s.selectionLastVisit[id]; ok && now.Sub(last) < interval {
			skipped++
			continue
		}
		s.selectionLastVisit[id] = now
		kept = append(kept, id)
	}
	if skipped > 0 && s.Logger != nil {
		s.Logger.DebugContext(ctx, "selection backpressure stretched repositories",
			"skipped_repository_count", skipped,
			"selected_repository_count", len(kept),
			"resync_interval", interval.String(),
		)
	}
	return kept
}
