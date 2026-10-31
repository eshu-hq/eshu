// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core/cvss"
)

// TestSupplyChainImpactWinnerSelectUsesTheSharedSeverityBucket pins the write
// side of the #9693 bucket parity. The stored severity_bucket must come from
// the same expression the read-time findings and aggregates queries use,
// cvss.BucketSQL, so a finding whose source carried only a vector (stored as a
// derived cvss_score) or only a label (stored as
// provenance.selected_severity_label) buckets the same way at write time and
// at read time. The read side is pinned in
// go/internal/query/supply/chain/impact/severity_bucket_parity_test.go against
// the same function with the fact alias.
func TestSupplyChainImpactWinnerSelectUsesTheSharedSeverityBucket(t *testing.T) {
	t.Parallel()

	want := cvss.BucketSQL("source.payload")
	if !strings.Contains(supplyChainImpactWinnerSelectSQL, want+" AS severity_bucket") {
		t.Fatalf("winner select SQL does not embed cvss.BucketSQL(\"source.payload\") as severity_bucket:\n%s", supplyChainImpactWinnerSelectSQL)
	}
	if strings.Contains(supplyChainImpactWinnerSelectSQL, "source.payload->>'cvss_score', '')::numeric, 0) > 0.0  THEN") {
		t.Fatal("winner select SQL still carries the hand-written score-only severity CASE")
	}
}
