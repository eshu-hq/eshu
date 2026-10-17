// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// TestFingerprintedFusedReadKeepsThePlainReadBody pins that the fingerprinted
// fused statement and the plain fused statement read the same rows under the
// same fence: the inner foreign read and the fence predicate of the plain
// statement (listResolvedByReposWithCorpusFenceSQL, #6740) must appear verbatim
// in the fingerprinted one, so an edit to one cannot silently drift from the
// other. The bodies are cut out of the shipped constant, not copied.
func TestFingerprintedFusedReadKeepsThePlainReadBody(t *testing.T) {
	t.Parallel()

	plain := listResolvedByReposWithCorpusFenceSQL
	start := strings.Index(plain, "LEFT JOIN (\n")
	end := strings.Index(plain, ") AS r ON fence.complete")
	if start < 0 || end < start {
		t.Fatalf("plain fused statement shape changed; cannot cut the inner read:\n%s", plain)
	}
	innerRead := plain[start:end]
	// The plain statement carries %s holes for the repo id lists; the
	// fingerprinted builder fills them with $1..$n. Compare the body around the
	// holes.
	holeless := strings.NewReplacer("%s", "$1, $2").Replace(innerRead)

	fingerprinted := backfill.ListResolvedByReposWithCorpusFenceAndInputFingerprintSQL(incompleteScopeRelationshipGenerationsPredicate, 2)
	if !strings.Contains(fingerprinted, holeless) {
		t.Fatalf("fingerprinted fused read does not contain the plain inner read.\nplain inner:\n%s\nfingerprinted:\n%s",
			holeless, fingerprinted)
	}
	if !strings.Contains(fingerprinted, incompleteScopeRelationshipGenerationsPredicate) {
		t.Fatal("fingerprinted fused read does not carry the shared corpus-fence predicate")
	}
	if strings.Count(fingerprinted, ";") != 0 {
		t.Fatal("fingerprinted fused read must stay ONE statement so the fence, rows, and fingerprint share a snapshot")
	}
}

// TestFingerprintExpressionIsSharedByTheFusedReadAndTheReopenListing proves the
// recorded value and the listing-side value come from one SQL definition: both
// statements embed the exact text backfill.InputFingerprintExprSQL returns for
// their own operands, so neither side can carry a hand copy that drifts. The
// behavioral half (recorded == listing-side for one snapshot) is
// TestInputFingerprintReopenUnchangedItemStaysSucceeded on a real database.
func TestFingerprintExpressionIsSharedByTheFusedReadAndTheReopenListing(t *testing.T) {
	t.Parallel()

	fused := backfill.ListResolvedByReposWithCorpusFenceAndInputFingerprintSQL(incompleteScopeRelationshipGenerationsPredicate, 2)
	wantFused := backfill.InputFingerprintExprSQL("$3::text", "ARRAY[$1::text, $2::text]")
	if !strings.Contains(fused, wantFused) {
		t.Fatalf("fused read does not embed the shared fingerprint expression:\n%s", fused)
	}

	listing := backfill.ListSucceededDeployableUnitWorkItemsQuery
	wantListing := backfill.InputFingerprintExprSQL("work.generation_id", "fingerprint.repo_ids")
	if !strings.Contains(listing, wantListing) {
		t.Fatalf("reopen listing does not embed the shared fingerprint expression:\n%s", listing)
	}
}

// TestDeployableUnitReopenListingIsAFlagNotAReadinessFilter pins the ruling's
// forbidden shapes out of the listing: no timestamp predicate on the work row
// and no readiness gate predicate. Every succeeded item above the replay floor
// is returned, with the fingerprint verdict as a column, so a gate that is
// closed at listing time can never hide a changed item.
func TestDeployableUnitReopenListingIsAFlagNotAReadinessFilter(t *testing.T) {
	t.Parallel()

	listing := backfill.ListSucceededDeployableUnitWorkItemsQuery
	for _, forbidden := range []string{
		"activated_at > ", "activated_at >= ", "updated_at >", "work.updated_at <",
		"incomplete", "fence", "canonical_nodes_committed", "graph_projection_phase_state",
	} {
		if strings.Contains(listing, forbidden) {
			t.Errorf("reopen listing contains forbidden predicate fragment %q", forbidden)
		}
	}
	for _, want := range []string{
		"work.domain = $1",
		"AND work.status = 'succeeded'",
		"WHEN fingerprint.work_item_id IS NULL THEN false",
		"fingerprint.repo_ids",
		"AS unchanged",
	} {
		if !strings.Contains(listing, want) {
			t.Errorf("reopen listing missing %q", want)
		}
	}
	if !strings.Contains(listing, "ORDER BY work.updated_at ASC, work.work_item_id ASC") {
		t.Error("reopen listing lost the stable reopen order")
	}
}

// TestFingerprintExpressionAvoidsMD5 keeps md5() out of the shared expression:
// the production database runs FIPS-mode OpenSSL, where md5() raises "could not
// compute MD5 hash: unsupported" and would fail the reopen listing and the fused
// read on every call. SHA-256 is the approved digest there.
func TestFingerprintExpressionAvoidsMD5(t *testing.T) {
	t.Parallel()

	expr := backfill.InputFingerprintExprSQL("$1::text", "ARRAY[$2::text]")
	if strings.Contains(strings.ToLower(expr), "md5(") {
		t.Fatal("fingerprint expression calls md5(), which fails under FIPS-mode OpenSSL")
	}
	if !strings.Contains(expr, "sha256(") {
		t.Fatal("fingerprint expression does not use sha256()")
	}
}
