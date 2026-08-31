// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recordScopeGrantInlineCap emits the #5408 operator signal when a scoped
// token's grant set overflows the SHAPE-A inline-map cap, and does nothing
// otherwise.
//
// Call it ONCE per read, at the top of a handler, before the scope clauses are
// built. The cap depends only on the token's grant set, so the answer is the
// same for every clause the request builds — and a single request builds that
// disjunction more than once (infraSearchScopeClause alone three times), so
// calling it per clause would record one degraded read as three and make the
// counter useless for "how many reads came back incomplete".
//
// surface names the read that degraded (for example "infra_search",
// "infra_resource_aggregates"). It is a fixed, low-cardinality string chosen by
// the caller, never user input, so the metric cannot be pushed into unbounded
// label cardinality by a request.
//
// The metric's wire label key is "reason" (telemetry.AttrReason), carrying the
// surface as its value — the same shape
// eshu_dp_query_k8s_select_candidate_scan_truncated_total uses. An earlier
// revision described the label as "surface" in the metric description and the
// telemetry-coverage row while emitting "reason", so PromQL written against the
// published contract ({surface="infra_search"}) matched no series. The docs now
// say reason.
//
// The log carries the grant counts because the metric deliberately does not: an
// operator seeing a non-zero rate needs to know WHICH token to fix, and
// grant-set size is the first thing they will ask for. The counts are sizes,
// not ids, so nothing about the grant contents leaks into logs. It logs through
// the package-level slog, matching the rest of this package — an earlier
// revision took a *slog.Logger and every production caller passed nil, which
// made the warning unreachable and the documented detail a promise the code did
// not keep.
func recordScopeGrantInlineCap(
	ctx context.Context,
	instruments *telemetry.Instruments,
	filter repositoryAccessFilter,
	surface string,
) {
	if !filter.GrantInlineCapExceeded() {
		return
	}

	slog.WarnContext(ctx, "scoped token grant set exceeded the inline-map cap; USES and DEFINES-collision admission truncated",
		"surface", surface,
		"granted_repositories", len(filter.AllowedRepositoryIDs),
		"granted_scopes", len(filter.AllowedScopeIDs),
		"inline_term_cap", maxScopeGrantInlineTerms,
		"degradation", "fail_closed_missing_rows",
		"issue", "#5408",
	)

	if instruments != nil && instruments.QueryScopeGrantInlineCapped != nil {
		instruments.QueryScopeGrantInlineCapped.Add(ctx, 1,
			metric.WithAttributes(telemetry.AttrReason(surface)))
	}
}
