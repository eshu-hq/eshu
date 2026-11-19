// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// DocumentationReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the documentation reads (the findings
// list, facts list, evidence-packet fetch, packet freshness, and the
// findings count and inventory aggregates). Canonical reads persist
// through explicit core-owned admission contracts: eshu-api keeps the
// HTTP shape and calls this versioned port, which the core-owned
// content reader and finding aggregate store satisfy jointly,
// composed by the consumer. A breaking change to the read shape fails
// the frozen method-set test until that list is deliberately updated;
// bump this version alongside (manually — the bump is convention, the
// frozen list is the enforcement) so dest consumers can pin the
// shape.
const DocumentationReadAdmissionContractVersion = "v0"

// DocumentationReadAdmission is the versioned admission-contract port
// the API handler will depend on instead of the two shared handles.
// It declares exactly the eight reads the documentation routes admit
// through — the six read-model reads (DocumentationFindings,
// DocumentationFacts, DocumentationEvidencePacket,
// DocumentationEvidencePacketFreshness, and the WithFilter variants
// of the packet and freshness reads, mirroring the read-model store
// as exercised through the findings, facts, packet, and freshness
// legs) and the two cheap-summary aggregates
// (CountDocumentationFindings, DocumentationFindingInventory,
// mirroring DocumentationFindingAggregateStore as exercised through
// the count and inventory legs) — so the boundary is held honest by
// the compile-time mirror assertion below plus the frozen method-set
// contract test. It will back the eshu-api extraction handoff for
// these reads. The seam is read-only on purpose: every declared
// method is a bounded read and the port carries no mutation surface,
// so a dest consumer implementing only this contract can serve the
// documentation reads but cannot acquire a write it did not bargain
// for through it.
//
// W-wave consumer: W5 documentation (all 6 oracle Documentation
// rows).
//
// Narrow by construction, recorded not silently assumed: the methods
// re-declare the store signatures in this documentation-scoped shape
// instead of embedding the read-model or aggregate store. This pin
// therefore grants nothing to the other families that read through
// shared ports — every other store consumer keeps its own per-route
// relay reason until deliberate pins cover them.
//
// This pin adopts the packet freshness read with its packet-scoped
// filter variant only. Freshness derivations owned by other lanes
// (generations, changed-since) stay out: each keeps its own standing
// until a deliberate pin covers it.
type DocumentationReadAdmission interface {
	DocumentationFindings(context.Context, querycontract.DocumentationFindingFilter) (querycontract.DocumentationFindingListReadModel, error)
	DocumentationFacts(context.Context, querycontract.DocumentationFactFilter) (querycontract.DocumentationFactListReadModel, error)
	DocumentationEvidencePacket(context.Context, string) (querycontract.DocumentationEvidencePacketReadModel, error)
	DocumentationEvidencePacketFreshness(context.Context, string, string) (querycontract.DocumentationEvidencePacketFreshnessReadModel, error)
	DocumentationEvidencePacketWithFilter(context.Context, querycontract.DocumentationEvidencePacketFilter) (querycontract.DocumentationEvidencePacketReadModel, error)
	DocumentationEvidencePacketFreshnessWithFilter(context.Context, querycontract.DocumentationEvidencePacketFreshnessFilter) (querycontract.DocumentationEvidencePacketFreshnessReadModel, error)
	CountDocumentationFindings(context.Context, DocumentationFindingAggregateFilter) (DocumentationFindingAggregateCount, error)
	DocumentationFindingInventory(
		context.Context,
		DocumentationFindingAggregateFilter,
		DocumentationFindingInventoryDimension,
		int,
		int,
	) ([]DocumentationFindingInventoryRow, error)
}

// The mirror assertion keeps the documentation-scoped re-declaration
// honest, and it works in one direction only: removal, rename, or
// re-signing of a mirrored method on any staying handler-facing port
// fails compilation here. A new method on those ports does not widen
// this pin — adoption takes deliberate re-declaration plus a
// frozen-list update. It pins no shared port — the assertion names
// the read surface only as the shapes this pin mirrors, and no other
// family may cite this pin as coverage.
var _ DocumentationReadAdmission = interface {
	documentationReadModelStore
	DocumentationFindingAggregateStore
}(nil)
