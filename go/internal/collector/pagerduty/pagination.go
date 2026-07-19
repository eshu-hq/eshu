// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import "context"

// Default triple-bound for PagerDuty classic offset pagination. PagerDuty's
// REST v2 list endpoints stop reliably serving pages past offset 10000
// (https://developer.pagerduty.com/docs/pagination); defaultMaxPaginationRecords
// stays well below that ceiling so a bound-hit truncation always occurs before
// the provider itself would refuse the request.
const (
	defaultMaxPaginationPages   = 10
	defaultMaxPaginationRecords = 1000
	// defaultMaxPaginationRecordsBound is the configuration-time ceiling for
	// TargetConfig.PaginationMaxRecords (validateTarget in source.go), kept in
	// lockstep with maxPagerDutyPaginationRecords in
	// internal/workflow/pagerduty_config.go.
	defaultMaxPaginationRecordsBound = 5000
	// pagerDutyMaxPageSize is PagerDuty's per-request page ceiling (the
	// provider clamps a larger `limit` to this). It is the assumed natural
	// page size when a target sets no explicit per-resource limit, used to
	// decide when the remaining record allowance must shrink the final page.
	pagerDutyMaxPageSize = 100
)

// paginationBounds triple-bounds one PagerDuty offset-paginated resource
// fetch: a page-count ceiling and a record-count ceiling. The caller's
// context deadline is the third bound, enforced by paginateOffset checking
// ctx.Err() before every page request.
type paginationBounds struct {
	maxPages   int
	maxRecords int
}

// paginationBoundsForTarget resolves the configured pagination bounds for a
// target, falling back to safe defaults when unset or non-positive.
func paginationBoundsForTarget(target TargetConfig) paginationBounds {
	maxPages := target.PaginationMaxPages
	if maxPages <= 0 {
		maxPages = defaultMaxPaginationPages
	}
	maxRecords := target.PaginationMaxRecords
	if maxRecords <= 0 {
		maxRecords = defaultMaxPaginationRecords
	}
	return paginationBounds{maxPages: maxPages, maxRecords: maxRecords}
}

// paginationRequestLimit returns the `limit` query value one page should
// request so the final page never over-fetches past the remaining record
// allowance. configuredLimit is the target's per-resource page size (0 =
// unset, use the provider default). remaining is maxRecords minus records
// already fetched. A zero return means "send no limit param" (provider
// default page size).
//
// The remaining allowance only shrinks the request when it is smaller than
// the page that would otherwise be fetched: with a configured limit it caps
// to min(configuredLimit, remaining); with no configured limit it caps only
// when remaining is below the provider page ceiling (otherwise it returns 0
// to preserve the pre-pagination behavior of omitting `limit`). This is the
// over-fetch-avoidance half of the maxRecords bound; paginateOffset still
// trims the accumulated output defensively in case a server ignores the cap.
func paginationRequestLimit(configuredLimit, remaining int) int {
	natural := configuredLimit
	if natural <= 0 {
		natural = pagerDutyMaxPageSize
	}
	limit := natural
	if remaining > 0 && remaining < limit {
		limit = remaining
	}
	if configuredLimit <= 0 && limit >= pagerDutyMaxPageSize {
		// No configured limit and no binding remaining cap: keep the original
		// behavior of omitting the `limit` param (provider default page size).
		return 0
	}
	return limit
}

// paginateOffset drives one PagerDuty classic offset-paginated resource
// fetch. fetchPage requests one page at the given offset, honoring the
// requestLimit hint (a positive value it must apply to the request's `limit`
// so the final page cannot over-fetch past maxRecords; zero means "no cap /
// provider default"), and returns how many items it decoded and whether the
// server signaled another page is available (the response's "more" field).
// PagerDuty's classic REST v2 pagination omits a reliable "total" unless the
// caller opts in, so "more" is the only signal paginateOffset trusts.
//
// paginateOffset stops in exactly one of these ways:
//   - the server exhausts "more" (truncated=false: every available record was
//     read),
//   - fetchPage returns zero items while still signaling "more" (a defensive
//     terminal case for a non-advancing/malformed page; truncated=false
//     because paginateOffset cannot distinguish "provider bug" from "no more
//     data" and must not spin forever on either),
//   - the configured page or record bound is hit while more pages remain
//     (truncated=true: the caller stopped early and observed truth is
//     incomplete); when the record bound is exceeded the returned records is
//     capped to exactly maxRecords so the caller trims its output to the cap,
//   - the context is canceled or times out (returns ctx.Err(); checked before
//     every page request so a cancellation between pages never issues one
//     more network call).
//
// The next offset is always computed locally as the running sum of items
// already fetched, never from a server-echoed offset, so a provider that
// echoes a stale or non-advancing offset cannot cause paginateOffset to loop.
// records is always the number of items the caller should keep: it equals the
// total appended except when the record bound trims it down to maxRecords.
func paginateOffset(
	ctx context.Context,
	bounds paginationBounds,
	fetchPage func(ctx context.Context, offset int, requestLimit int) (itemCount int, more bool, err error),
) (pages int, records int, truncated bool, err error) {
	offset := 0
	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return pages, records, truncated, ctxErr
		}
		remaining := bounds.maxRecords - records
		itemCount, more, fetchErr := fetchPage(ctx, offset, remaining)
		if fetchErr != nil {
			return pages, records, truncated, fetchErr
		}
		pages++
		records += itemCount
		if records >= bounds.maxRecords {
			// Enforce the record bound exactly on the output: even if a server
			// ignored the requestLimit cap and returned a full page past
			// maxRecords, the caller keeps at most maxRecords. Truncated only
			// when the provider still had more pages OR this page itself
			// overshot the cap; a page that lands exactly on maxRecords with
			// more=false read everything and is not truncated.
			overshot := records > bounds.maxRecords
			if records > bounds.maxRecords {
				records = bounds.maxRecords
			}
			return pages, records, more || overshot, nil
		}
		if !more || itemCount == 0 {
			return pages, records, truncated, nil
		}
		if pages >= bounds.maxPages {
			return pages, records, true, nil
		}
		offset += itemCount
	}
}
