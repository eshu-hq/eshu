// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ooxmlpreflight classifies OOXML documentation packages before any
// document extractor reads source content.
//
// The package inspects ZIP/package metadata, relationship parts, bounded
// content-type XML, and selected structure-only XML start elements for `.docx`,
// `.xlsx`, and `.pptx` candidates. It returns metadata-only safety decisions,
// structure counts, and warning classes; callers remain responsible for
// deciding whether to run a format-specific extractor. It does not emit facts,
// persist rows, call providers, write graph state, or expose runtime/API/MCP
// behavior.
package ooxmlpreflight
