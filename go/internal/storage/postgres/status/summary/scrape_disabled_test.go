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

// TestReadScrapeRefusesAReaderThatIsOffWithoutTouchingTheDatabase pins that
// ReadScrape checks Enabled itself, as Read does: an off or nil reader sends no
// summary statement and does not panic, so a caller that forgets the gate
// cannot run the clock and row reads while the flag is off.
func TestReadScrapeRefusesAReaderThatIsOffWithoutTouchingTheDatabase(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		reader *summary.ModelReader[string]
	}{
		{"flag off", summary.NewModelReaderWithConfig[string](summary.ReadConfig{Enabled: false, StaleAfter: time.Minute})},
		{"nil reader", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			hooks := summary.ScrapeHooks[string]{
				Select: func(context.Context) (summary.Selection, error) {
					calls++
					return summary.Selection{}, nil
				},
				Decode:  func([]summary.Entry) (string, error) { calls++; return "", nil },
				Observe: func(context.Context, summary.ScrapeObservation) { calls++ },
			}
			_, err := tc.reader.ReadScrape(context.Background(), hooks)
			if !errors.Is(err, summary.ErrScrapeReaderDisabled) {
				t.Fatalf("ReadScrape() error = %v, want ErrScrapeReaderDisabled", err)
			}
			if calls != 0 {
				t.Fatalf("ReadScrape() called a hook %d times on a reader that is off", calls)
			}
		})
	}
}
