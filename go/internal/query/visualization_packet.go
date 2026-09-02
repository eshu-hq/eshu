// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// The visualization-packet builder implementation moved to querycontract for
// #6060 so a handler-family subpackage -- currently internal/query/code's
// graph-query visualization route -- can build a VisualizationPacket without
// importing this package, which it cannot do without an import cycle through
// root's compatibility aliases. What follows are plain type aliases and thin
// function forwarders.
//
// A type alias carries the type but not access to its unexported fields or
// methods, so the three root files that drive the builder directly had to be
// updated. visualization_packet_evidence.go and visualization_packet_story.go
// needed only the method renames addNode/addEdge/finalize ->
// AddNode/AddEdge/Finalize. visualization_packet_graph_query.go needed more
// than renames: it read builder.nodes and builder.edges and assigned
// builder.truth directly, none of which a caller outside querycontract can do
// now, so those became the Empty, EdgeCount and SetTruth accessors.
//
// Every other root caller compiles unchanged. Where a root file in this diff
// changed for some other reason -- content_reader_entity_names.go,
// content_reader_index_readiness.go, entity_resolve_identity.go,
// evidence_citation.go, language_registry.go, repository_coverage.go -- it is
// naming the leaf package for a different promoted symbol, not touching the
// builder.

const (
	// VisualizationMaxNodes bounds the number of nodes a visualization packet
	// may carry.
	VisualizationMaxNodes = querycontract.VisualizationMaxNodes
	// VisualizationMaxEdges bounds the number of edges a visualization packet
	// may carry.
	VisualizationMaxEdges = querycontract.VisualizationMaxEdges
)

// VisualizationView names the derived-view family a packet was built from.
type VisualizationView = querycontract.VisualizationView

const (
	// VisualizationViewServiceStory is the service-story dossier subgraph.
	VisualizationViewServiceStory = querycontract.VisualizationViewServiceStory
	// VisualizationViewEvidenceCitation is the evidence-citation subgraph.
	VisualizationViewEvidenceCitation = querycontract.VisualizationViewEvidenceCitation
	// VisualizationViewIncidentContext is the incident-context subgraph.
	VisualizationViewIncidentContext = querycontract.VisualizationViewIncidentContext
	// VisualizationViewGraphQuery is the executed-Cypher-result subgraph.
	VisualizationViewGraphQuery = querycontract.VisualizationViewGraphQuery
	// VisualizationViewUnsupported marks a packet with no derivable subgraph.
	VisualizationViewUnsupported = querycontract.VisualizationViewUnsupported
)

// VisualizationNode is one bounded node in a visualization packet.
type VisualizationNode = querycontract.VisualizationNode

// VisualizationEdge is one bounded edge in a visualization packet.
type VisualizationEdge = querycontract.VisualizationEdge

// VisualizationLimits states a packet's payload bounds and retained counts.
type VisualizationLimits = querycontract.VisualizationLimits

// VisualizationTruncation records what a packet dropped to stay within bounds.
type VisualizationTruncation = querycontract.VisualizationTruncation

// VisualizationPacket is a compact, bounded, derived view of an existing
// story, evidence-citation, or incident-context query response.
type VisualizationPacket = querycontract.VisualizationPacket

// visualizationBuilder accumulates nodes and edges before a packet is
// finalized. See querycontract.VisualizationBuilder for the full contract.
type visualizationBuilder = querycontract.VisualizationBuilder

func newVisualizationBuilder(view VisualizationView, title string) *visualizationBuilder {
	return querycontract.NewVisualizationBuilder(view, title)
}

// unsupportedVisualizationPacket returns an explicit unsupported packet.
func unsupportedVisualizationPacket(
	view VisualizationView,
	truth *TruthEnvelope,
	limitations []string,
	nextCalls []map[string]any,
) VisualizationPacket {
	return querycontract.UnsupportedVisualizationPacket(view, truth, limitations, nextCalls)
}

// visualizationNodeID derives a stable, opaque node ID.
func visualizationNodeID(kind string, parts ...string) string {
	return querycontract.VisualizationNodeID(kind, parts...)
}

// visualizationEdgeID derives a stable edge ID from its endpoints and label.
func visualizationEdgeID(source, target, relationship string) string {
	return querycontract.VisualizationEdgeID(source, target, relationship)
}
