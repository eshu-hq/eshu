// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"strings"
	"testing"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// TestLedgerReadGateParsesTheSwitchOnce pins the PR-3c read gate
// (ESHU_CHANGED_SINCE_LEDGER_READ_ENABLED, default off) to the same parse
// the writer switch uses, so the API, the MCP server and any later reader
// cannot disagree on a value.
func TestLedgerReadGateParsesTheSwitchOnce(t *testing.T) {
	t.Parallel()
	if linksfreshnessstore.LedgerReadEnabledEnv != "ESHU_CHANGED_SINCE_LEDGER_READ_ENABLED" {
		t.Fatalf("LedgerReadEnabledEnv = %q", linksfreshnessstore.LedgerReadEnabledEnv)
	}
	for value, want := range map[string]bool{
		"": false, "true": true, "TRUE": true, " true ": true, "1": true, "t": true, "yes": true, "y": true, "on": true,
		"false": false, "0": false, "f": false, "no": false, "n": false, "off": false, "maybe": false, "2": false,
	} {
		got := linksfreshnessstore.ReadEnabled(func(key string) string {
			if key != linksfreshnessstore.LedgerReadEnabledEnv {
				t.Fatalf("ReadEnabled read %q", key)
			}
			return value
		})
		if got != want {
			t.Fatalf("ReadEnabled(%q) = %v, want %v", value, got, want)
		}
	}
}

// TestLedgerReadFallbackReasonsAreClosed pins the bounded fallback set of
// the PR-3c read (2026-10-16 theory proof): every miss answers with one of
// these reasons, is counted, and falls back to the legacy statement with
// the wire contract unchanged.
func TestLedgerReadFallbackReasonsAreClosed(t *testing.T) {
	t.Parallel()
	got := linksfreshnessstore.ReadFallbackReasons()
	if len(got) != 7 {
		t.Fatalf("ReadFallbackReasons() = %v, want 7 reasons", got)
	}
	seen := map[linksfreshnessstore.ReadFallbackReason]bool{}
	for _, reason := range got {
		if string(reason) == "" {
			t.Fatal("fallback reason must not be empty")
		}
		if seen[reason] {
			t.Fatalf("duplicate fallback reason %q", reason)
		}
		seen[reason] = true
	}
	for _, reason := range []linksfreshnessstore.ReadFallbackReason{
		linksfreshnessstore.ReadFallbackCurrentUnlinked,
		linksfreshnessstore.ReadFallbackPriorOffChain,
		linksfreshnessstore.ReadFallbackCursorMoved,
		linksfreshnessstore.ReadFallbackDigestMismatch,
		linksfreshnessstore.ReadFallbackSameGeneration,
		linksfreshnessstore.ReadFallbackChainTooLarge,
		linksfreshnessstore.ReadFallbackLedgerError,
	} {
		if !seen[reason] {
			t.Fatalf("fallback reason %q missing from ReadFallbackReasons()", reason)
		}
	}
}

// TestLedgerReadBudgetIsBounded pins the provisional delta-row budget of the
// chain read (2026-10-16 theory proof): a chain whose stored delta rows
// exceed it falls back to the legacy statement instead of composing an
// unbounded read.
func TestLedgerReadBudgetIsBounded(t *testing.T) {
	t.Parallel()
	if linksfreshnessstore.ChainDeltaRowBudget != 200000 {
		t.Fatalf("ChainDeltaRowBudget = %d, want 200000 (provisional theory-proof budget)", linksfreshnessstore.ChainDeltaRowBudget)
	}
	if linksfreshnessstore.ChainMaxLinks < 2 {
		t.Fatalf("ChainMaxLinks = %d, want at least 2", linksfreshnessstore.ChainMaxLinks)
	}
}

// TestLedgerReadStatementsTakeNoLocks pins the reader's lock footprint
// (2026-10-16 theory proof): plain SELECTs over the ledger, no row lock,
// no advisory lock. A reader that locked would serialize against the link
// writer's per-scope fence.
func TestLedgerReadStatementsTakeNoLocks(t *testing.T) {
	t.Parallel()
	queries := map[string]string{
		"ReadCursorQuery":           linksfreshnessstore.ReadCursorQuery,
		"ReadChainQuery":            linksfreshnessstore.ReadChainQuery,
		"ReadLinkDeltasQuery":       linksfreshnessstore.ReadLinkDeltasQuery,
		"ReadUnchangedSamplesQuery": linksfreshnessstore.ReadUnchangedSamplesQuery,
		"ReadTombstoneProbeQuery":   linksfreshnessstore.ReadTombstoneProbeQuery,
	}
	for name, query := range queries {
		if query == "" {
			t.Fatalf("%s is empty", name)
		}
		upper := strings.ToUpper(query)
		for _, forbidden := range []string{"FOR UPDATE", "FOR KEY SHARE", "FOR SHARE", "FOR NO KEY UPDATE", "ADVISORY", "INSERT ", "UPDATE ", "DELETE "} {
			if strings.Contains(upper, forbidden) {
				t.Fatalf("%s contains %q: a ledger read must be a lock-free SELECT", name, forbidden)
			}
		}
	}
}

// TestLedgerReadStatementsKeyOnTheFullLinkKey pins the PR-3c addition of the
// PR-3d arbiter review: every link read names the full three-column key
// (scope_id, generation_id, prior_generation_id) and never aggregates by
// generation_id alone.
func TestLedgerReadStatementsKeyOnTheFullLinkKey(t *testing.T) {
	t.Parallel()
	for name, query := range map[string]string{
		"ReadChainQuery":            linksfreshnessstore.ReadChainQuery,
		"ReadLinkDeltasQuery":       linksfreshnessstore.ReadLinkDeltasQuery,
		"ReadUnchangedSamplesQuery": linksfreshnessstore.ReadUnchangedSamplesQuery,
	} {
		for _, column := range []string{"scope_id", "generation_id", "prior_generation_id"} {
			if !strings.Contains(query, column) {
				t.Fatalf("%s does not name %s: links are read by the full three-column key", name, column)
			}
		}
	}
}

// TestChainUnchangedSamplesQueryKeysEveryLink pins the chain builder: one
// full-key exclusion clause per link, with sequential bind numbers and no
// generation_id-alone predicate.
func TestChainUnchangedSamplesQueryKeysEveryLink(t *testing.T) {
	t.Parallel()
	for _, links := range []int{1, 2, 4} {
		query, err := linksfreshnessstore.BuildChainUnchangedSamplesQuery(links)
		if err != nil {
			t.Fatalf("BuildChainUnchangedSamplesQuery(%d): %v", links, err)
		}
		if got := strings.Count(query, "prior_generation_id"); got != links {
			t.Fatalf("BuildChainUnchangedSamplesQuery(%d) names prior_generation_id %d times, want %d", links, got, links)
		}
		upper := strings.ToUpper(query)
		for _, forbidden := range []string{"FOR UPDATE", "ADVISORY", "INSERT ", "UPDATE ", "DELETE "} {
			if strings.Contains(upper, forbidden) {
				t.Fatalf("chain unchanged-samples query contains %q", forbidden)
			}
		}
	}
	if _, err := linksfreshnessstore.BuildChainUnchangedSamplesQuery(0); err == nil {
		t.Fatal("BuildChainUnchangedSamplesQuery(0) must fail")
	}
}
