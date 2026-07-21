// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestIdentityEpochCacheMissOnFingerprintChange(t *testing.T) {
	t.Parallel()

	// A supersession where active_generation_id flips but the identity-fact
	// count and max-observed_at stay the same. The active fingerprint MUST
	// detect this and trigger a cache miss (reload).

	factRow := []any{
		"fact-1", "scope-1", "gen-1",
		"oci_registry.image_tag_observation", "stable-key-1", "1.0.0",
		"oci_registry", int64(0), "reported", "oci_registry",
		"source-key-1", "uri://example", "rec-1",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		false,
		[]byte(`{}`),
	}

	// First call: probe with fingerprint=42, loads 1 fact, caches.
	// Second call: same count and max but fingerprint=99 → must reload.
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 42),
			{rows: [][]any{factRow}},
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 42),
			// Second call: same count+max, different fingerprint
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 99),
			{rows: [][]any{factRow}},
			probeQueryRow(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 99),
		},
	}

	store := newFactStoreWithCache(db, 0)

	loaded1, err := store.ListActiveContainerImageIdentityFacts(context.Background())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(loaded1) != 1 {
		t.Fatalf("first call len = %d, want 1", len(loaded1))
	}

	// Second call: fingerprint changed → must reload (not false hit).
	loaded2, err := store.ListActiveContainerImageIdentityFacts(context.Background())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(loaded2) != 1 {
		t.Fatalf("second call len = %d, want 1 (reloaded after fingerprint change)", len(loaded2))
	}

	var loadQueries int
	for _, q := range db.queries {
		if strings.Contains(q.query, "LIMIT") {
			loadQueries++
		}
	}
	if loadQueries != 2 {
		t.Fatalf("load page queries = %d, want 2 (fingerprint change → reload)", loadQueries)
	}
}

// extractIdentityFilter extracts the parenthesized 6-arm filter from a SQL query.
func extractIdentityFilter(query string) string {
	whereIdx := strings.Index(query, "WHERE")
	if whereIdx < 0 {
		panic(fmt.Sprintf("cannot find WHERE in query: %s", query))
	}
	rest := query[whereIdx+5:]
	parenStart := strings.Index(rest, "(")
	if parenStart < 0 {
		panic(fmt.Sprintf("cannot find opening paren after WHERE in query: %s", query))
	}
	start := whereIdx + 5 + parenStart

	depth := 0
	inSingle := false
	end := -1
	for i := start; i < len(query); i++ {
		ch := query[i]
		if inSingle {
			if ch == '\'' {
				inSingle = false
			}
			continue
		}
		if ch == '\'' {
			inSingle = true
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = i + 1
				goto done
			}
		}
	}
done:
	if end < 0 {
		panic(fmt.Sprintf("cannot find filter end in query: %s", query))
	}
	return query[start:end]
}
