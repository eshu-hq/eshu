// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package semanticguard evaluates deterministic security gates for semantic extraction.
//
// The package is pure model logic: callers pass an already-evaluated provider
// policy decision, ACL state, extractor state, content classifications,
// redaction evidence, prompt-safety evidence, and retention posture. It returns
// an audit-safe decision and never loads credentials, constructs prompts,
// opens storage, emits telemetry, or calls providers.
package semanticguard
