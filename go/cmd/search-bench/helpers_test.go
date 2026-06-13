package main

import (
	"testing"
	"time"

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
