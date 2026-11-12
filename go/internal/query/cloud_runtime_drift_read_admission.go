// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
)

// CloudRuntimeDriftReadAdmissionContractVersion versions the #9669 W8b
// admission-contract shape for the CloudRuntimeDrift family reads: the
// bounded active-finding page and the total active count behind the
// provider-neutral runtime drift readback and the drift investigation
// packet. Canonical reads persist through explicit core-owned admission
// contracts: eshu-api keeps the HTTP shape and calls this versioned
// port, which the core-owned Postgres multi-cloud drift store satisfies.
// A breaking change to the read shape fails the frozen method-set test
// until that list is deliberately updated; bump this version alongside
// (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
const CloudRuntimeDriftReadAdmissionContractVersion = "v0"

// CloudRuntimeDriftReadAdmission is the versioned admission-contract
// port the API handler will depend on instead of a direct
// MultiCloudRuntimeDriftStore handle. It re-declares exactly the two
// reads the CloudRuntimeDrift family admits through — the bounded
// provider-aggregated active-finding page and the total active count
// for the same bounded filters — so the boundary is held honest by the
// compile-time mirror assertions below plus the frozen method-set
// contract test; it will back the eshu-api extraction handoff for this
// family. The seam is read-only on purpose: both declared methods are
// bounded reads and the port carries no mutation surface, so a dest
// consumer implementing only this contract can serve the drift
// readbacks but cannot acquire a write it did not bargain for through
// it.
//
// D-wave consumer: W8b tier-1 CloudRuntimeDrift pin, per the arbiter W8b
// ruling on #9669 (APPROVED-WITH-CONDITIONS). The satellite records this
// pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid. Arbiter review of this pin precedes satellite termination of
// the drift legs.
//
// In scope (each route admits its reads through exactly this):
//
//   - POST /api/v0/cloud/runtime-drift/findings admits its bounded,
//     filterable, paginated readback through
//     ListActiveMultiCloudRuntimeDriftFindings plus
//     CountActiveMultiCloudRuntimeDriftFindings for the truncation
//     total.
//   - GET /api/v0/investigations/drift/packet admits its packet build
//     through ListActiveMultiCloudRuntimeDriftFindings only; the
//     packet carries no total, so the count read stays unexercised on
//     this leg by construction.
//
// Narrow by construction, recorded not silently assumed: the two
// methods re-declare the MultiCloudRuntimeDriftStore signatures in this
// CloudRuntimeDrift-scoped shape instead of embedding the store or
// aliasing it, so no other family can cite this pin — a blanket or
// shared-port pin is forbidden, and the widened shape needs arbiter
// review before satellite consumption. Citing TerraformDrift #10442 or
// IaC #10455 (or the drift pin's mcp37 same-handler sentence) as
// coverage for either route here is void: neither covers the
// provider-aggregated MultiCloudRuntimeDriftStore surface this pin
// re-declares.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - GraphQuery.Run and RunSingle: no drift route issues a graph read;
//     the inventory, selector, and reachability legs belong to the IaC
//     pin, never widened here.
//   - The ContentStore surface: no drift route reads file or entity
//     content; the dead-route content fallback rides the frozen
//     CoreReadContracts, not this pin.
//   - The AWS-specific finding row extras (MatchedTerraformConfigFile,
//     ModulePath, service and environment candidates, dependency
//     paths): the pin projects exactly the provider-neutral row the
//     store returns, and those fields are structurally unreachable on
//     any real fact.
//   - The grant checks (scoped-empty short-circuits, the direct scope
//     grant comparison), the fixed failure messages, the packet bounds,
//     the truth envelope, and QueryProfile are handler behavior, not
//     port shape: there is no method set to freeze, the same standing
//     as Clock and FinalizeTimeout in the mutation pins.
//
// There is deliberately no WithReadStore constructor on this pin: the
// satellite keeps building the Postgres adapter the way cmd/api wiring
// does today
// (NewPostgresMultiCloudRuntimeDriftStoreWithReadStore over the
// request-scoped guarded reader) and the pin freezes only the read
// shape the handlers may call through.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type CloudRuntimeDriftReadAdmission interface {
	// ListActiveMultiCloudRuntimeDriftFindings returns one bounded page
	// of active findings for the caller's scope.
	ListActiveMultiCloudRuntimeDriftFindings(
		ctx context.Context,
		filter MultiCloudRuntimeDriftFilter,
	) ([]MultiCloudRuntimeDriftFindingRow, error)
	// CountActiveMultiCloudRuntimeDriftFindings returns the total active
	// finding count for the same bounded filters, used to report
	// truncation.
	CountActiveMultiCloudRuntimeDriftFindings(
		ctx context.Context,
		filter MultiCloudRuntimeDriftFilter,
	) (int, error)
}

// The mirror assertion keeps the CloudRuntimeDrift-scoped re-declaration
// honest, and it works in one direction only: removal, rename, or
// re-signing of either mirrored method on MultiCloudRuntimeDriftStore
// fails compilation here. A store addition deliberately does not widen
// this pin — a new store method is adopted only by deliberate
// re-declaration plus a frozen-list update. It pins no shared port —
// the assertion names the store only as the shape this pin mirrors, and
// no other family may cite this pin as coverage.
var _ CloudRuntimeDriftReadAdmission = MultiCloudRuntimeDriftStore(nil)

// The satisfier assertion proves the production adapter the API wiring
// builds today satisfies the pin: the Postgres multi-cloud drift store,
// in both its constructors, answers exactly the two pinned reads.
var _ CloudRuntimeDriftReadAdmission = (*PostgresMultiCloudRuntimeDriftStore)(nil)
