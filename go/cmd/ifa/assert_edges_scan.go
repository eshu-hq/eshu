// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
	materialized "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/materialized"
)

type materializedEdgeScan struct {
	counts       map[string]int
	endpointErrs []string
	identityErrs []string
	propertyErrs []string
	// pairs counts, per endpoint pair of a OneEdgePerEndpointPair type, every
	// label-matching edge regardless of evidence_source or identity, so a
	// duplicate the provenance filter would skip still surfaces (#6671).
	pairs map[string]*endpointPairCount
}

// endpointPairCount is one shared-identity endpoint pair's stamp-blind
// multiplicity plus the evidence_source values seen on it, for the report.
type endpointPairCount struct {
	label   string
	count   int
	sources []string
}

func scanMaterializedEdges(
	ctx context.Context,
	reader graphdump.Reader,
	edgeTypes map[string]struct{},
	endpoints map[string]materialized.MaterializedEdgeEndpoint,
	identity map[string][]string,
	expectedPropertyKeys map[string][]string,
	labels map[string]string,
) (materializedEdgeScan, error) {
	scan := materializedEdgeScan{counts: make(map[string]int), pairs: make(map[string]*endpointPairCount)}
	err := reader.StreamEdges(ctx, func(edge graphdump.Edge) error {
		if _, ok := edgeTypes[edge.Type]; !ok {
			return nil
		}
		// A constrained type must match its endpoint labels too. Only families
		// with a proven type collision carry constraints, and the cypher-side
		// guard requires them to be total over the family's registered types.
		if endpoint, constrained := endpoints[edge.Type]; constrained {
			if !hasLabel(edge.FromLabels, endpoint.FromLabel) || !hasLabel(edge.ToLabels, endpoint.ToLabel) {
				return nil
			}
			// Counted before the provenance filter below, which would otherwise
			// skip the unstamped or other-writer copy of a shared edge.
			if endpoint.OneEdgePerEndpointPair {
				scan.countEndpointPair(edge)
			}
			// Provenance distinguishes live writers that share a type and labels.
			if endpoint.EvidenceSource != "" {
				if got, _ := edge.Props["evidence_source"].(string); got != endpoint.EvidenceSource {
					return nil
				}
			}
		}
		fromUID := endpointID(edge.FromProps, edge.FromLabels)
		toUID := endpointID(edge.ToProps, edge.ToLabels)
		if fromUID == "" || toUID == "" {
			scan.recordEndpointDefect(edge, fromUID, toUID)
			return nil
		}
		liveEdge := materializededges.ExpectedEdge{RelationshipType: edge.Type, SourceEntityID: fromUID, TargetEntityID: toUID}
		if declared := identity[edge.Type]; len(declared) > 0 {
			props, badProps := readExpectedProperties(edge.Props, declared)
			if len(badProps) > 0 {
				scan.identityErrs = append(scan.identityErrs, fmt.Sprintf(
					"%s edge (from=%q to=%q) has missing or non-string declared identity properties %v — an unmaterialized identity property",
					edge.Type, fromUID, toUID, badProps,
				))
				return nil
			}
			liveEdge.Identity = props
		}
		key := liveEdge.Key()
		if propertyKeys := expectedPropertyKeys[key]; len(propertyKeys) > 0 {
			properties, badProps := readExpectedProperties(edge.Props, propertyKeys)
			if len(badProps) > 0 {
				scan.propertyErrs = append(scan.propertyErrs, fmt.Sprintf(
					"%s edge (from=%q to=%q) has missing, non-string, or blank asserted properties %v",
					edge.Type, fromUID, toUID, badProps,
				))
				return nil
			}
			liveEdge.Properties = properties
			key = liveEdge.Key()
		}
		scan.counts[key]++
		labels[key] = expectedEdgeLabel(liveEdge)
		return nil
	})
	return scan, err
}

// countEndpointPair adds one edge to its endpoint pair's stamp-blind count.
// An edge with an unidentified endpoint is not counted here; the owned-edge
// path reports it as an endpoint defect.
func (s *materializedEdgeScan) countEndpointPair(edge graphdump.Edge) {
	fromID := endpointID(edge.FromProps, edge.FromLabels)
	toID := endpointID(edge.ToProps, edge.ToLabels)
	if fromID == "" || toID == "" {
		return
	}
	label := fmt.Sprintf("%s|%s|%s", edge.Type, fromID, toID)
	pair := s.pairs[label]
	if pair == nil {
		pair = &endpointPairCount{label: label}
		s.pairs[label] = pair
	}
	pair.count++
	source, _ := edge.Props["evidence_source"].(string)
	if source == "" {
		source = "<unset>"
	}
	pair.sources = append(pair.sources, source)
}

// sharedIdentityDuplicates reports every OneEdgePerEndpointPair pair holding
// more than one edge, with the stamps seen, in sorted order.
func (s *materializedEdgeScan) sharedIdentityDuplicates() []string {
	var out []string
	for _, pair := range s.pairs {
		if pair.count <= 1 {
			continue
		}
		sources := append([]string(nil), pair.sources...)
		sort.Strings(sources)
		out = append(out, fmt.Sprintf("%s (graph=%d, want 1; evidence_source values %v)", pair.label, pair.count, sources))
	}
	sort.Strings(out)
	return out
}

func (s *materializedEdgeScan) recordEndpointDefect(edge graphdump.Edge, fromUID, toUID string) {
	missing := "source and target"
	switch {
	case fromUID == "" && toUID != "":
		missing = "source"
	case toUID == "" && fromUID != "":
		missing = "target"
	}
	s.endpointErrs = append(s.endpointErrs, fmt.Sprintf(
		"%s edge whose %s endpoint carries none of uid, id, a CodeownerTeam endpoint's ref, or an Environment endpoint's name (from=%q to=%q) — an unmaterialized endpoint node",
		edge.Type, missing, fromUID, toUID,
	))
}

func readExpectedProperties(edgeProps map[string]any, keys []string) (map[string]string, []string) {
	properties := make(map[string]string, len(keys))
	var bad []string
	for _, key := range keys {
		value, ok := edgeProps[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			bad = append(bad, key)
			continue
		}
		properties[key] = value
	}
	sort.Strings(bad)
	return properties, bad
}
