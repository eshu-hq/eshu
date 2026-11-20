// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package serviceintelhttp

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/entity"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/service"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

// ServiceIntelHTTPReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the service intelligence report read
// (GET /api/v0/services/{service_name}/intelligence-report).
// Canonical reads persist through explicit core-owned admission
// contracts: eshu-api keeps the HTTP shape and calls this versioned
// port, which the core-owned entity handler, incident and supply-chain
// evidence sources, and static composer satisfy jointly, composed by
// the consumer. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const ServiceIntelHTTPReadAdmissionContractVersion = "v0"

// ServiceIntelHTTPReadAdmission is the versioned admission-contract
// port the API handler will depend on instead of the four direct
// handles. It declares exactly the four reads the intelligence-report
// route admits through — the service-story envelope build (mirroring
// the entity Handler seam as exercised through the dossier leg), the
// incident evidence read (mirroring IncidentEvidenceSource as
// exercised through the incidents_support section), the supply-chain
// inventory read (mirroring SupplyChainEvidenceSource as exercised
// through the supply_chain section), and the report compose
// (mirroring serviceintel.Compose as exercised through the compose
// leg) — so the boundary is held honest by the compile-time adapter
// assertion below plus the frozen method-set contract test. It will
// back the eshu-api extraction handoff for this read. The seam is
// read-only on purpose: every declared method is a bounded read or
// pure derivation and the port carries no mutation surface, so a dest
// consumer implementing only this contract can serve the
// intelligence-report read but cannot acquire a write it did not
// bargain for through it. This pin lives in package serviceintelhttp
// because the handler lives outside internal/query; the ruling's
// no-new-tree rule is met — the pin sits beside the handler it
// covers, the same placement the query-tree pins use.
//
// W-wave consumer: W5 service intelligence (the single oracle
// ServiceIntelHTTP row).
//
// Narrow by construction, recorded not silently assumed: the methods
// re-declare the source signatures in this
// service-intelligence-scoped shape instead of embedding the entity
// Handler or the source interfaces. This pin therefore grants nothing
// to the other service-story consumers — the story, context, and
// downstream legs keep their own entity-pin standing until deliberate
// pins cover them.
type ServiceIntelHTTPReadAdmission interface {
	BuildServiceStoryEnvelope(context.Context, service.WorkloadSelector, string) (map[string]any, *querycontract.TruthEnvelope, int, *querycontract.ErrorEnvelope)
	IncidentRecordsForWorkload(context.Context, string) ([]serviceintel.IncidentRecord, error)
	SupplyChainInventoryForWorkload(context.Context, string) (map[string]any, error)
	ComposeServiceIntelReport(serviceintel.ReportInput) serviceintel.Report
}

// serviceStoryEnvelopeBuilder names the entity seam the report leg
// builds its dossier through. The entity Handler satisfies it; a seam
// change that renames or re-signs the builder fails compilation
// below.
type serviceStoryEnvelopeBuilder interface {
	BuildServiceStoryEnvelope(context.Context, service.WorkloadSelector, string) (map[string]any, *querycontract.TruthEnvelope, int, *querycontract.ErrorEnvelope)
}

var _ serviceStoryEnvelopeBuilder = (*entity.Handler)(nil)

// serviceIntelHTTPDeriver is the minimal production-shaped satisfier
// behind the pin: it composes the entity builder, the two evidence
// sources, and the static composer exactly the way the handler leg
// wires them. The delegation keeps the pin honest in one direction:
// removal, rename, or re-signing of any of the four functions fails
// compilation here. A new source in this package does not widen this
// pin — adoption takes deliberate re-declaration plus a frozen-list
// update. It pins no shared port — the delegation names only the four
// reads this pin mirrors, and no other family may cite this pin as
// coverage.
type serviceIntelHTTPDeriver struct {
	Stories     serviceStoryEnvelopeBuilder
	Incidents   IncidentEvidenceSource
	SupplyChain SupplyChainEvidenceSource
}

func (d serviceIntelHTTPDeriver) BuildServiceStoryEnvelope(ctx context.Context, selector service.WorkloadSelector, operation string) (map[string]any, *querycontract.TruthEnvelope, int, *querycontract.ErrorEnvelope) {
	return d.Stories.BuildServiceStoryEnvelope(ctx, selector, operation)
}

func (d serviceIntelHTTPDeriver) IncidentRecordsForWorkload(ctx context.Context, workloadID string) ([]serviceintel.IncidentRecord, error) {
	records, err := d.Incidents.IncidentRecordsForWorkload(ctx, workloadID)
	if err != nil {
		return nil, fmt.Errorf("service intelligence incident records: %w", err)
	}
	return records, nil
}

func (d serviceIntelHTTPDeriver) SupplyChainInventoryForWorkload(ctx context.Context, workloadID string) (map[string]any, error) {
	inventory, err := d.SupplyChain.SupplyChainInventoryForWorkload(ctx, workloadID)
	if err != nil {
		return nil, fmt.Errorf("service intelligence supply-chain inventory: %w", err)
	}
	return inventory, nil
}

func (d serviceIntelHTTPDeriver) ComposeServiceIntelReport(in serviceintel.ReportInput) serviceintel.Report {
	return serviceintel.Compose(in)
}

var _ ServiceIntelHTTPReadAdmission = serviceIntelHTTPDeriver{}
