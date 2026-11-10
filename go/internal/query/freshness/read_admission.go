// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/status/changedsince"
	"github.com/eshu-hq/eshu/go/internal/status/generation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// FreshnessReadAdmissionContractVersion versions the #9669 D13
// admission-contract shape for the Freshness family reads: the bounded
// generation-lifecycle page and the two bounded changed-since delta
// summaries behind the freshness drilldown routes. Canonical reads
// persist through explicit core-owned admission contracts: eshu-api
// keeps the HTTP shape and calls this versioned port, which the
// core-owned Postgres status store satisfies. A breaking change to the
// read shape fails the frozen method-set test until that list is
// deliberately updated; bump this version alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
const FreshnessReadAdmissionContractVersion = "v0"

// FreshnessReadAdmission is the versioned admission-contract port the
// API handler depends on instead of a direct StatusStore handle. It
// re-declares exactly the three StatusStore signatures the freshness
// drilldown routes admit through — the repository-scope changed-since
// delta, the generation-lifecycle page, and the service-scope
// changed-since delta — so the boundary is held honest by the
// compile-time mirror assertion below plus the frozen method-set
// contract test; it will back the eshu-api extraction handoff for
// this family. The seam is read-only on purpose: every declared
// method is a bounded read and the port carries no mutation surface,
// so a dest consumer implementing only this contract can serve
// freshness drilldowns but cannot acquire a write it did not bargain
// for through it.
//
// D-wave consumer: D13 Freshness-first Wave-1, per the arbiter Wave-1
// ruling on #9669 (APPROVED-WITH-CONDITIONS). The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin
// is invalid. Arbiter review of this pin precedes satellite
// termination of the freshness legs.
//
// In scope (each route admits its reads through exactly this):
//
//   - GET /api/v0/freshness/generations admits its lifecycle page
//     through ListGenerationLifecycle, the bounded, ordered scope
//     generation history read.
//   - GET /api/v0/freshness/changed-since admits its delta summary
//     through ComputeChangedSinceDelta, the bounded two-generation
//     fact-set diff read.
//   - GET /api/v0/freshness/services/changed-since admits its
//     service delta summary through ComputeServiceChangedSinceDelta,
//     the bounded service-lineage evidence diff read.
//
// Narrow by construction, recorded not silently assumed: the three
// methods re-declare the StatusStore signatures in this
// Freshness-scoped shape instead of embedding the
// GenerationLifecycleReader, ChangedSinceReader, or
// ServiceChangedSinceReader ports or aliasing the store, so no other
// family can cite this pin — a blanket or shared-port pin is
// forbidden, and the widened shape needs arbiter review before
// satellite consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - The changed-since link-ledger read path
//     (StatusStore.LedgerReadEnabled, #7127 PR-3c) is not pinned
//     here. The satellite-consumable constructor below builds the
//     store ledger-off, so the read serves the legacy two-generation
//     diff exactly as before and issues no ledger SQL; opting into
//     the ledger stays a core-side wiring decision with no wire
//     change.
//   - The stored-summary reader (StatusStore.WithSummaryReader and
//     the process-wide StatusSummaryReader, #7009) is not pinned
//     here: there is no summary method in the frozen set, so a dest
//     consumer cannot acquire stored-summary reads through it.
//   - The snapshot legs (ReadStatusSnapshot,
//     ReadStatusSnapshotFiltered, ReadRawSnapshot) stay covered by
//     status.StatusReadAdmission v0, not widened here.
//   - The grant checks (RepositoryAccessFilterFromContext plus the
//     grant comparison), the fixed failure messages, and
//     QueryProfile are handler behavior, not port shape: there is no
//     method set to freeze, the same standing as Clock and
//     FinalizeTimeout in the mutation pins.
//
// Rollback: revert this pin. The seam is unwired — no handler,
// route, or caller depends on it yet — so rollback moves no traffic
// and changes no data.
type FreshnessReadAdmission interface {
	ComputeChangedSinceDelta(context.Context, changedsince.Filter) (changedsince.Summary, error)
	ComputeServiceChangedSinceDelta(context.Context, changedsince.ServiceFilter) (changedsince.ServiceSummary, error)
	ListGenerationLifecycle(context.Context, generation.LifecycleFilter) (generation.LifecyclePage, error)
}

// The mirror assertion keeps the Freshness-scoped re-declaration
// honest: any one-sided signature drift on the three StatusStore
// methods fails compilation here. It pins no wider store surface —
// the assertion names StatusStore only as the shape this pin
// mirrors, and no other family may cite this pin as coverage.
var _ FreshnessReadAdmission = postgres.StatusStore{}

// NewReadAdmissionWithReadStore builds the Freshness-scoped admission
// satisfier over a guarded read-only query port, so the satellite
// consumes the pin without a direct table or queue handle: pass the
// request-scoped db.ReadStore the same way the API wiring does. The
// store is ledger-off with no stored-summary reader, so the reads
// serve the legacy two-generation diff exactly as before.
func NewReadAdmissionWithReadStore(reader db.ReadStore) FreshnessReadAdmission {
	return postgres.NewStatusStore(reader)
}
