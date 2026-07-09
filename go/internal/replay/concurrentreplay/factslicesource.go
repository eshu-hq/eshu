// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package concurrentreplay

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// FactLoader loads the recorded fact envelopes for one scope generation. It
// matches ifa.FactLoader (go/internal/ifa/odu.go:21-23) exactly and is
// satisfied by postgres.FactStore.LoadFacts
// (go/internal/storage/postgres/facts.go:96-102). The interface is
// duplicated locally rather than imported from internal/ifa: this package's
// README ownership boundary forbids a replay -> ifa import (Ifá is a
// consumer of replayed facts, not a dependency of the replay plumbing), so
// FactLoader is redeclared here at the same shape instead.
type FactLoader interface {
	LoadFacts(context.Context, projector.ScopeGenerationWork) ([]facts.Envelope, error)
}

// FactSliceSource is a collector.Source that replays git-collector facts
// from recorded fact_records slices, one projector.ScopeGenerationWork
// descriptor at a time, via a FactLoader such as postgres.FactStore. Unlike
// cassette.Source (an in-memory tape), collector-git is live-only and has no
// cassette format of its own; git-derived facts instead live in the
// fact_records table that the original ingestion run already wrote, and
// FactSliceSource's job is to replay exactly those rows back out through the
// same collector.Source contract the rest of this package's concurrent
// replay Driver already expects.
//
// FactSliceSource is deliberately UNSYNCHRONIZED, mirroring cassette.Source:
// its index field is unguarded per-instance state, so calling Next
// concurrently on a bare *FactSliceSource races. This package's Source
// wrapper (see source.go) is the single synchronization point intended for
// concurrent use — wrap a FactSliceSource in NewSource before handing it to
// multiple Driver workers, exactly as driver_test.go's
// TestFactSliceSourceUnderDriver does.
type FactSliceSource struct {
	loader FactLoader
	slices []projector.ScopeGenerationWork
	index  int
}

// NewFactSliceSource returns a FactSliceSource that replays slices in order,
// loading each one's facts from loader on the Next call that reaches it. The
// returned source is a one-shot delegate: once every entry in slices has been
// served, Next reports permanent exhaustion (ok=false, err=nil) and never
// restarts, matching the one-shot contract concurrentreplay.Source assumes
// of any delegate it wraps.
func NewFactSliceSource(loader FactLoader, slices []projector.ScopeGenerationWork) *FactSliceSource {
	return &FactSliceSource{loader: loader, slices: slices}
}

// Next returns the CollectedGeneration for the next configured
// ScopeGenerationWork descriptor, loading its recorded facts from the
// wrapped FactLoader via collector.FactsFromSlice so the resulting
// CollectedGeneration is built the same way cassette-replayed generations
// are — an already-filled, closed Facts channel a Committer can drain
// without a background goroutine. Once every descriptor has been served,
// Next returns the zero CollectedGeneration, ok=false, err=nil on every
// subsequent call; it never restarts.
//
// A loader error is wrapped with %w and returned immediately; the source's
// index is still advanced past the failed descriptor, so a caller that
// chooses to keep calling Next (rather than treating the error as fatal, as
// concurrentreplay.Source and Driver do) will not be handed the same failed
// descriptor twice.
func (s *FactSliceSource) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	if s.index >= len(s.slices) {
		return collector.CollectedGeneration{}, false, nil
	}

	work := s.slices[s.index]
	s.index++

	envs, err := s.loader.LoadFacts(ctx, work)
	if err != nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("concurrentreplay: fact slice source load facts for scope %q generation %q: %w",
			work.Scope.ScopeID, work.Generation.GenerationID, err)
	}

	return collector.FactsFromSlice(work.Scope, work.Generation, envs), true, nil
}
