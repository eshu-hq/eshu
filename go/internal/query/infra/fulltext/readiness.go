// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fulltext

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// DefaultRefreshInterval is how often Watch re-probes readiness.
const DefaultRefreshInterval = 5 * time.Minute

// refreshTimeout bounds one readiness probe: the index state read plus one
// count per infra label.
const refreshTimeout = 2 * time.Minute

// IndexState is the closed set of index states readiness reports.
type IndexState string

const (
	// IndexUnknown means the state was never read or its probe failed.
	IndexUnknown IndexState = "unknown"
	// IndexMissing means the index does not exist.
	IndexMissing IndexState = "missing"
	// IndexOnline means the index is ONLINE.
	IndexOnline IndexState = "online"
	// IndexPopulating means the index is still being populated.
	IndexPopulating IndexState = "populating"
	// IndexFailed means the index is FAILED and must be dropped and rebuilt.
	IndexFailed IndexState = "failed"
)

// Runner runs one read statement and returns its rows. query.GraphQuery
// satisfies it.
type Runner interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// State is one readiness probe result. Its zero value is not ready.
type State struct {
	// Ready is true only when the index is ONLINE and no node is unkeyed.
	Ready bool
	// Index is the index state the probe read.
	Index IndexState
	// Unkeyed is the number of infra nodes without a search key flag.
	Unkeyed int64
	// Reason is ReasonReady when ready, else why not.
	Reason Reason
	// Err is the probe error, when the probe failed.
	Err error
	// CheckedAt is when the probe finished.
	CheckedAt time.Time
}

// Readiness caches whether the full-text read may serve searches. Requests
// read the cached value only; Refresh and Watch do the probing. A nil
// *Readiness is never ready.
type Readiness struct {
	runner      Runner
	logger      *slog.Logger
	instruments *telemetry.Instruments
	state       atomic.Pointer[State]
}

// NewReadiness returns an unprobed Readiness over runner. logger and
// instruments may be nil.
func NewReadiness(runner Runner, logger *slog.Logger, instruments *telemetry.Instruments) *Readiness {
	return &Readiness{runner: runner, logger: logger, instruments: instruments}
}

// Current returns the last probe result, or a not-ready State before the
// first probe.
func (r *Readiness) Current() State {
	if r == nil {
		return State{Index: IndexUnknown, Reason: ReasonNotReady}
	}
	if st := r.state.Load(); st != nil {
		return *st
	}
	return State{Index: IndexUnknown, Reason: ReasonNotReady}
}

// Refresh probes the index state and the unkeyed count, stores the result,
// records the readiness gauges, and logs when the state changed. Any probe
// error makes the result not ready.
func (r *Readiness) Refresh(ctx context.Context) State {
	ctx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()
	st := r.probe(ctx)
	st.CheckedAt = time.Now()
	prev := r.state.Swap(&st)
	r.record(ctx, st)
	if prev == nil || prev.Ready != st.Ready || prev.Index != st.Index || prev.Reason != st.Reason {
		r.logTransition(ctx, prev, st)
	}
	return st
}

func (r *Readiness) probe(ctx context.Context) State {
	st := State{Index: IndexUnknown, Reason: ReasonNotReady}
	raw, err := search.IndexState(ctx, r.runner)
	if err != nil {
		st.Err = err
		return st
	}
	st.Index = indexState(raw)
	counts, err := search.CountUnkeyed(ctx, r.runner)
	if err != nil {
		st.Err = err
		return st
	}
	for _, n := range counts {
		st.Unkeyed += n
	}
	switch {
	case st.Index != IndexOnline:
		st.Reason = ReasonIndexNotOnline
	case st.Unkeyed > 0:
		st.Reason = ReasonUnkeyed
	default:
		st.Ready, st.Reason = true, ReasonReady
	}
	return st
}

func indexState(raw string) IndexState {
	if raw == "" {
		return IndexMissing
	}
	switch strings.ToUpper(raw) {
	case "ONLINE":
		return IndexOnline
	case "POPULATING":
		return IndexPopulating
	case "FAILED":
		return IndexFailed
	default:
		return IndexUnknown
	}
}

func (r *Readiness) record(ctx context.Context, st State) {
	if r.instruments == nil {
		return
	}
	if g := r.instruments.InfraSearchKeyReady; g != nil {
		g.Record(ctx, boolGauge(st.Ready))
	}
	if g := r.instruments.InfraSearchKeyIndexOnline; g != nil {
		g.Record(ctx, boolGauge(st.Index == IndexOnline))
	}
	if g := r.instruments.InfraSearchKeyUnkeyedNodes; g != nil && st.Err == nil {
		g.Record(ctx, st.Unkeyed)
	}
}

func boolGauge(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func (r *Readiness) logTransition(ctx context.Context, prev *State, st State) {
	if r.logger == nil {
		return
	}
	attrs := []any{
		telemetry.EventAttr("query.infra_search_key.readiness_changed"),
		"ready", st.Ready,
		"index_state", string(st.Index),
		"unkeyed_nodes", st.Unkeyed,
		"reason", string(st.Reason),
	}
	if prev != nil {
		attrs = append(attrs, "previous_ready", prev.Ready, "previous_reason", string(prev.Reason))
	}
	if st.Err != nil {
		// The probe read's own graph-read log carries the bounded failure
		// class; the raw driver error can echo statement text.
		attrs = append(attrs, "probe_failed", true)
		r.logger.WarnContext(ctx, "infra search key readiness changed", attrs...)
		return
	}
	r.logger.InfoContext(ctx, "infra search key readiness changed", attrs...)
}

// Watch refreshes readiness now and then every interval until ctx is done.
// interval <= 0 uses DefaultRefreshInterval.
func (r *Readiness) Watch(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultRefreshInterval
	}
	r.Refresh(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Refresh(ctx)
		}
	}
}
