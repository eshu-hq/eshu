// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
)

// InfraReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the infrastructure reads (the cloud
// resource list, the ecosystem overview and graph-summary, the
// infra resource search and relationships, the relationship catalog
// and edges, and the infra resource count and inventory aggregates).
// Canonical reads persist through explicit core-owned admission
// contracts: eshu-api keeps the HTTP shape and calls this versioned
// port, which the core-owned graph reader, cloud-resource list store,
// and infra aggregate store satisfy jointly, composed by the
// consumer. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const InfraReadAdmissionContractVersion = "v0"

// InfraReadAdmission is the versioned admission-contract port the API
// handler will depend on instead of the three shared handles. It
// declares exactly the five reads the infra routes admit through —
// the bounded graph reads (Run, RunSingle, mirroring GraphQuery as
// exercised through the search, relationships, ecosystem, catalog,
// and edges legs), the cloud-resource identity list
// (ListCloudResourceIdentities, mirroring CloudResourceListStore as
// exercised through the cloud-resources leg), and the cheap-summary
// aggregates (CountInfraResources, InfraResourceInventory, mirroring
// InfraResourceAggregateStore as exercised through the count and
// inventory legs) — so the boundary is held honest by the
// compile-time mirror assertion below plus the frozen method-set
// contract test. It will back the eshu-api extraction handoff for
// these reads. The seam is read-only on purpose: every declared
// method is a bounded read and the port carries no mutation surface,
// so a dest consumer implementing only this contract can serve the
// infra reads but cannot acquire a write it did not bargain for
// through it.
//
// W-wave consumer: W5 infra (all 9 oracle Infra rows).
//
// Narrow by construction, recorded not silently assumed: the methods
// re-declare the store signatures in this infra-scoped shape instead
// of embedding or aliasing GraphQuery, CloudResourceListStore, or
// InfraResourceAggregateStore. This pin therefore grants nothing to
// the other families that read through shared ports — every other
// store consumer keeps its own per-route relay reason until deliberate
// pins cover them.
type InfraReadAdmission interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
	RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error)
	ListCloudResourceIdentities(context.Context, CloudResourceListPageFilter) ([]CloudResourceListIdentity, error)
	CountInfraResources(context.Context, InfraResourceAggregateFilter) (InfraResourceAggregateCount, error)
	InfraResourceInventory(
		context.Context,
		InfraResourceAggregateFilter,
		InfraResourceInventoryDimension,
		int,
		int,
	) ([]InfraResourceInventoryRow, InfraResourceAggregateSource, error)
}

// The mirror assertion keeps the infra-scoped re-declaration honest,
// and it works in one direction only: removal, rename, or re-signing
// of a mirrored method on any staying handler-facing port fails
// compilation here. A new method on those ports does not widen this
// pin — adoption takes deliberate re-declaration plus a frozen-list
// update. It pins no shared port — the assertion names the read
// surface only as the shapes this pin mirrors, and no other family
// may cite this pin as coverage.
var _ InfraReadAdmission = interface {
	GraphQuery
	CloudResourceListStore
	InfraResourceAggregateStore
}(nil)
