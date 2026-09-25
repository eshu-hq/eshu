// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestChangedSinceDigestNormalizationIsSharedAndScopedToContentEntities pins the
// two #7127 normalizations on the shipped statement without a database: the
// payload digest input neutralizes the collector's per-run indexed_at for
// content_entity rows only, and every fact_records scan excludes reducer-derived
// kinds with an escaped LIKE so reducerX kinds are not swallowed.
func TestChangedSinceDigestNormalizationIsSharedAndScopedToContentEntities(t *testing.T) {
	t.Parallel()

	if !strings.Contains(changedSincePayloadDigestInput, "payload - 'indexed_at'") ||
		!strings.Contains(changedSincePayloadDigestInput, "fact_kind = 'content_entity'") {
		t.Fatalf("digest input does not strip indexed_at for content_entity only: %s", changedSincePayloadDigestInput)
	}
	if got := strings.Count(changedSinceClassificationCTEs, "convert_to(("+changedSincePayloadDigestInput+")::text, 'UTF8')"); got != 4 {
		t.Fatalf("digest input used in %d payload hashes, want 4 (prior, current, prior duplicates, current duplicates)", got)
	}
	if strings.Contains(changedSinceClassificationCTEs, "convert_to(payload::text") ||
		strings.Contains(changedSinceClassificationCTEs, "convert_to(fact.payload::text") {
		t.Fatal("a payload hash bypasses the shared digest input")
	}
	if got := strings.Count(changedSinceClassificationCTEs, changedSinceExcludeReducerDerivedKinds); got != 5 {
		t.Fatalf("reducer-derived exclusion used %d times, want 5 (every fact_records scan)", got)
	}
	if got := strings.Count(changedSinceClassificationCTEs, "FROM fact_records"); got != 5 {
		t.Fatalf("classification CTE scans fact_records %d times, want 5", got)
	}
	if !strings.Contains(changedSinceExcludeReducerDerivedKinds, `NOT LIKE 'reducer\_%'`) {
		t.Fatalf("reducer exclusion must escape the underscore wildcard: %s", changedSinceExcludeReducerDerivedKinds)
	}
}
