// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"time"
)

// searchVectorReadyFreshnessWindow bounds how long after the last
// search-vector build sweep publishes search_vector_ready the read is still
// considered fresh. SearchVectorBuildRunner polls on a short cadence (~30s
// default, see defaultSearchVectorBuildPollInterval in
// go/internal/reducer/search_vector_build_runner.go) and only publishes the
// watermark when a bounded sweep completes with zero pending scopes, so a
// healthy signal is always within roughly one poll cadence of now. The window
// allows several cadences of headroom for a transient lease handoff or a slow
// sweep; a watermark older than this means the build sweep has fallen behind,
// so the read is reported stale (pending_search_vector) instead of served as
// silently fresh.
const searchVectorReadyFreshnessWindow = 2 * time.Minute

// SearchVectorReadyFreshness reports the freshness of the search-vector build
// sweep's search_vector_ready completion signal. Signaled is false when the
// handler has no configured reader for the signal (legacy/local configs
// without the Postgres-backed watermark), in which case the caller MUST leave
// the truth envelope fresh — the probe costs nothing until a reader is wired.
// When Signaled is true, Present reports whether the runner has ever
// published the watermark (i.e. completed at least one bounded sweep with
// zero pending scopes) and MaterializedAt carries the last publish time
// (valid only when Present is true).
type SearchVectorReadyFreshness struct {
	Signaled       bool
	Present        bool
	MaterializedAt time.Time
}

// SemanticSearchVectorReadyReader is the optional capability a semantic search
// backend implements when it can report the search-vector build sweep's
// search_vector_ready watermark. The handler type-asserts it so a backend (or
// test double) that does not implement it simply keeps the fresh envelope.
type SemanticSearchVectorReadyReader interface {
	SearchVectorReadyWatermark(context.Context) (SearchVectorReadyFreshness, error)
}

// applySearchVectorFreshness downgrades the truth envelope when the
// search-vector build sweep has never published search_vector_ready, or its
// last publish is behind the freshness window, or the watermark probe itself
// failed. It is a no-op when no reader is configured (fr.Signaled is false)
// and when the watermark is present and within the window. now is injected
// for deterministic tests.
func applySearchVectorFreshness(truth *TruthEnvelope, fr SearchVectorReadyFreshness, probeErr error, now time.Time) {
	if truth == nil || !fr.Signaled {
		return
	}
	if probeErr != nil {
		truth.Freshness = TruthFreshness{
			State:  FreshnessUnavailable,
			Detail: "could not determine search-vector build sweep freshness",
		}
		return
	}
	if !fr.Present {
		// No watermark at all: the search-vector build sweep has never
		// completed a bounded sweep with zero pending scopes.
		truth.Freshness = TruthFreshness{
			State:  FreshnessBuilding,
			Detail: "search-vector build sweep has not published a search_vector_ready signal yet",
		}
		WithFreshnessCause(truth, FreshnessCausePendingSearchVector)
		return
	}
	materializedAt := fr.MaterializedAt.UTC()
	observedAt := materializedAt.Format(time.RFC3339)
	if now.UTC().Sub(materializedAt) <= searchVectorReadyFreshnessWindow {
		truth.Freshness.ObservedAt = observedAt
		return
	}
	truth.Freshness = TruthFreshness{
		State:      FreshnessStale,
		ObservedAt: observedAt,
		Detail:     "search-vector build sweep is behind its search_vector_ready publish cadence",
	}
	WithFreshnessCause(truth, FreshnessCausePendingSearchVector)
}
