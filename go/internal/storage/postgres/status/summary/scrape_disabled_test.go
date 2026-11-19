// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// TestReadScrapeRefusesANilReaderWithoutTouchingTheDatabase pins that
// ReadScrape refuses a nil reader: with no config and no last-row memory it
// sends no summary statement and does not panic, so a store built without a
// summary reader fails the scrape read instead of running the live statement.
func TestReadScrapeRefusesANilReaderWithoutTouchingTheDatabase(t *testing.T) {
	t.Parallel()
	var reader *summary.ModelReader[string]
	calls := 0
	hooks := summary.ScrapeHooks[string]{
		Select: func(context.Context) (summary.Selection, error) {
			calls++
			return summary.Selection{}, nil
		},
		Decode:  func([]summary.Entry) (string, error) { calls++; return "", nil },
		Observe: func(context.Context, summary.ScrapeObservation) { calls++ },
	}
	_, err := reader.ReadScrape(context.Background(), hooks)
	if !errors.Is(err, summary.ErrScrapeReaderDisabled) {
		t.Fatalf("ReadScrape() error = %v, want ErrScrapeReaderDisabled", err)
	}
	if calls != 0 {
		t.Fatalf("ReadScrape() called a hook %d times with a nil reader", calls)
	}
}

// TestReadScrapeWithTheReaderOffServesTheStoredRow pins the #10872 contract:
// the scrape path never runs the live statement (there is no live hook), so
// a present-but-off reader still serves its stored row under the scrape
// fences instead of refusing. The Enabled gate only decides whether status
// routes may use the stored row; the scrape selection opts into stored-only.
func TestReadScrapeWithTheReaderOffServesTheStoredRow(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	reader := summary.NewModelReaderWithConfig[string](summary.ReadConfig{Enabled: false, StaleAfter: time.Minute})
	selected := 0
	hooks := summary.ScrapeHooks[string]{
		Select: func(context.Context) (summary.Selection, error) {
			selected++
			return summary.Selection{
				Source: summary.SourceModel, Reason: summary.ReasonFresh,
				AsOf: now.Add(-time.Second), Age: time.Second,
			}, nil
		},
		Decode:  func([]summary.Entry) (string, error) { return "served", nil },
		Observe: func(context.Context, summary.ScrapeObservation) {},
	}
	res, err := reader.ReadScrape(context.Background(), hooks)
	if err != nil {
		t.Fatalf("ReadScrape() with the reader off error = %v, want the stored serve", err)
	}
	if res.Source != summary.SourceModel || res.Reason != summary.ReasonFresh || res.Value != "served" || res.Stale {
		t.Fatalf("ReadScrape() with the reader off = %+v, want model/fresh, not stale", res)
	}
	if selected != 1 {
		t.Fatalf("ReadScrape() ran Select %d times, want 1", selected)
	}
}
