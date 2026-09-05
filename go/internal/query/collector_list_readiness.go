// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// CollectorListReadinessState classifies one gated supply-chain list answer so a
// caller can tell an empty page produced by an unconfigured feeding collector
// from a genuinely empty page produced by a configured-but-zero collector. See
// querycontract.CollectorListReadinessState for the full contract.
type CollectorListReadinessState = querycontract.CollectorListReadinessState

// Compatibility constants preserve the root query package's public contract.
const (
	CollectorListReadinessStateNotConfigured        = querycontract.CollectorListReadinessStateNotConfigured
	CollectorListReadinessStateReadyZeroResults     = querycontract.CollectorListReadinessStateReadyZeroResults
	CollectorListReadinessStateReadyWithResults     = querycontract.CollectorListReadinessStateReadyWithResults
	CollectorListReadinessStateReadinessUnavailable = querycontract.CollectorListReadinessStateReadinessUnavailable
)

// CollectorListReadinessCounts surfaces enough numeric coverage to interpret the
// readiness state without exposing raw payloads.
type CollectorListReadinessCounts = querycontract.CollectorListReadinessCounts

// CollectorListReadinessEnvelope is the readiness payload attached to a gated
// supply-chain list response under the "collector_readiness" body key so a UI,
// MCP client, or operator can tell "nothing matched" from "the feeding collector
// is not enabled."
type CollectorListReadinessEnvelope = querycontract.CollectorListReadinessEnvelope

// CollectorListReadinessStore reports whether a feeding collector is configured
// and enabled for the active deployment. It is a cheap, bounded lookup the gated
// list handlers run alongside their page so an empty page is never ambiguous.
type CollectorListReadinessStore = querycontract.CollectorListReadinessStore

// BuildCollectorListReadiness combines the bounded page result with the
// collector-configured probe to produce one readiness envelope.
func BuildCollectorListReadiness(
	kind scope.CollectorKind,
	resultsReturned int,
	truncated bool,
	configured bool,
) CollectorListReadinessEnvelope {
	return querycontract.BuildCollectorListReadiness(kind, resultsReturned, truncated, configured)
}

// BuildCollectorListReadinessUnavailable returns a readiness envelope used when
// the collector-configured probe itself failed.
func BuildCollectorListReadinessUnavailable(
	kind scope.CollectorKind,
	resultsReturned int,
	truncated bool,
) CollectorListReadinessEnvelope {
	return querycontract.BuildCollectorListReadinessUnavailable(kind, resultsReturned, truncated)
}

// The two functions below deliberately stay in package query rather than moving
// into querycontract with the types above. They are request-time orchestration,
// not contract: they take a context, call a live store, and mutate a response
// body. Two reviewers independently flagged the earlier version of this change
// for putting exactly that behaviour in the dependency-neutral leaf. A family
// package that needs the readiness envelope calls
// querycontract.BuildCollectorListReadiness itself and owns its own attach step.

// attachCollectorListReadiness runs the configured probe for kind and, when a
// store is wired, sets the "collector_readiness" key on body. A nil store leaves
// body untouched so handlers built without the probe keep their existing shape.
// The supplychain hub owns a behavior-identical copy; drift between the two
// trips TestCollectorListReadinessMatchesHub (#6542 review).
func attachCollectorListReadiness(
	ctx context.Context,
	body map[string]any,
	store CollectorListReadinessStore,
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
	store CollectorListReadinessStore,
	kind scope.CollectorKind,
	resultsReturned int,
	truncated bool,
) (CollectorListReadinessEnvelope, bool) {
	if store == nil {
		return CollectorListReadinessEnvelope{}, false
	}
	if resultsReturned > 0 {
		// Rows prove the collector ran; skip the probe entirely so a probe
		// failure cannot mask a demonstrably-working collector.
		return BuildCollectorListReadiness(kind, resultsReturned, truncated, true), true
	}
	configured, err := store.CollectorConfigured(ctx, kind)
	if err != nil {
		return BuildCollectorListReadinessUnavailable(kind, resultsReturned, truncated), true
	}
	return BuildCollectorListReadiness(kind, resultsReturned, truncated, configured), true
}
