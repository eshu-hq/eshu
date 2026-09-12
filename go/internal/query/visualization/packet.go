// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualization

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// The visualization-packet builder implementation lives in querycontract
// (#6060), so this leaf (#6642 Part A) can build a Packet
// without importing the query root, which it cannot do without an import
// cycle through root's compatibility aliases in visualization_alias.go. What
// follows are plain type aliases and thin function forwarders this family's
// own files (handler.go, decode.go, evidence.go, story.go) call by their
// package-local name.

const (
	// MaxNodes bounds the number of nodes a visualization packet
	// may carry.
	MaxNodes = querycontract.VisualizationMaxNodes
	// MaxEdges bounds the number of edges a visualization packet
	// may carry.
	MaxEdges = querycontract.VisualizationMaxEdges
)

// View names the derived-view family a packet was built from.
type View = querycontract.VisualizationView

const (
	// ViewServiceStory is the service-story dossier subgraph.
	ViewServiceStory = querycontract.VisualizationViewServiceStory
	// ViewEvidenceCitation is the evidence-citation subgraph.
	ViewEvidenceCitation = querycontract.VisualizationViewEvidenceCitation
	// ViewIncidentContext is the incident-context subgraph.
	ViewIncidentContext = querycontract.VisualizationViewIncidentContext
	// ViewGraphQuery is the executed-Cypher-result subgraph.
	ViewGraphQuery = querycontract.VisualizationViewGraphQuery
	// ViewUnsupported marks a packet with no derivable subgraph.
	ViewUnsupported = querycontract.VisualizationViewUnsupported
)

// Node is one bounded node in a visualization packet.
type Node = querycontract.VisualizationNode

// Edge is one bounded edge in a visualization packet.
type Edge = querycontract.VisualizationEdge

// Limits states a packet's payload bounds and retained counts.
type Limits = querycontract.VisualizationLimits

// Truncation records what a packet dropped to stay within bounds.
type Truncation = querycontract.VisualizationTruncation

// Packet is a compact, bounded, derived view of an existing
// story, evidence-citation, or incident-context query response.
type Packet = querycontract.VisualizationPacket

// visualizationBuilder accumulates nodes and edges before a packet is
// finalized. See querycontract.VisualizationBuilder for the full contract.
type visualizationBuilder = querycontract.VisualizationBuilder

func newVisualizationBuilder(view View, title string) *visualizationBuilder {
	return querycontract.NewVisualizationBuilder(view, title)
}

// unsupportedVisualizationPacket returns an explicit unsupported packet.
func unsupportedVisualizationPacket(
	view View,
	truth *querycontract.TruthEnvelope,
	limitations []string,
	nextCalls []map[string]any,
) Packet {
	return querycontract.UnsupportedVisualizationPacket(view, truth, limitations, nextCalls)
}

// visualizationNodeID derives a stable, opaque node ID.
func visualizationNodeID(kind string, parts ...string) string {
	return querycontract.VisualizationNodeID(kind, parts...)
}
