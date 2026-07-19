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

// paginateOffset drives one PagerDuty classic offset-paginated resource
// fetch. fetchPage requests one page at the given offset and returns how many
// items it decoded and whether the server signaled another page is available
// (the response's "more" field); PagerDuty's classic REST v2 pagination omits
// a reliable "total" unless the caller opts in, so "more" is the only signal
// paginateOffset trusts.
//
// paginateOffset stops in exactly one of four ways:
//   - the server exhausts "more" (truncated=false: every available record was
//     read),
//   - fetchPage returns zero items while still signaling "more" (a defensive
//     terminal case for a non-advancing/malformed page; truncated=false
//     because paginateOffset cannot distinguish "provider bug" from "no more
//     data" and must not spin forever on either),
//   - the configured page or record bound is hit while more pages remain
//     (truncated=true: the caller stopped early and observed truth is
//     incomplete),
//   - the context is canceled or times out (returns ctx.Err(); checked before
//     every page request so a cancellation between pages never issues one
//     more network call).
//
// The next offset is always computed locally as the running sum of items
// already fetched, never from a server-echoed offset, so a provider that
// echoes a stale or non-advancing offset cannot cause paginateOffset to loop.
func paginateOffset(
	ctx context.Context,
	bounds paginationBounds,
	fetchPage func(ctx context.Context, offset int) (itemCount int, more bool, err error),
) (pages int, records int, truncated bool, err error) {
	offset := 0
	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return pages, records, truncated, ctxErr
		}
		itemCount, more, fetchErr := fetchPage(ctx, offset)
		if fetchErr != nil {
			return pages, records, truncated, fetchErr
		}
		pages++
		records += itemCount
		if !more || itemCount == 0 {
			return pages, records, truncated, nil
		}
		if pages >= bounds.maxPages || records >= bounds.maxRecords {
			return pages, records, true, nil
		}
		offset += itemCount
	}
}
