// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codeprovenance holds the closed resolution-provenance vocabulary for
// code relationship edges defined by ADR #2222.
//
// The package is the single source of truth for how a CALLS, REFERENCES,
// INHERITS, IMPLEMENTS, OVERRIDES, ALIASES, INSTANTIATES, or USES_METACLASS
// edge's target entity was resolved (Method) and for the confidence and reason
// derived from that method. The reducer emits a Method on each materialization
// row; the graph edge writer derives confidence and reason from it. Keeping the
// vocabulary, the confidence tier table, and the reason table in one leaf
// package prevents the tiers from drifting between the reducer, the storage
// layer, and the query surface.
//
// Provenance is descriptive, not admissive: a Method records the resolver
// branch that produced an already-admitted edge. It never gates edge creation,
// changes which edges are written, or promotes a heuristic score to canonical
// truth. The vocabulary is closed; adding a resolver branch requires extending
// the Method constants, the confidence table, and the accuracy goldens (#2226)
// together.
package codeprovenance
