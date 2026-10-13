// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// unscopedInstruments are the six unscoped-search instruments that must carry
// the table label so an operator can split them per route (#7009, ruling 5).
var unscopedInstruments = []string{
	"eshu_dp_content_search_unscoped_total",
	"eshu_dp_content_search_unscoped_duration_seconds",
	"eshu_dp_content_search_unscoped_overrun_seconds",
	"eshu_dp_content_search_tail_cancel_total",
	"eshu_dp_content_search_step_cancel_total",
	"eshu_dp_content_search_no_progress_total",
}

// Each route labels every one of the six instruments with its table, and the
// span carries search.want (offset + limit + 1). Two calls per route make all
// six fire: a partial whose tail the server cancelled, then a stalled probe
// that advances nothing.
func TestUnscopedSignalsCarryTheTableLabel(t *testing.T) {
	t.Parallel()

	routes := []struct {
		table            string
		partial, stalled func() *fakeCorpus
		search           func(*signals, int, int) error
	}{
		{
			table: "content_files", partial: partialCorpus, stalled: stalledCorpus,
			search: func(s *signals, limit, offset int) error {
				_, err := s.searcher.Search(context.Background(), "needle", limit, offset, querycontract.SearchCursor{})
				return err
			},
		},
		{
			table:   "content_entities",
			partial: func() *fakeCorpus { return sparseEntityPartialCorpus(120 * ms) }, stalled: stalledEntityCorpus,
			search: func(s *signals, limit, offset int) error {
				_, err := s.searcher.SearchEntities(context.Background(), "needle", limit, offset, querycontract.SearchCursor{})
				return err
			},
		},
	}
	for _, route := range routes {
		t.Run(route.table, func(t *testing.T) {
			t.Parallel()

			s := newSignals(t, route.partial())
			if err := route.search(s, 200, 7); err != nil {
				t.Fatalf("partial call error = %v", err)
			}
			attrs := spanAttrs(t, s.spans)
			if got := attrs["search.want"].AsInt64(); got != 208 {
				t.Errorf("search.want = %d, want 208 (offset 7 + limit 200 + 1)", got)
			}
			if got := attrs["db.sql.table"].AsString(); got != route.table {
				t.Errorf("db.sql.table = %q, want %q", got, route.table)
			}
			s.searcher.Store = newFakeDB(route.stalled(), s.clock)
			if err := route.search(s, 50, 0); err != nil {
				t.Fatalf("stalled call error = %v", err)
			}

			var collected metricdata.ResourceMetrics
			if err := s.reader.Collect(context.Background(), &collected); err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			seen := map[string]int{}
			for _, scope := range collected.ScopeMetrics {
				for _, m := range scope.Metrics {
					switch data := m.Data.(type) {
					case metricdata.Sum[int64]:
						for _, dp := range data.DataPoints {
							checkTable(t, m.Name, dp.Attributes.Value, route.table)
							seen[m.Name]++
						}
					case metricdata.Histogram[float64]:
						for _, dp := range data.DataPoints {
							checkTable(t, m.Name, dp.Attributes.Value, route.table)
							seen[m.Name]++
						}
					}
				}
			}
			for _, name := range unscopedInstruments {
				if seen[name] == 0 {
					t.Errorf("%s recorded no data point", name)
				}
			}
		})
	}
}

// checkTable fails the test when a data point's table label is not want.
func checkTable(t *testing.T, name string, value func(attribute.Key) (attribute.Value, bool), want string) {
	t.Helper()
	got, ok := value("table")
	if !ok || got.AsString() != want {
		t.Errorf("%s data point table = %q (present %v), want %q", name, got.AsString(), ok, want)
	}
}
