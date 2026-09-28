// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// searchVectorReadyFreshnessWindow bounds how long after the last
// search-vector build sweep publishes search_vector_ready the read is still
// considered fresh. SearchVectorBuildRunner polls on a short cadence (~30s
// default, see defaultSearchVectorBuildPollInterval in
// go/internal/reducer/searchvector/search_vector_build_runner.go) and only publishes the
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

// truthWithSearchVectorFreshness builds the response truth envelope and, when
// a search-vector-ready reader is configured AND the resolved mode is
// vector-backed (semantic or hybrid — the same gate semanticSearchBackend
// uses to decide whether LocalHybrid's vector path is even wired in),
// downgrades it from the search-vector build sweep's search_vector_ready
// watermark so an outstanding build is attributable (pending_search_vector)
// instead of served as silently fresh. mode:"keyword" is served entirely by
// the deterministic lexical index and is never degraded by vector/index
// readiness (see semanticSearchDegradation), so it must never be downgraded
// by a pending search-vector build. Mirrors applyWinnersFreshness's call-site
// shape in findings_handler.go: a probe failure reports
// the envelope unavailable rather than dropping the already-served results.
func (h *SemanticSearchHandler) truthWithSearchVectorFreshness(r *http.Request, mode searchbench.Mode) *querycontract.TruthEnvelope {
	truth := h.truth()
	if h.SearchVectorReady == nil || !searchVectorBackedMode(mode) {
		return truth
	}
	ctx, span := semanticSearchTracer.Start(r.Context(), telemetry.SpanQuerySemanticSearchVectorReady)
	watermark, err := h.SearchVectorReady.SearchVectorReadyWatermark(ctx)
	outcome := "missing"
	if err != nil {
		outcome = "error"
		span.SetStatus(codes.Error, "watermark probe failed")
	} else if watermark.Present {
		outcome = "present"
	}
	span.SetAttributes(attribute.String("search.vector_ready.outcome", outcome))
	span.End()
	applySearchVectorFreshness(truth, watermark, err, time.Now())
	return truth
}

// searchVectorBackedMode reports whether mode retrieves through the
// search-vector index (semantic or hybrid), matching the gate
// semanticSearchBackend uses to decide whether LocalHybrid's vector path is
// engaged. mode:"keyword" is served entirely by the deterministic lexical
// index and never touches search-vector state.
func searchVectorBackedMode(mode searchbench.Mode) bool {
	return mode == searchbench.ModeSemantic || mode == searchbench.ModeHybrid
}

// applySearchVectorFreshness downgrades the truth envelope when the
// search-vector build sweep has never published search_vector_ready, or its
// last publish is behind the freshness window, or the watermark probe itself
// failed. It is a no-op when no reader is configured (fr.Signaled is false)
// and when the watermark is present and within the window. now is injected
// for deterministic tests.
func applySearchVectorFreshness(truth *querycontract.TruthEnvelope, fr SearchVectorReadyFreshness, probeErr error, now time.Time) {
	if truth == nil || !fr.Signaled {
		return
	}
	if probeErr != nil {
		truth.Freshness = querycontract.TruthFreshness{
			State:  querycontract.FreshnessUnavailable,
			Detail: "could not determine search-vector build sweep freshness",
		}
		return
	}
	if !fr.Present {
		// No watermark at all: the search-vector build sweep has never
		// completed a bounded sweep with zero pending scopes.
		truth.Freshness = querycontract.TruthFreshness{
			State:  querycontract.FreshnessBuilding,
			Detail: "search-vector build sweep has not published a search_vector_ready signal yet",
		}
		querycontract.WithFreshnessCause(truth, querycontract.FreshnessCausePendingSearchVector)
		return
	}
	materializedAt := fr.MaterializedAt.UTC()
	observedAt := materializedAt.Format(time.RFC3339)
	if now.UTC().Sub(materializedAt) <= searchVectorReadyFreshnessWindow {
		truth.Freshness.ObservedAt = observedAt
		return
	}
	truth.Freshness = querycontract.TruthFreshness{
		State:      querycontract.FreshnessStale,
		ObservedAt: observedAt,
		Detail:     "search-vector build sweep is behind its search_vector_ready publish cadence",
	}
	querycontract.WithFreshnessCause(truth, querycontract.FreshnessCausePendingSearchVector)
}
