// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package imagepreflight classifies image documentation sources before any OCR
// extractor reads pixels, text, or image metadata.
//
// The package inspects bounded image container metadata for resource limits,
// malformed media, unsupported codecs, animated GIF first-frame handling,
// external-reference markers, sensitive-looking markers, and metadata
// redaction signals. It returns metadata-only counts and warning classes;
// callers remain responsible for deciding whether a later reviewed OCR
// extractor may emit documentation facts. It does not emit facts, persist rows,
// call providers, run OCR, write graph state, or expose runtime/API/MCP
// behavior.
package imagepreflight
