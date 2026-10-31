// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core/cvss"
)

// TestSeverityBucketFactSQLIsTheSharedBucket pins the read side of the #9693
// bucket parity: the read-time bucket is exactly cvss.BucketSQL over the fact
// payload, the same expression the winners recompute embeds over the source
// payload (pinned in go/internal/storage/postgres), so the stored and the live
// bucket cannot disagree.
func TestSeverityBucketFactSQLIsTheSharedBucket(t *testing.T) {
	t.Parallel()

	if want := cvss.BucketSQL("fact.payload"); severityBucketFactSQL != want {
		t.Fatalf("severityBucketFactSQL differs from cvss.BucketSQL(\"fact.payload\"):\n%s\n---\n%s", severityBucketFactSQL, want)
	}
}

// TestReadQueriesBucketThroughTheSharedExpression proves every read query that
// filters or groups by severity embeds the shared expression rather than a
// private copy.
func TestReadQueriesBucketThroughTheSharedExpression(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"ListFindingsQuery":            ListFindingsQuery,
		"ListFindingsFromWinnersQuery": ListFindingsFromWinnersQuery,
		"AggregateCanonicalFactsCTE":   AggregateCanonicalFactsCTE,
	} {
		if name == "ListFindingsFromWinnersQuery" {
			// The winners read filters on the stored column, not the payload.
			if !strings.Contains(query, "w.severity_bucket") {
				t.Errorf("%s does not filter on the stored severity_bucket", name)
			}
			continue
		}
		if !strings.Contains(query, severityBucketFactSQL) {
			t.Errorf("%s does not embed severityBucketFactSQL", name)
		}
	}
}
