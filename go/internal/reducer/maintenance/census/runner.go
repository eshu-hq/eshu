// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package census

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph/anchor"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Runner samples the id-anchor census on an interval (#7212): the
// number of graph nodes with an id that the labeled Neo4j entity-context anchor
// cannot reach. The scan is read-only and idempotent, so replicas need no
// lease: each reports its own snapshot, read at its own time, and the cost is
// one bounded scan per replica per interval. The result is recorded after the
// pass, never during a metrics scrape.
type Runner struct {
	Source      anchor.CensusSource
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
	// Interval is the delay between passes; Timeout bounds one pass.
	Interval time.Duration
	Timeout  time.Duration
	// Wait and Now are test seams; nil means a timer and time.Now.
	Wait func(context.Context, time.Duration) error
	Now  func() time.Time
}

// Run takes the first pass at once, so the startup log line carries the count,
// then one pass per interval until ctx ends.
func (r *Runner) Run(ctx context.Context) error {
	first := true
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.RunOnce(ctx, first)
		first = false
		if err := r.wait(ctx, r.Interval); err != nil {
			return err
		}
	}
}

// RunOnce takes one census under its own deadline and records the outcome. A
// failed pass leaves the unreachable and id-bearing gauges and the last-success
// time at their last good values; a successful pass records the pair together,
// so a metrics reader can tell a healthy zero (id-bearing above zero) from an
// empty graph. first marks the startup pass in the log line.
func (r *Runner) RunOnce(ctx context.Context, first bool) {
	passCtx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	started := r.clock()
	census, err := r.Source.AnchorCensus(passCtx)
	duration := r.clock().Sub(started)
	if ctx.Err() != nil {
		return
	}
	outcome := "ok"
	if err != nil {
		outcome = "failed"
	}
	if r.Instruments != nil {
		attrs := metric.WithAttributes(attribute.String(telemetry.MetricDimensionOutcome, outcome))
		r.Instruments.GraphIDAnchorCensusPasses.Add(ctx, 1, attrs)
		r.Instruments.GraphIDAnchorCensusDuration.Record(ctx, duration.Seconds(), attrs)
		if err == nil {
			r.Instruments.GraphIDAnchorUnreachableNodes.Record(ctx, census.Residual)
			r.Instruments.GraphIDAnchorIDBearingNodes.Record(ctx, census.IDBearing)
			r.Instruments.GraphIDAnchorCensusLastSuccess.Record(ctx, r.clock().Unix())
		}
	}
	if r.Logger == nil {
		return
	}
	if err != nil {
		r.Logger.Error("id anchor census pass failed",
			slog.String("error", err.Error()),
			slog.Bool("first_pass", first),
			slog.Float64("duration_seconds", duration.Seconds()),
			telemetry.FailureClassAttr("id_anchor_census_error"),
			telemetry.PhaseAttr(telemetry.PhaseReduction))
		return
	}
	level := slog.LevelInfo
	if census.Residual > 0 {
		level = slog.LevelWarn
	}
	r.Logger.Log(ctx, level, "id anchor census",
		slog.Bool("snapshot", true),
		slog.Bool("first_pass", first),
		slog.Int64("id_bearing_nodes", census.IDBearing),
		slog.Int64("via_uid_nodes", census.ViaUIDOnly),
		slog.Int64("via_id_nodes", census.ViaID),
		slog.Int64("unreachable_nodes", census.Residual),
		slog.Float64("duration_seconds", duration.Seconds()),
		telemetry.PhaseAttr(telemetry.PhaseReduction))
}

func (r *Runner) clock() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Runner) wait(ctx context.Context, d time.Duration) error {
	if r.Wait != nil {
		return r.Wait(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
