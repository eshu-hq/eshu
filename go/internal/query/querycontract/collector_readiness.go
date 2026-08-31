// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// CollectorListReadinessState classifies one gated supply-chain list answer so a
// caller can tell an empty page produced by an unconfigured feeding collector
// from a genuinely empty page produced by a configured-but-zero collector.
//
// The gated list tools (SBOM/attestation attachments, package-registry
// packages/versions/dependencies/correlations, container-image identities, and
// CI/CD run correlations) are fed by opt-in collectors that are off in a default
// git-only deploy. Without this signal a zero-row page is ambiguous: an agent
// cannot tell "no data exists" from "collector not enabled". This envelope is
// the lightweight per-collector mirror of the vulnerability impact-findings
// readiness envelope, which already distinguishes not_configured for its own
// gated tool.
type CollectorListReadinessState string

const (
	// CollectorListReadinessStateNotConfigured means no enabled instance of the
	// feeding collector is registered, so an empty page reflects a disabled
	// collection lane rather than an absence of matching data.
	CollectorListReadinessStateNotConfigured CollectorListReadinessState = "not_configured"
	// CollectorListReadinessStateReadyZeroResults means the feeding collector is
	// configured and enabled but the bounded query returned no rows, so the empty
	// page is a genuine zero result for the requested scope.
	CollectorListReadinessStateReadyZeroResults CollectorListReadinessState = "ready_zero_results"
	// CollectorListReadinessStateReadyWithResults means the page returned at
	// least one row. Returned rows are themselves proof the collector ran, so the
	// configured probe is not consulted for a non-empty page.
	CollectorListReadinessStateReadyWithResults CollectorListReadinessState = "ready_with_results"
	// CollectorListReadinessStateReadinessUnavailable means the configured probe
	// itself failed. The page is still returned but its emptiness cannot be
	// classified, so callers must not read zero rows as a configured-but-empty
	// result in this state.
	CollectorListReadinessStateReadinessUnavailable CollectorListReadinessState = "readiness_unavailable"
)

// CollectorListReadinessCounts surfaces enough numeric coverage to interpret the
// readiness state without exposing raw payloads.
type CollectorListReadinessCounts struct {
	ResultsReturned  int  `json:"results_returned"`
	ResultsTruncated bool `json:"results_truncated"`
}

// CollectorListReadinessEnvelope is the readiness payload attached to a gated
// supply-chain list response under the "collector_readiness" body key so a UI,
// MCP client, or operator can tell "nothing matched" from "the feeding collector
// is not enabled."
type CollectorListReadinessEnvelope struct {
	State         CollectorListReadinessState  `json:"readiness_state"`
	CollectorKind string                       `json:"collector_kind"`
	Counts        CollectorListReadinessCounts `json:"counts"`
}

// CollectorListReadinessStore reports whether a feeding collector is configured
// and enabled for the active deployment. It is a cheap, bounded lookup the gated
// list handlers run alongside their page so an empty page is never ambiguous.
type CollectorListReadinessStore interface {
	// CollectorConfigured reports whether at least one enabled, non-deactivated
	// instance of the collector kind is registered.
	CollectorConfigured(context.Context, scope.CollectorKind) (bool, error)
}

// BuildCollectorListReadiness combines the bounded page result with the
// collector-configured probe to produce one readiness envelope. It is
// deterministic and never mutates its inputs. A non-empty page always reports
// ready_with_results because returned rows are themselves proof the collector
// ran; the configured probe only disambiguates an empty page.
func BuildCollectorListReadiness(
	kind scope.CollectorKind,
	resultsReturned int,
	truncated bool,
	configured bool,
) CollectorListReadinessEnvelope {
	state := CollectorListReadinessStateNotConfigured
	switch {
	case resultsReturned > 0:
		state = CollectorListReadinessStateReadyWithResults
	case configured:
		state = CollectorListReadinessStateReadyZeroResults
	}
	return CollectorListReadinessEnvelope{
		State:         state,
		CollectorKind: string(kind),
		Counts: CollectorListReadinessCounts{
			ResultsReturned:  resultsReturned,
			ResultsTruncated: truncated,
		},
	}
}

// BuildCollectorListReadinessUnavailable returns a readiness envelope used when
// the collector-configured probe itself failed. The page is still returned but
// the envelope explicitly says emptiness cannot be classified.
func BuildCollectorListReadinessUnavailable(
	kind scope.CollectorKind,
	resultsReturned int,
	truncated bool,
) CollectorListReadinessEnvelope {
	return CollectorListReadinessEnvelope{
		State:         CollectorListReadinessStateReadinessUnavailable,
		CollectorKind: string(kind),
		Counts: CollectorListReadinessCounts{
			ResultsReturned:  resultsReturned,
			ResultsTruncated: truncated,
		},
	}
}
