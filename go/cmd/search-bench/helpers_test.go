// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func TestPercentileNearestRank(t *testing.T) {
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	durations := []time.Duration{ms(50), ms(10), ms(40), ms(20), ms(30)}
	if got := percentile(durations, 50); got != ms(30) {
		t.Errorf("p50 = %v, want 30ms", got)
	}
	if got := percentile(durations, 95); got != ms(50) {
		t.Errorf("p95 = %v, want 50ms", got)
	}
	if got := percentile(durations, 100); got != ms(50) {
		t.Errorf("p100 = %v, want 50ms", got)
	}
	if got := percentile(nil, 50); got != 0 {
		t.Errorf("empty = %v, want 0", got)
	}
}

func TestDeriveQueriesRanksFrequentTokens(t *testing.T) {
	docs := []searchdocs.Document{
		{Title: "payment processor charge"},
		{Title: "payment service refund"},
		{Title: "payment gateway"},
		{Title: "auth handler login"},
	}
	queries := deriveQueries(docs, 3)
	if len(queries) != 3 {
		t.Fatalf("queries = %d, want 3", len(queries))
	}
	// "payment" appears three times as a standalone token and must rank first.
	// Tokens are split on non-alphanumeric (not camelCase) and filtered to >=4 chars.
	if queries[0] != "payment" {
		t.Errorf("top query = %q, want payment", queries[0])
	}
}

func TestSplitTokensLowercasesAndSplits(t *testing.T) {
	got := splitTokens("Payment_Processor.charge(amount)")
	want := []string{"payment", "processor", "charge", "amount"}
	if len(got) != len(want) {
		t.Fatalf("tokens = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseCorpusCapsSupportsAllAndDeduplicates(t *testing.T) {
	got, err := parseCorpusCaps("500, 1000, all, 500", 1250)
	if err != nil {
		t.Fatalf("parseCorpusCaps() error = %v, want nil", err)
	}
	want := []int{500, 1000, 1250}
	if len(got) != len(want) {
		t.Fatalf("caps = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("caps[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestParseCorpusCapsRejectsInvalidCaps(t *testing.T) {
	for _, raw := range []string{"", "0", "-1", "abc"} {
		if _, err := parseCorpusCaps(raw, 100); err == nil {
			t.Fatalf("parseCorpusCaps(%q) error = nil, want non-nil", raw)
		}
	}
}

func TestLoadQuerySuiteAcceptsProofWrappedSuite(t *testing.T) {
	suite := validSearchBenchQuerySuite()
	payload, err := json.Marshal(struct {
		Version string                 `json:"version"`
		Suite   searchbench.QuerySuite `json:"suite"`
	}{
		Version: searchbench.RetrievalProofVersion,
		Suite:   suite,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	path := t.TempDir() + "/suite.json"
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	got, err := loadQuerySuite(path)
	if err != nil {
		t.Fatalf("loadQuerySuite() error = %v, want nil", err)
	}
	if got.Version != searchbench.QuerySuiteVersion {
		t.Fatalf("suite version = %q, want %q", got.Version, searchbench.QuerySuiteVersion)
	}
	if len(got.Queries) != searchbench.MinimumQuerySuiteSize {
		t.Fatalf("queries = %d, want %d", len(got.Queries), searchbench.MinimumQuerySuiteSize)
	}
}

func validSearchBenchQuerySuite() searchbench.QuerySuite {
	queries := make([]searchbench.Query, 0, searchbench.MinimumQuerySuiteSize)
	for i := 1; i <= searchbench.MinimumQuerySuiteSize; i++ {
		id := fmt.Sprintf("%02d", i)
		queries = append(queries, searchbench.Query{
			ID:              "q-" + id,
			Text:            "checkout service " + id,
			RepoID:          "repo:checkout",
			Mode:            searchbench.ModeHybrid,
			Limit:           10,
			ExpectedHandles: []string{"service:checkout-" + id},
		})
	}
	return searchbench.QuerySuite{
		Version: searchbench.QuerySuiteVersion,
		Queries: queries,
	}
}
