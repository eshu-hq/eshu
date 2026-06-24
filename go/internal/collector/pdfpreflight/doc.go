// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package pdfpreflight classifies PDF documentation sources before any PDF
// extractor reads page text, links, or document metadata.
//
// The package inspects bounded PDF markers for resource limits, malformed
// structure, encryption, active content, embedded files, external references,
// annotations, metadata fields, and image-only signals. It returns
// metadata-only counts and warning classes; callers remain responsible for
// deciding whether a later reviewed extractor may emit documentation facts. It
// does not emit facts, persist rows, call providers, write graph state, or
// expose runtime/API/MCP behavior.
package pdfpreflight
