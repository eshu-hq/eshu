// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producerstore

import (
	"strings"
	"testing"
)

// TestManifestReadsEndWithTheSharedManifestLessLeg pins that the package.json,
// composer.json, and go.mod reads share one manifest-less dirty leg (#8243):
// each statement carries ManifestLessLeg exactly once, as its tail, so the
// three reads cannot drift apart on which generations mark a scope dirty.
func TestManifestReadsEndWithTheSharedManifestLessLeg(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"package":  PackageManifestsQuery,
		"composer": ComposerManifestsQuery,
		"gomod":    GoModuleManifestsQuery,
	} {
		if got := strings.Count(query, ManifestLessLeg); got != 1 {
			t.Errorf("%s read carries the shared leg %d times, want 1", name, got)
		}
		if !strings.HasSuffix(query, ManifestLessLeg) {
			t.Errorf("%s read does not end with the shared manifest-less leg", name)
		}
		if got := strings.Count(query, "'manifest_less'"); got != 1 {
			t.Errorf("%s read has %d manifest_less legs, want 1", name, got)
		}
	}
}

// TestManifestLessLegKeepsEveryUnactivatedGenerationExceptCoveredWriters pins
// the shape of the narrowed rule: the never-activated signal stays, and the
// only generations it skips are superseded or failed ones whose write start is
// at or before the write start of the scope's newest activated full
// generation. A NULL marker compares as -infinity and a newest full without a
// marker covers nothing (the COALESCE(..., false) wrapper), so a pending,
// completed, or unknown-status row, and any row without a marked full, stays
// dirty.
func TestManifestLessLegKeepsEveryUnactivatedGenerationExceptCoveredWriters(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"    FROM scope_generations AS pending\n" +
			"    WHERE pending.scope_id = scope.scope_id\n" +
			"      AND pending.activated_at IS NULL\n" +
			"      AND NOT (\n" +
			"        pending.status IN ('superseded', 'failed')\n" +
			"        AND COALESCE(COALESCE(pending.projection_write_started_at, '-infinity'::timestamptz) <= last_full.pws, false)\n" +
			"      )\n",
	} {
		if !strings.Contains(ManifestLessLeg, want) {
			t.Errorf("ManifestLessLeg is missing:\n%s", want)
		}
	}
	if strings.Contains(ManifestLessLeg, "fact_work_items") {
		t.Error("ManifestLessLeg reads fact_work_items; the narrowed rule must not need a work-row probe")
	}
}
