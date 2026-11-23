// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package completionstore

import (
	"strings"
	"testing"
)

// manifestMarkerFanoutShapeError reports how query fails the #10972 shape: the
// locked, scheduled current_consumers set excludes manifest markers, and the
// skipped_markers count reads the same predicate positively. It exists as a
// function so the seeded-violation test can feed it a broken statement.
func manifestMarkerFanoutShapeError(query string) string {
	consumers := between(query, "current_consumers AS MATERIALIZED (", "), skipped_markers AS MATERIALIZED (")
	skipped := between(query, "skipped_markers AS MATERIALIZED (", "), captured AS MATERIALIZED (")
	switch {
	case consumers == "" || skipped == "":
		return "current_consumers or skipped_markers CTE is missing"
	case !strings.Contains(consumers, "AND NOT ("+manifestMarkerPredicate+")"):
		return "current_consumers does not exclude manifest markers"
	case !strings.Contains(skipped, "AND ("+manifestMarkerPredicate+")") || strings.Contains(skipped, "FOR NO KEY UPDATE"):
		return "skipped_markers must count markers without locking them"
	case !strings.Contains(manifestMarkerPredicate, "payload->>'trigger'") ||
		!strings.Contains(manifestMarkerPredicate, "'manifest_dependency'") ||
		!strings.Contains(manifestMarkerPredicate, "source.domain = 'supply_chain_impact'"):
		return "marker predicate lost its trigger, value, or domain guard"
	}
	return ""
}

func between(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return ""
	}
	rest := s[i+len(from):]
	j := strings.Index(rest, to)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// TestFanoutQueryExcludesManifestMarkers pins that the shipped completion
// fan-out leaves manifest markers unlocked and unscheduled (#10972); the live
// twin proves the rows. The seeded violation drops the exclusion and must fail.
func TestFanoutQueryExcludesManifestMarkers(t *testing.T) {
	t.Parallel()

	if msg := manifestMarkerFanoutShapeError(FanoutCrossScopeCompletionQuery); msg != "" {
		t.Fatalf("shipped fan-out query: %s", msg)
	}
	broken := strings.Replace(FanoutCrossScopeCompletionQuery, "AND NOT ("+manifestMarkerPredicate+")", "", 1)
	if manifestMarkerFanoutShapeError(broken) == "" {
		t.Fatal("a fan-out query without the marker exclusion passed the shape check")
	}
}
