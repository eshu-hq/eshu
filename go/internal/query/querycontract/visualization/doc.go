// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package visualization holds the visualization-packet contract: the packet,
// node, edge, limits and truncation types, VisualizationBuilder, and the stable
// node and edge ID hashers that query handlers use to shape graph views.
//
// It imports its parent querycontract for TruthEnvelope and the TruthLevel
// constants; the parent does not import it back.
package visualization
