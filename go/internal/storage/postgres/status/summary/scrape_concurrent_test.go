// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// movingQueryer answers the summary statements from a row and a clock that a
// writer goroutine replaces while scrapers read them, and counts live runs.
type movingQueryer struct {
	row      atomic.Pointer[[]any]
	now      atomic.Pointer[time.Time]
	liveRuns atomic.Int32
}

func (q *movingQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	switch {
	case strings.Contains(query, "to_regclass('status_summary_snapshots')"):
		return &readerRows{rows: [][]any{{*q.now.Load(), true}}}, nil
	case strings.Contains(query, "FROM status_summary_snapshots"):
		if row := q.row.Load(); row != nil {
			return &readerRows{rows: [][]any{*row}}, nil
		}
		return &readerRows{}, nil
	case strings.Contains(query, "active_work_stage"):
		q.liveRuns.Add(1)
	}
	return &readerRows{}, nil
}

// TestScrapeLastRowIsSafeAndWholeUnderParallelScrapes is the concurrency proof
// of the per-process last row. One writer goroutine moves the database clock and
// replaces the stored row with fresh rows (each carrying its own id in the stage
// count and as_of), stale rows, and no row, while 16 scrapers run through the
// production store and one shared reader. It runs under -race. Every answer must
// be one whole row (the stage count is the id of the row whose as_of is reported,
// never a mix), a fresh serve is never marked stale and a stale one never fresh,
// the live statement never runs, once a scraper has seen a row it is never handed
// the zero summary again, and the as_of a scraper sees from the held row never
// goes back.
func TestScrapeLastRowIsSafeAndWholeUnderParallelScrapes(t *testing.T) {
	t.Parallel()

	sha := pgstatus.ActiveWorkSummarySourceSHA256()
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	rowFor := func(id int, asOf time.Time) []any {
		payload := encodedEntries(t,
			summary.Entry{Section: "queue", Ordinal: 1, JSON: queueEntryJSON},
			summary.Entry{Section: "stage", Ordinal: 1, JSON: fmt.Sprintf(`{"stage":"reducer","status":"pending","count":%d}`, id)},
		)
		return storedRow(asOf, summary.SchemaVersion, sha, 2, payload)
	}
	q := &movingQueryer{}
	q.now.Store(&base)
	first := rowFor(1, base.Add(time.Second))
	q.row.Store(&first)
	reader := pgstatus.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: true, StaleAfter: 33 * time.Second})
	store := pgstatus.NewStatusStore(q).WithSummaryReader(reader)

	stop := make(chan struct{})
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for step := 2; ; step++ {
			select {
			case <-stop:
				return
			default:
			}
			// The clock runs ahead of the newest row id by 1 s on fresh steps,
			// and 120 s ahead on stale steps; every third step has no row.
			now := base.Add(time.Duration(step) * time.Second)
			if step%4 == 0 {
				now = now.Add(120 * time.Second)
			}
			q.now.Store(&now)
			switch step % 3 {
			case 0:
				q.row.Store(nil)
			default:
				row := rowFor(step, base.Add(time.Duration(step)*time.Second))
				q.row.Store(&row)
			}
			time.Sleep(200 * time.Microsecond)
		}
	}()

	var scrapers sync.WaitGroup
	var models, lastRows atomic.Int64
	failures := make(chan string, 64)
	fail := func(format string, args ...any) {
		select {
		case failures <- fmt.Sprintf(format, args...):
		default:
		}
	}
	for range 16 {
		scrapers.Add(1)
		go func() {
			defer scrapers.Done()
			var seenRow bool
			var lastHeld time.Time
			for range 300 {
				snapshot, err := store.ReadStatusSnapshotFiltered(context.Background(), base, scrapeSelection())
				if err != nil {
					fail("scrape error: %v", err)
					return
				}
				got := snapshot.ActiveWorkSource
				switch got.Source {
				case statuspkg.ActiveWorkSourceModel:
					if got.Stale {
						fail("a fresh serve was marked stale: %+v", got)
					}
					seenRow = true
					models.Add(1)
				case statuspkg.ActiveWorkSourceLastRow:
					if !got.Stale {
						fail("a last-row serve was marked fresh: %+v", got)
					}
					if got.AsOf.Before(lastHeld) {
						fail("the held row went back in time: %v then %v", lastHeld, got.AsOf)
					}
					lastHeld, seenRow = got.AsOf, true
					lastRows.Add(1)
				case statuspkg.ActiveWorkSourceZero:
					if seenRow {
						fail("a scraper that had seen a row was handed the zero summary")
					}
					continue
				default:
					fail("unexpected source %+v", got)
					continue
				}
				if want := int(got.AsOf.Sub(base).Seconds()); len(snapshot.StageCounts) != 1 || snapshot.StageCounts[0].Count != want {
					fail("answer was not one whole row: as_of id %d, stages %+v", want, snapshot.StageCounts)
				}
			}
		}()
	}
	scrapers.Wait()
	close(stop)
	writer.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	if q.liveRuns.Load() != 0 {
		t.Fatalf("live statement ran %d times, want 0", q.liveRuns.Load())
	}
	if models.Load() == 0 || lastRows.Load() == 0 {
		t.Fatalf("the run served %d fresh and %d last-row answers; it must exercise both", models.Load(), lastRows.Load())
	}
}
