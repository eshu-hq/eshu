// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// suppressionRowCapFakePage builds one page of `count` fake
// vulnerability.suppression rows in scanFactEnvelope's 16-column order, for
// TestListActiveSupplyChainImpactFactsCapsRowsPerCall below.
func suppressionRowCapFakePage(startIndex, count int, baseTime time.Time) [][]any {
	page := make([][]any, count)
	for i := range page {
		factID := fmt.Sprintf("vuln-suppression:row-cap-%04d", startIndex+i)
		page[i] = []any{
			factID,
			"vex-scope:row-cap",
			"generation-1",
			"vulnerability.suppression",
			"stable-" + factID,
			"1.0.0",
			"vex",
			int64(1),
			"observed",
			"vex",
			factID,
			"",
			"",
			baseTime.Add(time.Duration(startIndex+i) * time.Second),
			false,
			[]byte(`{"suppression_id":"` + factID + `","scope":{"cve_id":"CVE-2026-0700"}}`),
		}
	}
	return page
}

// TestListActiveSupplyChainImpactFactsCapsRowsPerCall is the #5466 round-7
// review P1-B fix proof: even an unexpectedly broad identity filter cannot
// paginate without a per-call ceiling. This hermetic test lowers
// maxSupplyChainImpactActiveEvidenceRowsPerCall to a small number so it can
// prove the stop-early behavior without seeding thousands of rows:
// the fake queryer is primed with MORE pages than the lowered cap allows,
// and the test asserts (a) the returned envelope count is capped, not the
// full row count the fake could have produced, (b) the truncated bool is
// true, and (c) fewer queries were issued than exhausting every primed page
// would require -- proving genuine early termination, not a coincidental
// row count.
func TestListActiveSupplyChainImpactFactsCapsRowsPerCall(t *testing.T) {
	originalCap := maxSupplyChainImpactActiveEvidenceRowsPerCall
	maxSupplyChainImpactActiveEvidenceRowsPerCall = listFactsByKindPageSize + 1
	t.Cleanup(func() { maxSupplyChainImpactActiveEvidenceRowsPerCall = originalCap })

	baseTime := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
	// Prime FOUR full pages (4x listFactsByKindPageSize rows total) so the
	// fake queryer has far more than the lowered cap (listFactsByKindPageSize+1)
	// available -- if the cap did not stop pagination early, the loader
	// would exhaust all four primed pages and then fail on the fifth
	// QueryContext call (queryResponses would be empty), which the "fewer
	// queries than pages primed" assertion below also guards against.
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: suppressionRowCapFakePage(0, listFactsByKindPageSize, baseTime)},
			{rows: suppressionRowCapFakePage(listFactsByKindPageSize, listFactsByKindPageSize, baseTime)},
			{rows: suppressionRowCapFakePage(2*listFactsByKindPageSize, listFactsByKindPageSize, baseTime)},
			{rows: suppressionRowCapFakePage(3*listFactsByKindPageSize, listFactsByKindPageSize, baseTime)},
		},
	}
	store := NewFactStore(db)

	loaded, truncated, err := store.ListActiveSupplyChainImpactFacts(context.Background(), reducer.SupplyChainImpactFactFilter{
		CVEIDs: []string{"CVE-2026-0700"},
	})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts() error = %v, want nil", err)
	}
	if !truncated {
		t.Fatalf("truncated = false, want true (loaded %d rows against a %d-row cap)", len(loaded), maxSupplyChainImpactActiveEvidenceRowsPerCall)
	}
	if got, want := len(loaded), 2*listFactsByKindPageSize; got != want {
		t.Fatalf("len(loaded) = %d, want %d (the cap (%d) is crossed exactly after the SECOND page, so the loop must stop there, not return every primed row)", got, want, maxSupplyChainImpactActiveEvidenceRowsPerCall)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want %d (stopping after the cap is crossed, not exhausting all four primed pages)", got, want)
	}
}

// TestListActiveSupplyChainImpactFactsDoesNotTruncateUnderTheCap proves the
// cap does not fire, and truncated stays false, for a normal-sized result
// well under the cap -- the common case for the pre-existing
// cve_id/package_id/purl/subject_digest/repository_id/advisory_id anchors,
// which this fix must not regress.
func TestListActiveSupplyChainImpactFactsDoesNotTruncateUnderTheCap(t *testing.T) {
	baseTime := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: suppressionRowCapFakePage(0, 3, baseTime)},
		},
	}
	store := NewFactStore(db)

	loaded, truncated, err := store.ListActiveSupplyChainImpactFacts(context.Background(), reducer.SupplyChainImpactFactFilter{
		CVEIDs: []string{"CVE-2026-0700"},
	})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts() error = %v, want nil", err)
	}
	if truncated {
		t.Fatalf("truncated = true, want false (only 3 rows, far under the cap)")
	}
	if got, want := len(loaded), 3; got != want {
		t.Fatalf("len(loaded) = %d, want %d", got, want)
	}
}
