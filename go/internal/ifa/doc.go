// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ifa defines the contract-layer skeleton for Ifá conformance cases.
//
// P0 is intentionally narrow: an Odù consumes facts.Envelope inputs, optionally
// loaded through the projector FactStore.LoadFacts seam, and renders them with
// the shared replay canonicalizer. Later Ifá phases derive graph/query
// expectations and coverage; this package establishes the fact-seam input and
// comparison boundary without importing collector or parser internals.
package ifa
