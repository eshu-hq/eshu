// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// IaCReadAdmissionContractVersion versions the #9669 O6 pin-3
// admission-contract shape for the IaC reads (GET
// /api/v0/iac/resources, GET /api/v0/replatforming/selectors, and
// the IaC POST family). Canonical reads persist through
// explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned graph,
// inventory, management, selector, and reachability stores satisfy
// jointly, composed by the consumer. A breaking change to the read
// shape fails the frozen method-set test until that list is
// deliberately updated; bump this version alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
const IaCReadAdmissionContractVersion = "v0"

// IaCReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of the five shared handles. It
// declares exactly the eight reads the IaC routes admit through —
// the bounded inventory graph read (Run, mirroring
// querycontract.GraphQuery with RunSingle deliberately excluded),
// the inventory facet read (Summary, mirroring InventoryStore as
// exercised through the resources facet leg), the selector
// inventory read (ListReplatformingSelectors, mirroring
// ReplatformingSelectorStore as exercised through the
// type-asserted Management handle), the cloud-management finding
// reads (ListUnmanagedCloudResources and
// CountUnmanagedCloudResources, mirroring ManagementStore), and
// the materialized cleanup reads (ListLatestCleanupFindings,
// CountLatestCleanupFindings, and HasLatestRows, mirroring the
// ReachabilityStore surface in handler.go) — so the boundary is
// held honest by the compile-time mirror assertion below plus the
// frozen method-set contract test. It will back the eshu-api
// extraction handoff for these reads. The seam is read-only on
// purpose: every declared method is a bounded read and the port
// carries no mutation surface, so a dest consumer implementing only
// this contract can serve the IaC reads but cannot acquire a write
// it did not bargain for through it.
//
// O6 consumer: IaC deliberate widened pin (O6 pin 3, AM-D4-IAC —
// the widened shape carries the two POST-only stores alongside the
// GET legs, so the direction stands but the shape delta needs
// arbiter review per the Compare gap-04 standing before satellite
// consumption). The satellite records this pin in a follow-up; a
// satellite PR consuming an unminted pin is invalid. Arbiter review
// of this pin precedes satellite termination of the IaC legs.
//
// Narrow by construction, recorded not silently assumed: the eight
// methods re-declare the shared-port signatures in this IaC-scoped
// shape instead of embedding or aliasing querycontract.GraphQuery,
// InventoryStore, ManagementStore, ReplatformingSelectorStore, or
// ReachabilityStore. This pin therefore grants nothing to the other
// families that read through those shared ports — every other
// GraphQuery, ContentStore, or management-surface consumer keeps
// its own per-route relay reason until deliberate pins cover them.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - The grant checks (RepositoryAccessFilterFromContext plus the
//     scope/repo grant comparisons, bindIaCManagementFilterAccess,
//     and the scoped-empty short-circuits) and the fixed failure
//     messages are handler behavior, not port shape: there is no
//     method set to freeze, the same standing as QueryProfile in
//     this handler.
//   - GraphQuery.RunSingle is deliberately excluded: the resource
//     list hydrates its bounded inventory page through Run only.
//     The dead-route content-fallback leg (ListRepositories,
//     MatchRepositories, ListRepoFiles, GetFileContent) rides the
//     IaCDeadContentAdmission v0 supplement beside this pin, never
//     inside it. MetricsReadAdmission does not apply: non-test iac
//     touches no TimeSeries source.
//   - No suppression leg exists on these routes; the suppressions
//     mutation row stays the Decision 2 tier-C survivor via
//     SuppressionAdmission, never a read termination here.
//
// Rollback: revert this pin. The seam is unwired — no handler,
// route, or caller depends on it yet — so rollback moves no traffic
// and changes no data.
type IaCReadAdmission interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
	Summary(ctx context.Context, access querycontract.RepositoryAccessFilter, limit int) (InventorySummary, error)
	ListReplatformingSelectors(ctx context.Context, limit int, allowedScopeIDs []string) (ReplatformingSelectorPage, error)
	ListUnmanagedCloudResources(ctx context.Context, filter ManagementFilter) ([]ManagementFindingRow, error)
	CountUnmanagedCloudResources(ctx context.Context, filter ManagementFilter) (int, error)
	ListLatestCleanupFindings(ctx context.Context, repoIDs []string, families []string, includeAmbiguous bool, limit int, offset int) ([]ReachabilityFindingRow, error)
	CountLatestCleanupFindings(ctx context.Context, repoIDs []string, families []string, includeAmbiguous bool) (int, error)
	HasLatestRows(ctx context.Context, repoIDs []string, families []string) (bool, error)
}

// The mirror assertion keeps the IaC-scoped re-declaration
// honest, and it works in one direction only: removal, rename, or
// re-signing of any of the 8 mirrored methods on any of the five
// shared ports fails compilation here. The pin deliberately mirrors
// only part of two of those ports — GraphQuery.RunSingle and
// InventoryStore.SearchActive ride the combination below but are
// not adopted — so shared-port addition deliberately does not
// widen this pin: a new shared-port method is adopted only by
// deliberate re-declaration plus a frozen-list update. It pins no
// shared port — the assertion names the combination only as the
// shape this pin mirrors, and no other family may cite this pin as
// coverage.
var _ IaCReadAdmission = interface {
	querycontract.GraphQuery
	InventoryStore
	ManagementStore
	ReplatformingSelectorStore
	ReachabilityStore
}(nil)
