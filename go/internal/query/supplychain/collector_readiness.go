// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Family-local copy of root package query's attachCollectorListReadiness and
// collectorListReadiness. Root's collector_list_readiness.go documents this
// exact split: the attach step is request-time orchestration that stays out
// of the dependency-neutral leaf, so each handler family owns its own copy
// and calls querycontract.BuildCollectorListReadiness itself (packagereg
// precedent). The hub cannot call the root helpers without an import cycle.
// Both functions MUST stay behavior-identical to their root sources (named
// above); do not extend them with family-specific semantics. Drift trips
// root's TestCollectorListReadinessMatchesHub parity test (#6542 review),
// which serves both copies over the shared probe matrix.

// attachCollectorListReadiness runs the configured probe for kind and, when a
// store is wired, sets the "collector_readiness" key on body. A nil store leaves
// body untouched so handlers built without the probe keep their existing shape.
func attachCollectorListReadiness(
	ctx context.Context,
	body map[string]any,
	store querycontract.CollectorListReadinessStore,
	kind scope.CollectorKind,
	resultsReturned int,
	truncated bool,
) {
	envelope, ok := collectorListReadiness(ctx, store, kind, resultsReturned, truncated)
	if !ok {
		return
	}
	body["collector_readiness"] = envelope
}

// collectorListReadiness builds the readiness envelope for a page of
// resultsReturned rows. A nil store yields no envelope (the optional readiness
// field stays unset). A non-empty page is classified ready_with_results without
// consulting the probe: returned rows are themselves proof the collector ran, so
// a stale or failing probe must never downgrade an already-evidenced page. The
// configured probe is consulted only for an empty page, where it disambiguates
// not_configured from ready_zero_results; a probe error there yields the
// readiness_unavailable envelope so the page is never dropped. The boolean
// reports whether an envelope was produced.
func collectorListReadiness(
	ctx context.Context,
	store querycontract.CollectorListReadinessStore,
	kind scope.CollectorKind,
	resultsReturned int,
	truncated bool,
) (querycontract.CollectorListReadinessEnvelope, bool) {
	if store == nil {
		return querycontract.CollectorListReadinessEnvelope{}, false
	}
	if resultsReturned > 0 {
		// Rows prove the collector ran; skip the probe entirely so a probe
		// failure cannot mask a demonstrably-working collector.
		return querycontract.BuildCollectorListReadiness(kind, resultsReturned, truncated, true), true
	}
	configured, err := store.CollectorConfigured(ctx, kind)
	if err != nil {
		return querycontract.BuildCollectorListReadinessUnavailable(kind, resultsReturned, truncated), true
	}
	return querycontract.BuildCollectorListReadiness(kind, resultsReturned, truncated, configured), true
}
