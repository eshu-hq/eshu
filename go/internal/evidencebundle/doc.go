// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package evidencebundle composes and validates deterministic, share-safe
// evidence_bundle.v1 artifacts.
//
// A bundle is not a graph export or backup format. It packages bounded answer
// packet summaries, investigation packet summaries, capability catalog handles,
// freshness/readiness state, missing evidence, and reproduce calls into one
// redacted snapshot that can be attached to support or issue workflows.
package evidencebundle
