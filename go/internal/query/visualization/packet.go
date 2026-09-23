// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualization

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	contractviz "github.com/eshu-hq/eshu/go/internal/query/querycontract/visualization"
)

// The visualization-packet builder implementation lives in
// querycontract/visualization (#6060, moved out of querycontract by #6597), so
// this leaf (#6642 Part A) can build a Packet
// without importing the query root, which it cannot do without an import
// cycle through root's compatibility aliases in visualization_alias.go. What
// follows are plain type aliases and thin function forwarders this family's
// own files (handler.go, decode.go, evidence.go, story.go) call by their
// package-local name.

const (
	// MaxNodes bounds the number of nodes a visualization packet
	// may carry.
	MaxNodes = contractviz.VisualizationMaxNodes
	// MaxEdges bounds the number of edges a visualization packet
	// may carry.
	MaxEdges = contractviz.VisualizationMaxEdges
)

// View names the derived-view family a packet was built from.
type View = contractviz.VisualizationView

const (
	// ViewServiceStory is the service-story dossier subgraph.
	ViewServiceStory = contractviz.VisualizationViewServiceStory
	// ViewEvidenceCitation is the evidence-citation subgraph.
	ViewEvidenceCitation = contractviz.VisualizationViewEvidenceCitation
	// ViewIncidentContext is the incident-context subgraph.
	ViewIncidentContext = contractviz.VisualizationViewIncidentContext
	// ViewGraphQuery is the executed-Cypher-result subgraph.
	ViewGraphQuery = contractviz.VisualizationViewGraphQuery
	// ViewUnsupported marks a packet with no derivable subgraph.
	ViewUnsupported = contractviz.VisualizationViewUnsupported
)

// Node is one bounded node in a visualization packet.
type Node = contractviz.VisualizationNode

// Edge is one bounded edge in a visualization packet.
type Edge = contractviz.VisualizationEdge

// Limits states a packet's payload bounds and retained counts.
type Limits = contractviz.VisualizationLimits

// Truncation records what a packet dropped to stay within bounds.
type Truncation = contractviz.VisualizationTruncation

// Packet is a compact, bounded, derived view of an existing
// story, evidence-citation, or incident-context query response.
type Packet = contractviz.VisualizationPacket

// visualizationBuilder accumulates nodes and edges before a packet is
// finalized. See contractviz.VisualizationBuilder for the full contract.
type visualizationBuilder = contractviz.VisualizationBuilder

func newVisualizationBuilder(view View, title string) *visualizationBuilder {
	return contractviz.NewVisualizationBuilder(view, title)
}

// unsupportedVisualizationPacket returns an explicit unsupported packet.
func unsupportedVisualizationPacket(
	view View,
	truth *querycontract.TruthEnvelope,
	limitations []string,
	nextCalls []map[string]any,
) Packet {
	return contractviz.UnsupportedVisualizationPacket(view, truth, limitations, nextCalls)
}

// visualizationNodeID derives a stable, opaque node ID.
func visualizationNodeID(kind string, parts ...string) string {
	return contractviz.VisualizationNodeID(kind, parts...)
}
