// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestSortDedupWinnersDirtyIdentifiersOrdersByTheConflictKey pins the Go half
// of the #10837 direction-C capture shape: identifiers reach the staging
// insert sorted by the fact conflict key with exact duplicates removed, so
// one statement's row batch is deterministic and never stages one unit
// twice. The staging INSERT takes no row locks, so the order is a batch
// shape, not a lock order.
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

// TestCaptureWinnersDirtyFactsSQLIsKeyOrderedVersionedUpsert pins the #10837
// direction-C capture shape: rows stage into the shared queue with one row
// per mark, so a re-mark of a queued key never waits on the log's conflict
// arbiter. The statement names the staging table and the writer's source,
// and carries neither an ORDER BY (no locks to take in order) nor an
// ON CONFLICT (no unique index on the hot path): the tick collapse restores
// the version math.
func TestCaptureWinnersDirtyFactsSQLIsKeyOrderedVersionedUpsert(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"INSERT INTO supply_chain_impact_winners_dirty_staging",
		"'fact_upsert'",
	} {
		if !strings.Contains(captureWinnersDirtyFactsSQL, want) {
			t.Errorf("capture statement does not contain %q:\n%s", want, captureWinnersDirtyFactsSQL)
		}
	}
	for _, banned := range []string{
		"supply_chain_impact_winners_dirty_log",
		"ORDER BY",
		"ON CONFLICT",
		"dirty_version",
	} {
		if strings.Contains(captureWinnersDirtyFactsSQL, banned) {
			t.Errorf("capture statement contains %q, the hot path it must avoid:\n%s", banned, captureWinnersDirtyFactsSQL)
		}
	}
}

// TestQueueWinnersAuditRepairsSQLIsKeyOrderedVersionedUpsert pins the #10837
// direction-C audit-queue shape: verified stale keys stage as source
// "audit" with no arbiter, and the pending check (which unions the staging
// queue) keeps a staged key from being re-queued.
func TestQueueWinnersAuditRepairsSQLIsKeyOrderedVersionedUpsert(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"INSERT INTO supply_chain_impact_winners_dirty_staging",
		"'audit'",
		"winners_audit_pending",
	} {
		if !strings.Contains(queueWinnersAuditRepairsSQL, want) {
			t.Errorf("audit queue statement does not contain %q:\n%s", want, queueWinnersAuditRepairsSQL)
		}
	}
	for _, banned := range []string{
		"supply_chain_impact_winners_dirty_log",
		"ON CONFLICT",
		"dirty_version",
	} {
		if strings.Contains(queueWinnersAuditRepairsSQL, banned) {
			t.Errorf("audit queue statement contains %q, the hot path it must avoid:\n%s", banned, queueWinnersAuditRepairsSQL)
		}
	}
}

// TestCollapseWinnersStagingFactSQLPinsVersionArithmetic pins the #10837
// direction-C conservation contract in the fact collapse arm: new keys land
// with dirty_version set to the staged mark count, and a queued key's
// version grows by exactly the staged count (never +1). The SELECT feeds
// the merge in conflict-key order, and the delete removes exactly the
// staged seqs the merge read.
func TestCollapseWinnersStagingFactSQLPinsVersionArithmetic(t *testing.T) {
	t.Parallel()

	if !strings.HasPrefix(strings.TrimSpace(collapseWinnersStagingFactSQL), "WITH staged AS (") {
		t.Errorf("fact collapse does not open with the shared staged CTE:\n%s", collapseWinnersStagingFactSQL)
	}
	if !strings.Contains(collapseWinnersStagingFactSQL, collapseWinnersStagingFactSelectSQL) {
		t.Errorf("fact collapse does not run the pinned SELECT arm byte-identically (the EXPLAIN proof derives from the arm):\n%s", collapseWinnersStagingFactSQL)
	}
	for _, want := range []string{
		"INSERT INTO supply_chain_impact_winners_dirty_log",
		"min(captured_at) AS first_mark",
		"(array_agg(source ORDER BY seq))[1] AS first_source",
		"array_agg(seq) AS seqs",
		"ORDER BY package_id, repository_id, subject_digest, c0, advisory_id",
		"ON CONFLICT (package_id, repository_id, subject_digest, c0, advisory_id) WHERE kind = 'fact'",
		"DO UPDATE SET dirty_version = supply_chain_impact_winners_dirty_log.dirty_version + EXCLUDED.dirty_version",
		"USING (SELECT unnest(seqs) AS seq FROM staged)",
	} {
		if !strings.Contains(collapseWinnersStagingFactSQL, want) {
			t.Errorf("fact collapse does not contain %q:\n%s", want, collapseWinnersStagingFactSQL)
		}
	}
}

// TestCollapseWinnersStagingScopeSQLIsTheScopeTwin pins the scope collapse
// arm: the same version arithmetic over the (scope_id, generation_id) pair,
// key-ordered, targeting the scope partial unique index.
func TestCollapseWinnersStagingScopeSQLIsTheScopeTwin(t *testing.T) {
	t.Parallel()

	if !strings.Contains(collapseWinnersStagingScopeSQL, collapseWinnersStagingScopeSelectSQL) {
		t.Errorf("scope collapse does not run the pinned SELECT arm byte-identically:\n%s", collapseWinnersStagingScopeSQL)
	}
	for _, want := range []string{
		"INSERT INTO supply_chain_impact_winners_dirty_log",
		"min(captured_at) AS first_mark",
		"(array_agg(source ORDER BY seq))[1] AS first_source",
		"ORDER BY scope_id, generation_id",
		"ON CONFLICT (scope_id, generation_id) WHERE kind = 'scope'",
		"DO UPDATE SET dirty_version = supply_chain_impact_winners_dirty_log.dirty_version + EXCLUDED.dirty_version",
		"USING (SELECT unnest(seqs) AS seq FROM staged) AS u",
	} {
		if !strings.Contains(collapseWinnersStagingScopeSQL, want) {
			t.Errorf("scope collapse does not contain %q:\n%s", want, collapseWinnersStagingScopeSQL)
		}
	}
}

// TestStagingUnionRemainingUnionsDepthAndMinimum pins the residual math one
// tick stamps: depths add, and the horizon minimum is the earlier of the two
// minima. An invalid minimum is an empty set and always loses; two empty
// sets stay invalid so the caller can tell "empty" from "missing".
func TestStagingUnionRemainingUnionsDepthAndMinimum(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	earlier := now.Add(-time.Second)
	logMin := sql.NullTime{Time: now, Valid: true}
	stagingMin := sql.NullTime{Time: earlier, Valid: true}
	var invalid sql.NullTime

	if got, min := stagingUnionRemaining(3, logMin, 5, stagingMin); got != 8 || !min.Valid || !min.Time.Equal(earlier) {
		t.Errorf("stagingUnionRemaining(3, now, 5, earlier) = (%d, %v), want (8, earlier)", got, min)
	}
	if got, min := stagingUnionRemaining(0, invalid, 4, stagingMin); got != 4 || !min.Valid || !min.Time.Equal(earlier) {
		t.Errorf("staging-only remainder = (%d, %v), want (4, earlier)", got, min)
	}
	if got, min := stagingUnionRemaining(2, logMin, 0, invalid); got != 2 || !min.Valid || !min.Time.Equal(now) {
		t.Errorf("log-only remainder = (%d, %v), want (2, now)", got, min)
	}
	if got, min := stagingUnionRemaining(0, invalid, 0, invalid); got != 0 || min.Valid {
		t.Errorf("empty remainder = (%d, %v), want (0, invalid)", got, min)
	}
}
