// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualization

import (
	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/evidence"
)

// VisualizationReadAdmissionContractVersion versions the #9669 G2
// admission-contract shape for the visualization derive read (POST
// /api/v0/visualizations/derive). Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and calls
// this versioned port, which the core-owned static derive satisfies. A
// breaking change to the read shape fails the frozen method-set test until
// that list is deliberately updated; bump this version alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
const VisualizationReadAdmissionContractVersion = "v0"

// VisualizationReadAdmission is the versioned admission-contract port the API
// handler will depend on instead of calling the static packet builders
// directly. It re-declares exactly the three reads the visualization derive
// route admits through — one static packet builder per derivable view
// (service story, evidence citation, incident context) — so the boundary is
// held honest by the compile-time adapter assertion below plus the frozen
// method-set contract test; it will back the eshu-api extraction handoff for
// this family. The seam is read-only on purpose: every declared method is a
// bounded pure derivation over the caller-supplied source response and the
// port carries no mutation surface, so a dest consumer implementing only
// this contract can serve visualization derives but cannot acquire a write
// it did not bargain for through it. This pin lives in package
// visualization because the Packet and View types are package-local aliases.
// The empty-struct handler takes no store; the pin freezes the static
// derive the dispatch issues so the dest side knows exactly which pure
// functions back the route.
//
// G2-wave consumer: the G2 satellite leaf terminating this family's #9669
// MUST-TERMINATE rows. The satellite records this pin in a follow-up; a
// satellite PR consuming an unminted pin is invalid. Arbiter review of this
// pin precedes satellite termination of the visualization leg.
//
// In scope: the visualization derive read. The route admits its derive
// through exactly this pin:
//
//   - POST /api/v0/visualizations/derive
//
// Narrow by construction, recorded not silently assumed: the three methods
// re-declare the staying static signatures whole in this
// visualization-scoped shape instead of embedding a shared port or aliasing
// a wider builder surface, so no other family can cite this pin — a blanket
// or shared-port pin is forbidden, and the widened shape needs arbiter
// review before satellite consumption. This pin adopts only the reads named
// above.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own standing
// until a deliberate pin covers it):
//
//   - The ViewGraphQuery executed-Cypher-result view stays unpinned here by
//     construction: the derive dispatch never issues it (the switch covers
//     service story, evidence citation, and incident context only), so a
//     graph-results leg cannot ride this pin.
//   - The BuildEvidenceCitationPacketFromMap and
//     BuildIncidentContextPacketFromMap transport adapters stay unpinned
//     here: the dispatch decodes through its own unexported path, so the
//     seeded-drift test plants exactly one FromMap neighbor to prove the
//     guard sees it.
//   - The JSON decode, the view allow-list error, and the truth derivation
//     are handler behavior, not port shape: there is no method set to
//     freeze.
//   - No constructor, selector, or store surface: the satellite satisfies
//     this port with its own wiring; this pin carries the derive shape
//     only.
//
// Rollback: revert this pin plus the handler Reads field and leg
// routing. The derive dispatch admits through the pin with a static
// default when unset — rollback restores the direct static builders
// and moves no traffic beyond the handler seam.
type VisualizationReadAdmission interface {
	BuildServiceStoryPacket(map[string]any, *querycontract.TruthEnvelope) Packet
	BuildEvidenceCitationPacket(evidence.EvidenceCitationResponse, *querycontract.TruthEnvelope) Packet
	BuildIncidentContextPacket(model.IncidentContextResponse, *querycontract.TruthEnvelope) Packet
}

// visualizationStaticDeriver is the minimal static satisfier behind the pin:
// it delegates each re-declared read to the static packet builder the
// derive dispatch calls today. The delegation keeps the pin honest in one
// direction: removal, rename, or re-signing of any of the three builders
// fails compilation here. A new builder in this package does not widen this
// pin — adoption takes deliberate re-declaration plus a frozen-list update.
// It pins no shared port — the delegation names only the three builders
// this pin mirrors, and no other family may cite this pin as coverage.
type visualizationStaticDeriver struct{}

func (visualizationStaticDeriver) BuildServiceStoryPacket(response map[string]any, truth *querycontract.TruthEnvelope) Packet {
	return BuildServiceStoryPacket(response, truth)
}

func (visualizationStaticDeriver) BuildEvidenceCitationPacket(response evidence.EvidenceCitationResponse, truth *querycontract.TruthEnvelope) Packet {
	return BuildEvidenceCitationPacket(response, truth)
}

func (visualizationStaticDeriver) BuildIncidentContextPacket(response model.IncidentContextResponse, truth *querycontract.TruthEnvelope) Packet {
	return BuildIncidentContextPacket(response, truth)
}

var _ VisualizationReadAdmission = visualizationStaticDeriver{}
