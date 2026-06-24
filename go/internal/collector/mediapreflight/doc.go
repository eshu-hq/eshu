// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package mediapreflight classifies media documentation sources before any
// transcript extractor reads audio samples, video frames, subtitles, or text.
//
// The package inspects bounded media container metadata for resource limits,
// malformed media, unsupported codecs, no-audio markers, external-reference
// markers, sensitive-looking markers, and metadata redaction signals. It
// returns metadata-only counts and warning classes; callers remain responsible
// for deciding whether a later reviewed transcription extractor may emit
// documentation facts. It does not emit facts, persist rows, call providers,
// run transcription, write graph state, or expose runtime/API/MCP behavior.
package mediapreflight
