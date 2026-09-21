// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestExpectedRelationalCountsMatchThePlanAndTheIaCFacts(t *testing.T) {
	plan := BuildSeedPlan(SeedPlanOptions{TotalScopes: 800})
	facts := BuildIaCFacts("s", "g", 30)

	got := expectedRelationalCounts(plan, facts)

	// Each of the first three tables carries the one standing refresh seed
	// row migration 115 leaves on a freshly migrated database.
	want := map[string]int{
		"ingestion_scopes":  800 + standingRefreshSeedRows,
		"scope_generations": countGenerations(plan) + standingRefreshSeedRows,
		"fact_work_items":   countWorkItems(plan) + standingRefreshSeedRows,
		"fact_records":      30,
	}
	for table, n := range want {
		if got[table] != n {
			t.Errorf("expected %s = %d, want %d", table, got[table], n)
		}
	}
}

// fixedCounter serves table row counts for verifyRelationalCounts without a
// database.
func fixedCounter(counts map[string]int) rowCounter {
	return func(_ context.Context, table string) (int, error) { return counts[table], nil }
}

// TestVerifyRelationalCountsFailsAShortSeedRED and its GREEN twin are the seeded
// pair for the read-back: a seed that silently writes
// fewer rows than planned (an insert dropped, half the scopes, IaC facts that
// never landed) would otherwise leave the work metric to read a shrunken corpus
// as GREEN, because work only ever fails on MORE reads.
func TestVerifyRelationalCountsFailsAShortSeedRED(t *testing.T) {
	expected := map[string]int{"ingestion_scopes": 800, "fact_records": 150000, "fact_work_items": 67200}
	short := fixedCounter(map[string]int{"ingestion_scopes": 400, "fact_records": 0, "fact_work_items": 67200})

	err := verifyRelationalCounts(context.Background(), short, expected)

	if err == nil {
		t.Fatal("a seed with half the scopes and no IaC facts passed verification")
	}
	for _, want := range []string{"ingestion_scopes: got 400, want 800", "fact_records: got 0, want 150000"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "fact_work_items") {
		t.Errorf("error names a table that matched: %v", err)
	}
}

func TestVerifyRelationalCountsAcceptsACleanSeedGREEN(t *testing.T) {
	expected := map[string]int{"ingestion_scopes": 800, "fact_records": 150000}
	clean := fixedCounter(map[string]int{"ingestion_scopes": 800, "fact_records": 150000})

	if err := verifyRelationalCounts(context.Background(), clean, expected); err != nil {
		t.Fatalf("a clean seed failed verification: %v", err)
	}
}

// TestARunWhoseSeedFailsSendsNoRequests pins that a seed problem stops the run
// before any route is swept: run() returns the seed error before it waits on the
// read model, opens the meter, or issues a single request to eshu-api.
func TestARunWhoseSeedFailsSendsNoRequests(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
	}))
	defer srv.Close()

	err := run(runOptions{
		postgresDSN:    "postgresql://nobody:x@127.0.0.1:1/none?connect_timeout=1",
		apiBaseURL:     srv.URL,
		apiKey:         "k",
		totalScopes:    4,
		nodesPerLabel:  1,
		iacFactCount:   3,
		iterations:     2,
		requestTimeout: time.Second,
		backgroundIdle: time.Millisecond,
	})

	if err == nil {
		t.Fatal("run succeeded against an unreachable database")
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("eshu-api received %d requests although the seed failed", got)
	}
}
