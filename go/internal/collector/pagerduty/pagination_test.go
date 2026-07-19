// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"context"
	"errors"
	"testing"
)

func TestPaginateOffsetFetchesAllPagesUntilMoreIsFalse(t *testing.T) {
	t.Parallel()

	bounds := paginationBounds{maxPages: 10, maxRecords: 1000}
	var requestedOffsets []int
	pages := [][]int{{1, 2}, {3, 4}, {5}}
	pageIndex := 0
	total := 0
	gotPages, gotRecords, truncated, err := paginateOffset(context.Background(), bounds,
		func(_ context.Context, offset int, _ int) (int, bool, error) {
			requestedOffsets = append(requestedOffsets, offset)
			page := pages[pageIndex]
			pageIndex++
			total += len(page)
			return len(page), pageIndex < len(pages), nil
		})
	if err != nil {
		t.Fatalf("paginateOffset() error = %v, want nil", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false when more naturally exhausts")
	}
	if gotPages != 3 {
		t.Fatalf("pages = %d, want 3", gotPages)
	}
	if gotRecords != 5 {
		t.Fatalf("records = %d, want 5", gotRecords)
	}
	wantOffsets := []int{0, 2, 4}
	if len(requestedOffsets) != len(wantOffsets) {
		t.Fatalf("requestedOffsets = %v, want %v", requestedOffsets, wantOffsets)
	}
	for i, want := range wantOffsets {
		if requestedOffsets[i] != want {
			t.Fatalf("requestedOffsets[%d] = %d, want %d", i, requestedOffsets[i], want)
		}
	}
}

func TestPaginateOffsetStopsWhenSinglePageHasNoMore(t *testing.T) {
	t.Parallel()

	bounds := paginationBounds{maxPages: 10, maxRecords: 1000}
	calls := 0
	gotPages, gotRecords, truncated, err := paginateOffset(context.Background(), bounds,
		func(_ context.Context, _ int, _ int) (int, bool, error) {
			calls++
			return 3, false, nil
		})
	if err != nil {
		t.Fatalf("paginateOffset() error = %v, want nil", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false for single exhausted page")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if gotPages != 1 || gotRecords != 3 {
		t.Fatalf("pages=%d records=%d, want pages=1 records=3", gotPages, gotRecords)
	}
}

func TestPaginateOffsetStopsWhenMaxPagesBoundHit(t *testing.T) {
	t.Parallel()

	bounds := paginationBounds{maxPages: 2, maxRecords: 1000}
	calls := 0
	gotPages, _, truncated, err := paginateOffset(context.Background(), bounds,
		func(_ context.Context, _ int, _ int) (int, bool, error) {
			calls++
			return 1, true, nil
		})
	if err != nil {
		t.Fatalf("paginateOffset() error = %v, want nil", err)
	}
	if !truncated {
		t.Fatal("truncated = false, want true when max-page bound is hit")
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want exactly 2 (bounded)", calls)
	}
	if gotPages != 2 {
		t.Fatalf("pages = %d, want 2", gotPages)
	}
}

func TestPaginateOffsetStopsWhenMaxRecordsBoundHit(t *testing.T) {
	t.Parallel()

	// A fetchPage that IGNORES the requestLimit hint and always returns a full
	// 3-item page proves the defensive output trim: records must be capped to
	// exactly maxRecords=5, never the 6 actually appended.
	bounds := paginationBounds{maxPages: 100, maxRecords: 5}
	calls := 0
	var lastRequestLimit int
	_, gotRecords, truncated, err := paginateOffset(context.Background(), bounds,
		func(_ context.Context, _ int, requestLimit int) (int, bool, error) {
			calls++
			lastRequestLimit = requestLimit
			return 3, true, nil
		})
	if err != nil {
		t.Fatalf("paginateOffset() error = %v, want nil", err)
	}
	if !truncated {
		t.Fatal("truncated = false, want true when max-record bound is hit")
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want exactly 2 (3 fetched, then 2 remaining requested)", calls)
	}
	if gotRecords != 5 {
		t.Fatalf("records = %d, want 5 (output trimmed to maxRecords, not the 6 appended)", gotRecords)
	}
	if lastRequestLimit != 2 {
		t.Fatalf("last requestLimit = %d, want 2 (remaining allowance = maxRecords - already-fetched)", lastRequestLimit)
	}
}

func TestPaginateOffsetEnforcesMaxRecordsExactlyOnNonMultiplePageSize(t *testing.T) {
	t.Parallel()

	// The reported P2-1 bug: maxRecords=150 with a 100-item page size is not an
	// exact multiple, so the pre-fix loop accepted a whole second page (200
	// kept for a 150 cap). With the fix the final page requests only the
	// remaining 50 (honoring requestLimit) and the output is exactly 150.
	bounds := paginationBounds{maxPages: 100, maxRecords: 150}
	var requestedLimits []int
	gotPages, gotRecords, truncated, err := paginateOffset(context.Background(), bounds,
		func(_ context.Context, _ int, requestLimit int) (int, bool, error) {
			requestedLimits = append(requestedLimits, requestLimit)
			// Simulate a well-behaved server: honor the cap, page size 100.
			served := 100
			if requestLimit > 0 && requestLimit < served {
				served = requestLimit
			}
			return served, true, nil
		})
	if err != nil {
		t.Fatalf("paginateOffset() error = %v, want nil", err)
	}
	if gotRecords != 150 {
		t.Fatalf("records = %d, want exactly 150 (non-multiple cap enforced on output)", gotRecords)
	}
	if !truncated {
		t.Fatal("truncated = false, want true: more pages remained past the record cap")
	}
	if gotPages != 2 {
		t.Fatalf("pages = %d, want 2 (100 then a capped 50)", gotPages)
	}
	wantLimits := []int{150, 50}
	if len(requestedLimits) != len(wantLimits) {
		t.Fatalf("requestedLimits = %v, want %v", requestedLimits, wantLimits)
	}
	for i, want := range wantLimits {
		if requestedLimits[i] != want {
			t.Fatalf("requestedLimits[%d] = %d, want %d (final page must not over-fetch)", i, requestedLimits[i], want)
		}
	}
}

func TestPaginateOffsetExactMultiplePageSizeKeepsAllAndTruncates(t *testing.T) {
	t.Parallel()

	// The exact-multiple companion case: maxRecords=200, page size 100 → two
	// full pages, exactly 200 kept, and truncated=true because the provider
	// still reported more pages.
	bounds := paginationBounds{maxPages: 100, maxRecords: 200}
	gotPages, gotRecords, truncated, err := paginateOffset(context.Background(), bounds,
		func(_ context.Context, _ int, requestLimit int) (int, bool, error) {
			served := 100
			if requestLimit > 0 && requestLimit < served {
				served = requestLimit
			}
			return served, true, nil
		})
	if err != nil {
		t.Fatalf("paginateOffset() error = %v, want nil", err)
	}
	if gotRecords != 200 {
		t.Fatalf("records = %d, want exactly 200", gotRecords)
	}
	if !truncated {
		t.Fatal("truncated = false, want true: more pages remained past the exact-multiple cap")
	}
	if gotPages != 2 {
		t.Fatalf("pages = %d, want 2", gotPages)
	}
}

func TestPaginateOffsetExactCapWithNoMoreIsNotTruncated(t *testing.T) {
	t.Parallel()

	// Landing exactly on maxRecords with more=false read everything available,
	// so it is not a truncation.
	bounds := paginationBounds{maxPages: 100, maxRecords: 100}
	_, gotRecords, truncated, err := paginateOffset(context.Background(), bounds,
		func(_ context.Context, _ int, _ int) (int, bool, error) {
			return 100, false, nil
		})
	if err != nil {
		t.Fatalf("paginateOffset() error = %v, want nil", err)
	}
	if gotRecords != 100 {
		t.Fatalf("records = %d, want 100", gotRecords)
	}
	if truncated {
		t.Fatal("truncated = true, want false: exact cap reached but the provider had no more pages")
	}
}

func TestPaginationRequestLimitCapsFinalPageWithoutForcingLargePages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configured int
		remaining  int
		want       int
	}{
		{name: "configured limit under remaining stays", configured: 50, remaining: 1000, want: 50},
		{name: "remaining shrinks configured limit", configured: 100, remaining: 40, want: 40},
		{name: "no configured limit, remaining large omits param", configured: 0, remaining: 1000, want: 0},
		{name: "no configured limit, remaining below page ceiling caps", configured: 0, remaining: 50, want: 50},
		{name: "no configured limit, remaining exactly page ceiling omits param", configured: 0, remaining: 100, want: 0},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := paginationRequestLimit(tt.configured, tt.remaining); got != tt.want {
				t.Fatalf("paginationRequestLimit(%d, %d) = %d, want %d", tt.configured, tt.remaining, got, tt.want)
			}
		})
	}
}

func TestPaginateOffsetTerminatesOnEmptyPageDespiteMoreTrue(t *testing.T) {
	t.Parallel()

	bounds := paginationBounds{maxPages: 100, maxRecords: 1000}
	calls := 0
	gotPages, gotRecords, truncated, err := paginateOffset(context.Background(), bounds,
		func(_ context.Context, _ int, _ int) (int, bool, error) {
			calls++
			return 0, true, nil
		})
	if err != nil {
		t.Fatalf("paginateOffset() error = %v, want nil", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false: a non-advancing empty page is terminal, not a bound hit")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want exactly 1 (no infinite loop on a non-advancing page)", calls)
	}
	if gotPages != 1 || gotRecords != 0 {
		t.Fatalf("pages=%d records=%d, want pages=1 records=0", gotPages, gotRecords)
	}
}

func TestPaginateOffsetStopsOnContextCancellationBeforeNextPage(t *testing.T) {
	t.Parallel()

	bounds := paginationBounds{maxPages: 100, maxRecords: 1000}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	_, _, _, err := paginateOffset(ctx, bounds,
		func(_ context.Context, _ int, _ int) (int, bool, error) {
			calls++
			if calls == 1 {
				cancel()
			}
			return 1, true, nil
		})
	if err == nil {
		t.Fatal("paginateOffset() error = nil, want context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("paginateOffset() error = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want exactly 1 (loop must stop before issuing the next page request)", calls)
	}
}

func TestPaginateOffsetPropagatesFetchError(t *testing.T) {
	t.Parallel()

	bounds := paginationBounds{maxPages: 10, maxRecords: 1000}
	wantErr := errors.New("boom")
	_, _, _, err := paginateOffset(context.Background(), bounds,
		func(_ context.Context, _ int, _ int) (int, bool, error) {
			return 0, false, wantErr
		})
	if !errors.Is(err, wantErr) {
		t.Fatalf("paginateOffset() error = %v, want %v", err, wantErr)
	}
}
