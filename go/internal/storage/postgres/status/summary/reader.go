// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"time"
)

// flightKey is the Flight key of a ModelReader: it serves one model, so one key
// is enough.
const flightKey = "live"

// ModelReader is the process-wide reader of one stored model (#7009): its
// settings and the Flight that shares one live statement among concurrent
// fallbacks.
// Build it once at process startup and attach it to every status store; the
// API and MCP server build a store per snapshot transaction, so a Flight owned
// by a store would never share anything. T is the decoded model.
type ModelReader[T any] struct {
	// Config holds the reader flag and the stale limit.
	Config ReadConfig
	flight Flight[liveResult[T]]
}

// liveResult is a live result and the clock it was evaluated at, which a
// follower of a shared call reports as its as_of.
type liveResult[T any] struct {
	value T
	asOf  time.Time
}

// NewModelReader loads the reader settings from the environment (default off).
// An invalid stale_after while the reader is on is an error the caller returns
// at startup.
func NewModelReader[T any](getenv func(string) string) (*ModelReader[T], error) {
	cfg, err := LoadReadConfig(getenv)
	if err != nil {
		return nil, err
	}
	return NewModelReaderWithConfig[T](cfg), nil
}

// NewModelReaderWithConfig builds a ModelReader from explicit settings.
func NewModelReaderWithConfig[T any](cfg ReadConfig) *ModelReader[T] {
	return &ModelReader[T]{Config: cfg}
}

// Waiting reports how many reads have joined the in-flight shared live
// statement as followers (including any that later left on their own context),
// or -1 when none is in flight. Tests use it as a join barrier to hold the
// leader until every follower has joined; it is not a count of reads still
// blocked.
func (r *ModelReader[T]) Waiting() int {
	return r.flight.Waiting(flightKey)
}

// Hooks are the model-specific steps of ModelReader.Read.
type Hooks[T any] struct {
	// Select runs Select on the status read's queryer, with the caller's read
	// labeling around it.
	Select func(ctx context.Context) (Selection, error)
	// Decode decodes the served entries with the live read's decoder.
	Decode func([]Entry) (T, error)
	// Live runs the live statement, with the caller's read labeling.
	Live func(ctx context.Context) (T, error)
	// Clone copies a decoded value so shared results never alias.
	Clone func(T) T
	// Observe records one read decision.
	Observe func(ctx context.Context, selection Selection)
}

// Result is a decoded model and where it came from.
type Result[T any] struct {
	Value T
	// Source and Reason say which path answered and why.
	Source Source
	Reason Reason
	// AsOf is the time the data is true at: the stored row's as_of for the
	// model, otherwise the live statement's clock (the leader's, for a read
	// that shared another's live statement).
	AsOf time.Time
	// Age is how old the served data was at the read; zero for live data.
	Age time.Duration
}

// Read returns the model from the stored row when every fence passes, and
// otherwise from the live statement, labeled with a typed reason. One answer is
// never a mix of the two. A database error reading the row is returned, not
// hidden behind a fallback. A nil ModelReader or one that is off runs the live
// statement. Concurrent fallbacks share one live statement; a follower reports
// the leader's clock as its AsOf and stops waiting when its own ctx ends.
func (r *ModelReader[T]) Read(ctx context.Context, asOf time.Time, h Hooks[T]) (Result[T], error) {
	selection := Selection{Source: SourceLive, Reason: ReasonFlagOff}
	if r != nil && r.Config.Enabled {
		var err error
		if selection, err = h.Select(ctx); err != nil {
			return Result[T]{}, err
		}
		if selection.Source == SourceModel {
			if value, err := h.Decode(selection.Entries); err == nil {
				h.Observe(ctx, selection)
				return Result[T]{Value: value, Source: SourceModel, Reason: ReasonFresh, AsOf: selection.AsOf.UTC(), Age: selection.Age}, nil
			}
			selection.Source, selection.Reason, selection.Entries = SourceLiveFallback, ReasonDecode, nil
		}
	}
	h.Observe(ctx, selection)
	result := Result[T]{Source: selection.Source, Reason: selection.Reason, AsOf: asOf}
	live := func() (liveResult[T], error) {
		value, err := h.Live(ctx)
		return liveResult[T]{value: value, asOf: asOf}, err
	}
	if selection.Source != SourceLiveFallback {
		lr, err := live()
		result.Value = lr.value
		return result, err
	}
	lr, shared, err := r.flight.Do(ctx, flightKey, live)
	result.Value = lr.value
	if shared {
		result.Value = h.Clone(result.Value)
		result.AsOf = lr.asOf
	}
	return result, err
}
