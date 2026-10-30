// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedule

import (
	"reflect"
	"testing"
)

// TestRecordDerivedTargetSkipCarriesPackageIdentities proves the #9612
// per-package contract at the shared renderer: recorded identities land on
// their (reason, ecosystem) row sorted and deduped, rows without identities
// keep a count but no package list, and blank entries never record.
func TestRecordDerivedTargetSkipCarriesPackageIdentities(t *testing.T) {
	t.Parallel()

	counts := map[string]map[string]int{}
	packages := map[string][]DerivedTargetSkippedPackage{}
	RecordDerivedTargetSkip(counts, packages, DerivedTargetSkipReasonNonExactVersion, "npm",
		[]DerivedTargetSkippedPackage{{Ecosystem: "npm", PackageName: "ws"}, {Ecosystem: "npm", PackageName: "left-pad"}})
	RecordDerivedTargetSkip(counts, packages, DerivedTargetSkipReasonNonExactVersion, "npm",
		[]DerivedTargetSkippedPackage{{Ecosystem: "npm", PackageName: "ws"}})
	RecordDerivedTargetSkip(counts, packages, DerivedTargetSkipReasonMissingVersion, "npm", nil)
	RecordDerivedTargetSkip(counts, packages, "", "npm",
		[]DerivedTargetSkippedPackage{{Ecosystem: "npm", PackageName: "dropped"}})
	RecordDerivedTargetSkip(counts, packages, DerivedTargetSkipReasonNonExactVersion, "npm",
		[]DerivedTargetSkippedPackage{{Ecosystem: "", PackageName: ""}})

	evidence := DerivedTargetSkipEvidenceByReason("vulnerability_intelligence", 100, 2, counts, []string{"osv"}, packages)
	if len(evidence) != 2 {
		t.Fatalf("len(evidence) = %d, want 2 rows: %#v", len(evidence), evidence)
	}
	for _, row := range evidence {
		switch row.Reason {
		case DerivedTargetSkipReasonNonExactVersion:
			if row.SkippedCount != 3 {
				t.Fatalf("non-exact skipped count = %d, want 3", row.SkippedCount)
			}
			want := []DerivedTargetSkippedPackage{
				{Ecosystem: "npm", PackageName: "left-pad"},
				{Ecosystem: "npm", PackageName: "ws"},
			}
			if !reflect.DeepEqual(row.Packages, want) {
				t.Fatalf("non-exact packages = %#v, want sorted deduped %#v", row.Packages, want)
			}
		case DerivedTargetSkipReasonMissingVersion:
			if row.SkippedCount != 1 {
				t.Fatalf("missing-version skipped count = %d, want 1", row.SkippedCount)
			}
			if row.Packages != nil {
				t.Fatalf("missing-version packages = %#v, want nil for a count-only row", row.Packages)
			}
		default:
			t.Fatalf("unexpected skip row reason %q", row.Reason)
		}
	}
}
