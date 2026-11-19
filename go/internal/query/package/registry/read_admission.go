// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// PackageRegistryReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the package-registry reads (the
// package list, versions, dependencies, correlations, and
// dependency-chains plus the package count and inventory
// aggregates). Canonical reads persist through explicit core-owned
// admission contracts: eshu-api keeps the HTTP shape and calls this
// versioned port, which the core-owned graph reader, correlation
// store, and aggregate store satisfy jointly, composed by the
// consumer. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const PackageRegistryReadAdmissionContractVersion = "v0"

// PackageRegistryReadAdmission is the versioned admission-contract
// port the API handler will depend on instead of the three shared
// handles. It declares exactly the four reads the package-registry
// routes admit through — the bounded graph read (Run, mirroring
// querycontract.GraphQuery with RunSingle deliberately excluded: no
// registry leg issues a single-row graph read), the correlation read
// (ListPackageRegistryCorrelations, mirroring CorrelationStore as
// exercised through the correlations leg), and the package
// aggregates (CountPackageRegistryPackages,
// PackageRegistryPackageInventory, mirroring AggregateStore as
// exercised through the count and inventory legs) — so the boundary
// is held honest by the compile-time mirror assertion below plus the
// frozen method-set contract test. It will back the eshu-api
// extraction handoff for these reads. The seam is read-only on
// purpose: every declared method is a bounded read and the port
// carries no mutation surface, so a dest consumer implementing only
// this contract can serve the package-registry reads but cannot
// acquire a write it did not bargain for through it.
//
// W-wave consumer: W5 package-registry (all 7 oracle PackageRegistry
// rows).
//
// Narrow by construction, recorded not silently assumed: the methods
// re-declare the store signatures in this package-registry-scoped
// shape instead of embedding or aliasing querycontract.GraphQuery,
// CorrelationStore, or AggregateStore. This pin therefore grants
// nothing to the other families that read through shared ports —
// every other store consumer keeps its own per-route relay reason
// until deliberate pins cover them.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - RunSingle stays unpinned here by construction: no
//     package-registry leg issues a single-row graph read.
//   - The Content handle and the CollectorReadiness probe are handler
//     behavior, not port shape: the legs exercise no content read,
//     and the readiness probe is an optional envelope, not an
//     admitted read — there is no method set to freeze for either.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type PackageRegistryReadAdmission interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
	ListPackageRegistryCorrelations(context.Context, CorrelationFilter) (CorrelationPage, error)
	CountPackageRegistryPackages(context.Context, AggregateFilter) (AggregateCount, error)
	PackageRegistryPackageInventory(
		context.Context,
		AggregateFilter,
		InventoryDimension,
		int,
		int,
	) ([]InventoryRow, error)
}

// The mirror assertion keeps the package-registry-scoped
// re-declaration honest, and it works in one direction only:
// removal, rename, or re-signing of a mirrored method on any staying
// handler-facing port fails compilation here. A new method on those
// ports does not widen this pin — adoption takes deliberate
// re-declaration plus a frozen-list update. It pins no shared port —
// the assertion names the read surface only as the shapes this pin
// mirrors, and no other family may cite this pin as coverage.
var _ PackageRegistryReadAdmission = interface {
	querycontract.GraphQuery
	CorrelationStore
	AggregateStore
}(nil)
