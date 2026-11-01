// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package media builds source-neutral documentation facts from reviewed
// local media transcript results.
//
// The package runs media metadata preflight first, then passes safe local media
// inputs to an injected transcript engine. It emits documentation document,
// section, and provenance-only entity-mention facts with transcript_chunk
// incident-media provenance; it does not infer services, deployments,
// ownership, incidents, graph edges, or claim candidates from transcript text.
// Unsafe source locations and source identity fields are redacted before
// persistence, the segments that follow a sensitive keyword in time or slice
// order are redacted as its value, redacted transcript sections do not emit
// mention facts, and sensitive-looking mention hints are dropped before
// extraction. An engine that heard no speech wraps ErrNoSpeech; Extract records
// that as a no_speech document instead of an error. Runtime enablement,
// sandboxing, telemetry wiring, codec dependencies, and security review remain
// caller-owned before any hosted extraction path is turned on.
package media
