// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"encoding/json"
	"slices"
)

// supportedProtocolVersions lists the MCP protocol revisions this server
// implements, oldest first. The last entry is the server's latest and is the
// answer for a request that names no supported version.
//
// The list holds only 2024-11-05. Later revisions changed the wire contract in
// ways this server does not implement: 2025-03-26 replaced the HTTP+SSE
// transport with Streamable HTTP and added JSON-RPC batching, and 2025-06-18
// removed batching and requires an MCP-Protocol-Version header on HTTP
// requests. This server serves HTTP+SSE, decodes one JSON-RPC object per POST,
// and reads no version header. The extra structuredContent field on tool
// results is additive and does not make the server a 2025-06-18 server.
// Add a version here only with the transport behavior that revision requires.
var supportedProtocolVersions = []string{"2024-11-05"}

// negotiateProtocolVersion answers the initialize version negotiation (MCP
// lifecycle, "Version Negotiation"): a supported requested version is echoed
// back unchanged, and any other request, including an empty one, gets the
// latest version in supported. The match is exact.
func negotiateProtocolVersion(requested string, supported []string) string {
	if slices.Contains(supported, requested) {
		return requested
	}
	return supported[len(supported)-1]
}

// requestedProtocolVersion reads params.protocolVersion from an initialize
// request. A missing, malformed, or non-string value reads as empty, so the
// caller negotiates the server's own version instead of rejecting the
// handshake.
func requestedProtocolVersion(params json.RawMessage) string {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ""
	}
	return p.ProtocolVersion
}

// maxLoggedVersionBytes bounds a client-supplied version string in a log line.
const maxLoggedVersionBytes = 64

// truncateForLog bounds a client-supplied string before it reaches a log line,
// so a hostile initialize cannot write an unbounded value.
func truncateForLog(s string) string {
	if len(s) <= maxLoggedVersionBytes {
		return s
	}
	return s[:maxLoggedVersionBytes] + "..."
}
