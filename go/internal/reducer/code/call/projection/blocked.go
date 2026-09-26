// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// Bounded reasons a lane-wide gate holds the code-call lane shut. They are the
// closed "reason" label on eshu_dp_shared_projection_lane_blocked_total and
// eshu_dp_shared_projection_lane_blocking_scopes.
const (
	// BlockedReasonCanonicalCodeQuiescence means some code scope's active
	// generation has not committed its canonical nodes (#6184, #7133).
	BlockedReasonCanonicalCodeQuiescence = "canonical_code_quiescence"
	// BlockedReasonReducerGraphWork means graph-writing reducer work is still
	// pending or running on a local-authoritative profile.
	BlockedReasonReducerGraphWork = "reducer_graph_work_active"
)

const (
	// blockedReportInterval bounds how often a still-blocked lane re-logs and
	// re-samples its blockers. The gate itself is checked every cycle.
	blockedReportInterval = time.Minute
	// blockedScopeSampleLimit caps the scope ids named in one log line.
	blockedScopeSampleLimit = 10
)

// CanonicalCodeQuiescenceDescriber is an optional port that names the scopes
// holding the canonical-code quiescence gate. postgres.ReducerGraphDrain
// implements it. The runner calls it only when a blocked episode starts and
// then at most once per blockedReportInterval, never per cycle, so the
// per-cycle gate stays a short-circuit EXISTS.
type CanonicalCodeQuiescenceDescriber interface {
	DescribeUncommittedCanonicalCodeScopes(ctx context.Context, limit int) (int, []string, error)
}

// laneBlockState tracks one lane-wide blocked episode so concurrent partition
// workers report it once per interval instead of once per partition cycle.
type laneBlockState struct {
	mu         sync.Mutex
	reason     string
	since      time.Time
	lastReport time.Time
}

// blockObservation is the result of one blocked cycle.
type blockObservation struct {
	// report is true when this cycle should log and sample: the episode just
	// started, the reason changed, or blockedReportInterval has passed since
	// the last report.
	report bool
	// blockedFor is the current episode's age in seconds.
	blockedFor float64
	// replaced is the prior episode's reason when this cycle switched reasons,
	// so its gauge can be zeroed and its close logged; otherwise empty.
	replaced string
	// replacedFor is the replaced episode's age in seconds at the switch.
	replacedFor float64
}

// observe records a blocked cycle at now.
func (s *laneBlockState) observe(now time.Time, reason string) blockObservation {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reason != reason {
		obs := blockObservation{report: true}
		if s.reason != "" {
			obs.replaced = s.reason
			obs.replacedFor = now.Sub(s.since).Seconds()
		}
		s.reason = reason
		s.since = now
		s.lastReport = now
		return obs
	}
	obs := blockObservation{blockedFor: now.Sub(s.since).Seconds()}
	if now.Sub(s.lastReport) >= blockedReportInterval {
		s.lastReport = now
		obs.report = true
	}
	return obs
}

// release ends the episode. released is true only for the first open cycle
// after a blocked episode, with that episode's reason and age.
func (s *laneBlockState) release(now time.Time) (released bool, reason string, blockedFor float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reason == "" {
		return false, "", 0
	}
	reason, blockedFor = s.reason, now.Sub(s.since).Seconds()
	s.reason = ""
	return true, reason, blockedFor
}

// quiescenceDescriber returns the describer for the dependency the gate
// actually consults, or nil. It mirrors projectionLaneBlocked: ReducerGraphDrain
// when wired, otherwise CanonicalQuiescence. It never falls through to the other
// dependency, because that one is not what holds the lane and would name the
// wrong blockers.
func (r *Runner) quiescenceDescriber() CanonicalCodeQuiescenceDescriber {
	var gate any = r.CanonicalQuiescence
	if r.ReducerGraphDrain != nil {
		gate = r.ReducerGraphDrain
	}
	describer, _ := gate.(CanonicalCodeQuiescenceDescriber)
	return describer
}

func laneAttributes(reason string) metric.MeasurementOption {
	return metric.WithAttributes(
		telemetry.AttrDomain(reducercontract.DomainCodeCalls),
		telemetry.AttrReason(reason),
	)
}

// recordCodeCallLaneBlocked makes a blocked cycle visible (#7133): the lane
// sat shut for eight days on ops-qa with no log line and no metric. Every
// blocked partition cycle counts; the log line and the blocking-scope sample
// are rate-limited per episode. A sample failure is logged, never returned:
// the gate already answered and the sample is operator detail only.
func (r *Runner) recordCodeCallLaneBlocked(ctx context.Context, reason string) {
	if r.Instruments != nil && r.Instruments.SharedProjectionLaneBlocked != nil {
		r.Instruments.SharedProjectionLaneBlocked.Add(ctx, 1, laneAttributes(reason))
	}
	obs := r.blockedLane.observe(time.Now(), reason)
	if obs.replaced != "" {
		r.zeroLaneBlockerCount(ctx, obs.replaced)
		r.logLaneEpisodeClosed(ctx, obs.replaced, obs.replacedFor, reason)
	}
	if !obs.report {
		return
	}

	logAttrs := []any{
		log.Domain(string(reducercontract.DomainCodeCalls)),
		slog.String("blocked_reason", reason),
		slog.Float64("blocked_seconds", obs.blockedFor),
		telemetry.PhaseAttr(telemetry.PhaseShared),
	}
	if describer := r.quiescenceDescriber(); reason == BlockedReasonCanonicalCodeQuiescence && describer != nil {
		total, scopeIDs, err := describer.DescribeUncommittedCanonicalCodeScopes(ctx, blockedScopeSampleLimit)
		if err != nil {
			logAttrs = append(logAttrs, log.Err(err))
		} else {
			if r.Instruments != nil && r.Instruments.SharedProjectionLaneBlockerCount != nil {
				r.Instruments.SharedProjectionLaneBlockerCount.Record(ctx, int64(total), laneAttributes(reason))
			}
			logAttrs = append(logAttrs,
				slog.Int("blocking_scope_count", total),
				slog.Any("blocking_scope_ids", scopeIDs),
				slog.Int("blocking_scope_sample_limit", blockedScopeSampleLimit),
			)
		}
	}
	if r.Logger != nil {
		r.Logger.WarnContext(ctx, "code call projection lane blocked", logAttrs...)
	}
}

// recordCodeCallLaneReleased logs the first open cycle after a blocked
// episode and zeroes the blocking-scope gauge for that reason.
func (r *Runner) recordCodeCallLaneReleased(ctx context.Context) {
	released, reason, blockedFor := r.blockedLane.release(time.Now())
	if !released {
		return
	}
	r.zeroLaneBlockerCount(ctx, reason)
	r.logLaneEpisodeClosed(ctx, reason, blockedFor, "")
}

// logLaneEpisodeClosed writes the close record for a blocked episode: the lane
// opened (replacedBy empty) or the blocking reason switched to replacedBy. Both
// carry the closed reason and its age so an operator can total stall time.
func (r *Runner) logLaneEpisodeClosed(ctx context.Context, reason string, blockedFor float64, replacedBy string) {
	if r.Logger == nil {
		return
	}
	attrs := []any{
		log.Domain(string(reducercontract.DomainCodeCalls)),
		slog.String("blocked_reason", reason),
		slog.Float64("blocked_seconds", blockedFor),
		telemetry.PhaseAttr(telemetry.PhaseShared),
	}
	if replacedBy != "" {
		attrs = append(attrs, slog.String("replaced_by", replacedBy))
	}
	r.Logger.InfoContext(ctx, "code call projection lane released", attrs...)
}

func (r *Runner) zeroLaneBlockerCount(ctx context.Context, reason string) {
	if r.Instruments != nil && r.Instruments.SharedProjectionLaneBlockerCount != nil {
		r.Instruments.SharedProjectionLaneBlockerCount.Record(ctx, 0, laneAttributes(reason))
	}
}
