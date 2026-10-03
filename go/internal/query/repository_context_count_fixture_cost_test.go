// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"testing"
	"time"
)

// TestRepositoryContextReadModelFixtureCost measures the finished ContentReader
// methods against the same FIFO SQL fixture. It is a call-path check, not a
// server or endpoint latency benchmark: this driver does no PostgreSQL work.
func TestRepositoryContextReadModelFixtureCost(t *testing.T) {
	const samples = 100
	fullResults := make([]contentReaderQueryResult, 0, samples*4)
	countResults := make([]contentReaderQueryResult, 0, samples*3)
	for range samples {
		scope := contentReaderQueryResult{
			columns: []string{"scope_id"}, rows: [][]driver.Value{{"scope-one"}},
			queryContains: []string{"FROM ingestion_scopes"}, wantArgs: []driver.Value{"repo-one"},
		}
		names := contentReaderQueryResult{
			columns: []string{"name"}, rows: [][]driver.Value{{"service-one"}},
			queryContains: []string{"reducer_workload_identity"}, wantArgs: []driver.Value{"scope-one"},
		}
		platforms := contentReaderQueryResult{
			columns: []string{"count"}, rows: [][]driver.Value{{int64(11)}},
			queryContains: []string{"reducer_platform_materialization"}, wantArgs: []driver.Value{"scope-one"},
		}
		dependencies := contentReaderQueryResult{
			columns: []string{"count"}, rows: [][]driver.Value{{int64(0)}},
			queryContains: []string{"FROM resolved_relationships"}, wantArgs: []driver.Value{"repo-one"},
		}
		fullResults = append(fullResults, scope, names, platforms, dependencies)
		countResults = append(countResults, scope, platforms, dependencies)
	}
	full := NewContentReader(openContentReaderTestDB(t, fullResults))
	counts := NewContentReader(openContentReaderTestDB(t, countResults))
	var fullElapsed, countElapsed time.Duration
	readFull := func() {
		start := time.Now()
		got, err := full.RepositoryReadModelSummary(t.Context(), "repo-one")
		fullElapsed += time.Since(start)
		if err != nil || !got.Available || len(got.WorkloadNames) != 1 || got.WorkloadNames[0] != "service-one" || got.PlatformCount != 11 || got.DependencyCount != 0 {
			t.Fatalf("full summary = %+v, error = %v", got, err)
		}
	}
	readCounts := func() {
		start := time.Now()
		got, err := counts.RepositoryReadModelCounts(t.Context(), "repo-one")
		countElapsed += time.Since(start)
		if err != nil || !got.Available || got.PlatformCount != 11 || got.DependencyCount != 0 {
			t.Fatalf("count summary = %+v, error = %v", got, err)
		}
	}
	for i := range samples {
		if i%2 == 0 {
			readFull()
			readCounts()
		} else {
			readCounts()
			readFull()
		}
	}
	t.Logf("FIFO fixture: %d full summaries (%d SQL calls) in %s; %d count summaries (%d SQL calls) in %s; no server timing",
		samples, len(fullResults), fullElapsed, samples, len(countResults), countElapsed)
}
