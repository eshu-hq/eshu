// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestSortDedupWinnersDirtyIdentifiersOrdersByTheConflictKey pins the Go half
// of the #10827 writer lock order: identifiers reach the capture upsert
// sorted by the fact conflict key with exact duplicates removed, so
// concurrent multi-row writers take row locks in one total order and one
// statement never marks one unit twice.
func TestSortDedupWinnersDirtyIdentifiersOrdersByTheConflictKey(t *testing.T) {
	t.Parallel()

	b := supplychaincore.WinnersDirtyIdentifier{PackageID: "b", RepositoryID: "r", SubjectDigest: "d", C0: "c", AdvisoryID: "a"}
	a := supplychaincore.WinnersDirtyIdentifier{PackageID: "a", RepositoryID: "r", SubjectDigest: "d", C0: "c", AdvisoryID: "a"}
	a2 := supplychaincore.WinnersDirtyIdentifier{PackageID: "a", RepositoryID: "r", SubjectDigest: "d", C0: "c", AdvisoryID: "b"}
	in := []supplychaincore.WinnersDirtyIdentifier{b, a, b, a2, a}
	got := sortDedupWinnersDirtyIdentifiers(in)
	want := []supplychaincore.WinnersDirtyIdentifier{a, a2, b}
	if len(got) != len(want) {
		t.Fatalf("sortDedupWinnersDirtyIdentifiers() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortDedupWinnersDirtyIdentifiers()[%d] = %+v, want %+v (full result %+v)", i, got[i], want[i], got)
		}
	}
	// The input keeps its order: the helper copies before sorting.
	if in[0] != b || in[1] != a {
		t.Fatalf("sortDedupWinnersDirtyIdentifiers() mutated its input: %+v", in)
	}
	if out := sortDedupWinnersDirtyIdentifiers(nil); out != nil {
		t.Fatalf("sortDedupWinnersDirtyIdentifiers(nil) = %+v, want nil", out)
	}
}

// TestCaptureWinnersDirtyFactsSQLIsKeyOrderedVersionedUpsert pins the #10827
// capture shape: rows reach the log in conflict-key order, a re-mark of a
// queued key bumps only dirty_version, and the first mark's captured_at is
// never overwritten.
func TestCaptureWinnersDirtyFactsSQLIsKeyOrderedVersionedUpsert(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"INSERT INTO supply_chain_impact_winners_dirty_log",
		"'fact_upsert'",
		"ORDER BY package_id, repository_id, subject_digest, c0, advisory_id",
		"ON CONFLICT (package_id, repository_id, subject_digest, c0, advisory_id) WHERE kind = 'fact'",
		"DO UPDATE SET dirty_version = supply_chain_impact_winners_dirty_log.dirty_version + 1",
	} {
		if !strings.Contains(captureWinnersDirtyFactsSQL, want) {
			t.Errorf("capture statement does not contain %q:\n%s", want, captureWinnersDirtyFactsSQL)
		}
	}
	if strings.Contains(captureWinnersDirtyFactsSQL, "captured_at") {
		t.Errorf("capture statement writes captured_at, losing the first mark:\n%s", captureWinnersDirtyFactsSQL)
	}
}

// TestQueueWinnersAuditRepairsSQLIsKeyOrderedVersionedUpsert pins the #10827
// audit-queue shape: same conflict target and version-only bump as the
// writer paths, fed in conflict-key order.
func TestQueueWinnersAuditRepairsSQLIsKeyOrderedVersionedUpsert(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"INSERT INTO supply_chain_impact_winners_dirty_log",
		"'audit'",
		"ORDER BY s.package_id, s.repository_id, s.subject_digest, s.c0, s.advisory_id",
		"ON CONFLICT (package_id, repository_id, subject_digest, c0, advisory_id) WHERE kind = 'fact'",
		"DO UPDATE SET dirty_version = supply_chain_impact_winners_dirty_log.dirty_version + 1",
	} {
		if !strings.Contains(queueWinnersAuditRepairsSQL, want) {
			t.Errorf("audit queue statement does not contain %q:\n%s", want, queueWinnersAuditRepairsSQL)
		}
	}
}
