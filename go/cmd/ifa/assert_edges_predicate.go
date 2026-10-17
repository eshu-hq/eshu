// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
	materialized "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/materialized"
)

// validatePredicateExpectedEdges fails a predicate-scoped family's fixture
// whose edges the predicate itself would never select: every expected edge
// must assert an evidence_source property, and its (type, evidence_source)
// must pair on one registered writer. Without this, a fixture typo (an
// unprefixed type, a cross-writer stamp) would surface only as a misleading
// "missing edge" against a correct graph.
func validatePredicateExpectedEdges(domain string, predicate materialized.PredicateScopedEdgeFamily, expected []materializededges.ExpectedEdge) error {
	for i, edge := range expected {
		stamp := edge.Properties["evidence_source"]
		if stamp == "" {
			return fmt.Errorf("expected edge %d (%s) for predicate-scoped domain %q asserts no evidence_source property; the predicate pairs each type prefix with its writer's stamp, so an unstamped expectation could never distinguish them", i, edge.RelationshipType, domain)
		}
		paired := false
		for _, writer := range predicate.Writers {
			if writer.EvidenceSource == stamp && strings.HasPrefix(edge.RelationshipType, writer.TypePrefix) {
				paired = true
				break
			}
		}
		if !paired {
			return fmt.Errorf("expected edge %d (%s, evidence_source=%q) matches no writer pairing of predicate-scoped domain %q; fix the fixture's type prefix or stamp", i, edge.RelationshipType, stamp, domain)
		}
	}
	return nil
}
