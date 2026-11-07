// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation

import (
	"github.com/eshu-hq/eshu/go/internal/query"
)

// PacketFormat is a supported render format for an investigation evidence
// packet artifact.
type PacketFormat = query.InvestigationPacketFormat

// Supported packet render formats, re-exported so the cobra wrapper renders
// without importing the packet contract directly.
const (
	// PacketFormatJSON renders the canonical machine-readable packet.
	PacketFormatJSON = query.InvestigationPacketFormatJSON
	// PacketFormatMarkdown renders a human-readable summary.
	PacketFormatMarkdown = query.InvestigationPacketFormatMarkdown
	// PacketFormatHTML renders a self-contained HTML summary.
	PacketFormatHTML = query.InvestigationPacketFormatHTML
)

// ParsePacketFormat normalizes a requested render format. An empty value
// defaults to JSON, the canonical machine-readable form.
func ParsePacketFormat(raw string) (PacketFormat, error) {
	//nolint:wrapcheck // the packet contract's own message is what the operator needs
	return query.ParseInvestigationPacketFormat(raw)
}

// RenderPacket renders a packet in the requested format. JSON is the
// canonical form; markdown and HTML are deterministic human-readable views
// over the same data.
func RenderPacket(packet query.InvestigationEvidencePacket, format PacketFormat) ([]byte, error) {
	//nolint:wrapcheck // the packet contract's own message is what the operator needs
	return query.RenderInvestigationPacket(packet, format)
}
