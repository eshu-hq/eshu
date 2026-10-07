// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLastRowNeverMovesBackInTimeAndCopiesWhatItHolds(t *testing.T) {
	t.Parallel()

	var held lastRow
	if _, _, ok := held.recall(); ok {
		t.Fatal("an empty holder reported a row")
	}
	newer := time.Date(2026, 10, 7, 12, 0, 10, 0, time.UTC)
	entries := []Entry{{Section: "queue", Ordinal: 1, JSON: `{"a":1}`}}
	held.remember(entries, newer)
	entries[0].JSON = `{"mutated":true}`
	held.remember([]Entry{{Section: "queue", Ordinal: 1, JSON: `{"older":true}`}}, newer.Add(-time.Second))
	held.remember([]Entry{{Section: "queue", Ordinal: 1, JSON: `{"same":true}`}}, newer)

	got, asOf, ok := held.recall()
	if !ok || !asOf.Equal(newer) || len(got) != 1 || got[0].JSON != `{"a":1}` {
		t.Fatalf("held = %v at %v (ok=%v), want the first newer row unchanged", got, asOf, ok)
	}
	held.remember([]Entry{{Section: "queue", Ordinal: 1, JSON: `{"b":2}`}}, newer.Add(time.Second))
	if got, _, _ := held.recall(); got[0].JSON != `{"b":2}` {
		t.Fatalf("a newer row did not replace the held row: %v", got)
	}
}

// TestReadScrapeServesTheLastRowWhenAFreshRowDoesNotDecode covers the decode
// branch the production decoder reaches only on a malformed section: the
// scrape serves the last row with reason decode, never the row it could not
// decode, and never errors.
func TestReadScrapeServesTheLastRowWhenAFreshRowDoesNotDecode(t *testing.T) {
	t.Parallel()

	reader := NewModelReaderWithConfig[string](ReadConfig{Enabled: true, StaleAfter: time.Minute})
	asOf := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	now := asOf.Add(5 * time.Second)
	good := []Entry{{Section: "queue", Ordinal: 1, JSON: `{"oldest_outstanding_age_seconds":1}`}}
	bad := []Entry{{Section: "queue", Ordinal: 1, JSON: `{"oldest_outstanding_age_seconds":2}`}}
	var served Selection
	hooks := ScrapeHooks[string]{
		Select: func(context.Context) (Selection, error) { return served, nil },
		Decode: func(entries []Entry) (string, error) {
			if len(entries) == 1 && entries[0].JSON == bad[0].JSON {
				return "", errors.New("cannot decode")
			}
			return "decoded", nil
		},
		Observe: func(context.Context, ScrapeObservation) {},
	}

	served = Selection{Source: SourceModel, Reason: ReasonFresh, AsOf: asOf, Entries: good, Stored: good, Now: asOf}
	if res, err := reader.ReadScrape(context.Background(), hooks); err != nil || res.Source != SourceModel || res.Stale {
		t.Fatalf("first scrape = %+v, %v", res, err)
	}
	served = Selection{Source: SourceModel, Reason: ReasonFresh, AsOf: asOf.Add(time.Second), Entries: bad, Stored: bad, Now: now}
	res, err := reader.ReadScrape(context.Background(), hooks)
	if err != nil {
		t.Fatalf("ReadScrape() error = %v", err)
	}
	if res.Source != SourceLastRow || res.Reason != ReasonDecode || !res.Stale || !res.AsOf.Equal(asOf) || res.Age != 5*time.Second {
		t.Fatalf("scrape after an undecodable fresh row = %+v, want last_row/decode at the held as_of, 5s old", res)
	}
}
