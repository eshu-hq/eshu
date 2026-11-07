// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package packet holds the investigation evidence packet contract: the
// envelope every investigation family builds, the builders that fill it, and
// the render, validate, and identity helpers that read it.
//
// InvestigationEvidencePacket is the served shape behind the CLI
// investigation commands and the API packet routes. BuildDriftPacket,
// BuildSupplyChainImpactPacket, and the deployable-unit builder construct one
// packet per family; NewInvestigationEvidencePacket validates the input and
// stamps identity, bounds, and refusal state. RenderInvestigationPacket and
// ParseInvestigationPacketFormat present a packet as JSON, Markdown, or HTML.
// NewInvestigationEvidencePacket runs the internal contract gates (bounds,
// citations, refusal consistency) as it stamps each packet. The readiness
// cluster (readiness_*.go plus source_state.go)
// classifies source freshness and coverage for supply-chain packets.
//
// The HTTP responders stay in the parent query package
// (investigation_packet_api.go, investigation_packet_api_drift.go): this leaf
// never touches net/http. The package imports the parent querycontract for
// the truth and freshness vocabulary and the sibling answer and evidence
// leaves for packet answers and citation handles; neither sibling imports it
// back. Importers are the parent query package and the CLI investigation,
// support, and comparity packages.
//
// Keep behavior identical: the packet bytes are a compatibility surface
// pinned by OpenAPI and CLI golden tests. Add a field or a family only with
// its render, validate, and golden coverage in the same change.
package packet
