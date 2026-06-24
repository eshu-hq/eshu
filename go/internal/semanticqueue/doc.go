// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package semanticqueue plans metadata-only semantic extraction queue records.
//
// The package is pure: it computes stable fingerprints, job identifiers, skip
// states, stale markers, and retry/dead-letter transitions without calling
// providers, opening databases, or retaining raw prompts and responses.
package semanticqueue
