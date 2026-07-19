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
		func(_ context.Context, offset int) (int, bool, error) {
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
		func(_ context.Context, _ int) (int, bool, error) {
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
		func(_ context.Context, _ int) (int, bool, error) {
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

	bounds := paginationBounds{maxPages: 100, maxRecords: 5}
	calls := 0
	_, gotRecords, truncated, err := paginateOffset(context.Background(), bounds,
		func(_ context.Context, _ int) (int, bool, error) {
			calls++
			return 3, true, nil
		})
	if err != nil {
		t.Fatalf("paginateOffset() error = %v, want nil", err)
	}
	if !truncated {
		t.Fatal("truncated = false, want true when max-record bound is hit")
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want exactly 2 (3+3=6 >= 5)", calls)
	}
	if gotRecords != 6 {
		t.Fatalf("records = %d, want 6", gotRecords)
	}
}

func TestPaginateOffsetTerminatesOnEmptyPageDespiteMoreTrue(t *testing.T) {
	t.Parallel()

	bounds := paginationBounds{maxPages: 100, maxRecords: 1000}
	calls := 0
	gotPages, gotRecords, truncated, err := paginateOffset(context.Background(), bounds,
		func(_ context.Context, _ int) (int, bool, error) {
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
		func(_ context.Context, _ int) (int, bool, error) {
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
		func(_ context.Context, _ int) (int, bool, error) {
			return 0, false, wantErr
		})
	if !errors.Is(err, wantErr) {
		t.Fatalf("paginateOffset() error = %v, want %v", err, wantErr)
	}
}
