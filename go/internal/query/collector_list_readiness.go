// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
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

// The attach step used to live here alongside the types above. It is
// request-time orchestration, not contract: it takes a context, calls a live
// store, and mutates a response body. Two reviewers independently flagged the
// earlier version of this change for putting exactly that behaviour in the
// dependency-neutral leaf, so each handler family owns its own copy and calls
// querycontract.BuildCollectorListReadiness itself. The last package-query
// handler using the root copy moved to internal/query/cicd (#6642), so the
// root copy is deleted rather than kept as dead code. The family copies
// (supplychain hub, package registry, cicd) must stay behavior-identical,
// pinned by TestCollectorListReadinessMatchesHub (#6542 review).
