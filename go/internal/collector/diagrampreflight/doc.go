// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package diagrampreflight classifies diagram documentation sources before any
// diagram extractor reads labels, links, or diagram text.
//
// The package inspects bounded SVG/XML, JSON, and text-diagram input for
// resource limits, malformed structured input, external references, include
// directives, active content, and sensitive-looking markers. It returns
// metadata-only counts and warning classes; callers remain responsible for
// deciding whether a later reviewed extractor may emit documentation facts. It
// does not emit facts, persist rows, call providers, write graph state, or
// expose runtime/API/MCP behavior.
package diagrampreflight
