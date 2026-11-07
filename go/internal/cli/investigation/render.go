// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

// PacketFormat is a supported render format for an investigation evidence
// packet artifact.
type PacketFormat = packet.InvestigationPacketFormat

// Supported packet render formats, re-exported so the cobra wrapper renders
// without importing the packet contract directly.
const (
	PacketFormatJSON     = packet.InvestigationPacketFormatJSON
	PacketFormatMarkdown = packet.InvestigationPacketFormatMarkdown
	PacketFormatHTML     = packet.InvestigationPacketFormatHTML
)

// ParsePacketFormat normalizes a requested render format. An empty value
// defaults to JSON, the canonical machine-readable form.
func ParsePacketFormat(raw string) (PacketFormat, error) {
	//nolint:wrapcheck // the packet contract's own message is what the operator needs
	return packet.ParseInvestigationPacketFormat(raw)
}

// RenderPacket renders a packet in the requested format. JSON is the
// canonical form; markdown and HTML are deterministic human-readable views
// over the same data.
func RenderPacket(body packet.InvestigationEvidencePacket, format PacketFormat) ([]byte, error) {
	//nolint:wrapcheck // the packet contract's own message is what the operator needs
	return packet.RenderInvestigationPacket(body, format)
}
