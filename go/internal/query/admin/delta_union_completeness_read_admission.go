// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// DeltaUnionCompletenessReadAdmissionContractVersion versions the
// #9669 Gap 02 admission-contract shape for the delta-union
// completeness read (GET /api/v0/admin/delta-union/completeness).
// Canonical reads persist through explicit core-owned admission
// contracts: eshu-api keeps the HTTP shape and calls this versioned
// port, which the core-owned DeltaUnionCompletenessChecker satisfies.
// A breaking change to the read shape fails the frozen method-set test
// until that list is deliberately updated; bump this version alongside
// (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
const DeltaUnionCompletenessReadAdmissionContractVersion = "v0"

// DeltaUnionCompletenessReadAdmission is the versioned
// admission-contract port the API handler depends on instead of a
// direct checker handle. It carries exactly the Check method the
// route admits through, so the boundary is held honest by the
// compile-time assertion below plus the frozen method-set contract
// test — any one-sided signature drift fails compilation or the
// test; it will back the eshu-api extraction handoff for this read. The
// seam is read-only on purpose: the check compares the ACTIVE fact
// side against anchored per-repo graph reads and never repairs, so a
// dest consumer implementing only this contract can serve the report
// but cannot acquire a write it did not bargain for through it.
//
// D-wave consumer: D3 (admin reads, per the arbiter D-wave ruling on
// #9669: Decision 3 / census Q4 — admin reads go native only after the
// pin). The satellite records this pin in a follow-up; a satellite PR
// consuming an unminted pin is invalid.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - DeltaUnionActiveSource and DeltaUnionGraphRunner stay out: they
//     are the checker's own fact and graph dependencies, separate
//     seams with their own owners, not this route's admission shape.
//     Pinning them here would widen the boundary past the read.
//   - Store-as-a-whole and ReadStore stay out: the check never touches
//     fact_work_items. Pinning either would be a blanket pin; the
//     minting rule bars those.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type DeltaUnionCompletenessReadAdmission interface {
	Check(ctx context.Context, scopeIDs []string) (recovery.DeltaUnionCompletenessReport, error)
}

var _ DeltaUnionCompletenessReadAdmission = (*DeltaUnionCompletenessChecker)(nil)
