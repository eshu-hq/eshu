// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// CICDReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the CI/CD run-correlation reads (the
// run-correlation list plus the run-correlation count and inventory
// aggregates). Canonical reads persist through explicit core-owned
// admission contracts: eshu-api keeps the HTTP shape and calls this
// versioned port, which the core-owned correlation store and run
// aggregate store satisfy jointly, composed by the consumer. A
// breaking change to the read shape fails the frozen method-set test
// until that list is deliberately updated; bump this version
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
const CICDReadAdmissionContractVersion = "v0"

// CICDReadAdmission is the versioned admission-contract port the API
// handler will depend on instead of the two shared handles. It
// declares exactly the three reads the CI/CD routes admit through —
// the bounded run-correlation list (ListCICDRunCorrelations,
// mirroring querycontract.CICDRunCorrelationStore as exercised
// through the run-correlations leg) and the cheap-summary aggregates
// (CountRunCorrelations, RunCorrelationInventory, mirroring
// RunCorrelationAggregateStore as exercised through the count and
// inventory legs) — so the boundary is held honest by the
// compile-time mirror assertion below plus the frozen method-set
// contract test. It will back the eshu-api extraction handoff for
// these reads. The seam is read-only on purpose: every declared
// method is a bounded read and the port carries no mutation surface,
// so a dest consumer implementing only this contract can serve the
// CI/CD reads but cannot acquire a write it did not bargain for
// through it.
//
// W-wave consumer: W5 CICD (all 3 oracle CICD rows).
//
// Narrow by construction, recorded not silently assumed: the methods
// re-declare the store signatures in this CI/CD-scoped shape instead
// of embedding or aliasing querycontract.CICDRunCorrelationStore or
// RunCorrelationAggregateStore. This pin therefore grants nothing to
// the other families that read through shared ports — every other
// store consumer keeps its own per-route relay reason until deliberate
// pins cover them.
type CICDReadAdmission interface {
	ListCICDRunCorrelations(context.Context, querycontract.CICDRunCorrelationFilter) ([]querycontract.CICDRunCorrelationRow, error)
	CountRunCorrelations(context.Context, RunCorrelationAggregateFilter) (RunCorrelationAggregateCount, error)
	RunCorrelationInventory(
		context.Context,
		RunCorrelationAggregateFilter,
		RunCorrelationInventoryDimension,
		int,
		int,
	) ([]RunCorrelationInventoryRow, error)
}

// The mirror assertion keeps the CI/CD-scoped re-declaration honest,
// and it works in one direction only: removal, rename, or re-signing
// of a mirrored method on any staying handler-facing port fails
// compilation here. A new method on those ports does not widen this
// pin — adoption takes deliberate re-declaration plus a frozen-list
// update. It pins no shared port — the assertion names the read
// surface only as the shapes this pin mirrors, and no other family
// may cite this pin as coverage.
var _ CICDReadAdmission = interface {
	querycontract.CICDRunCorrelationStore
	RunCorrelationAggregateStore
}(nil)
