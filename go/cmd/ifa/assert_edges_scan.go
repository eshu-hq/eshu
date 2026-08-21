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
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

type materializedEdgeScan struct {
	counts       map[string]int
	endpointErrs []string
	identityErrs []string
	propertyErrs []string
}

func scanMaterializedEdges(
	ctx context.Context,
	reader graphdump.Reader,
	edgeTypes map[string]struct{},
	endpoints map[string]cypher.MaterializedEdgeEndpoint,
	identity map[string][]string,
	expectedPropertyKeys map[string][]string,
	labels map[string]string,
) (materializedEdgeScan, error) {
	scan := materializedEdgeScan{counts: make(map[string]int)}
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

func (s *materializedEdgeScan) recordEndpointDefect(edge graphdump.Edge, fromUID, toUID string) {
	missing := "source and target"
	switch {
	case fromUID == "" && toUID != "":
		missing = "source"
	case toUID == "" && fromUID != "":
		missing = "target"
	}
	s.endpointErrs = append(s.endpointErrs, fmt.Sprintf(
		"%s edge whose %s endpoint carries neither uid, id, nor (for a CodeownerTeam endpoint) ref (from=%q to=%q) — an unmaterialized endpoint node",
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
