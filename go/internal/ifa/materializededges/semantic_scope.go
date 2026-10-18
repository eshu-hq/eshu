// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"strings"

	materialized "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/materialized"
)

// LoadSemanticEntityContainmentExpectedEdges loads the semantic_entity_containment
// expected-edge fixture and returns the fixture's single owning repository
// alongside its edges, for `ifa assert-edges -domain semantic_entity_containment`.
//
// It is the rationale-pattern scope loader for this family
// (LoadRationaleExpectedEdgeRecords is the original): every edge must declare
// the same non-blank repo_id, and the live reader scopes its scan to that
// repo. A fixture that stops declaring its repo -- or declares two -- fails
// here, before any graph connection opens, never as a silently unscoped
// (graph-wide) or silently empty assert. The repo value therefore always
// comes fail-closed from the fixture, never from a literal in the assert.
func LoadSemanticEntityContainmentExpectedEdges(path string) (string, []ExpectedEdge, error) {
	edges, err := loadSQLRelationshipExpectedEdges(path)
	if err != nil {
		return "", nil, err
	}
	if len(edges) == 0 {
		return "", nil, fmt.Errorf("ifa: semantic expected edges %s declares no edges; an empty expected set would make the gate vacuous", path)
	}
	identity, err := materialized.MaterializedEdgeIdentityProperties("semantic_entity_containment")
	if err != nil {
		return "", nil, fmt.Errorf("ifa: load semantic expected edges: %w", err)
	}
	repoID := strings.TrimSpace(edges[0].RepoID)
	if repoID == "" {
		return "", nil, fmt.Errorf("ifa: semantic expected edges %s edge 0 has a blank repo_id", path)
	}
	out := make([]ExpectedEdge, len(edges))
	for i, e := range edges {
		if strings.TrimSpace(e.RelationshipType) == "" || strings.TrimSpace(e.SourceEntityID) == "" || strings.TrimSpace(e.TargetEntityID) == "" {
			return "", nil, fmt.Errorf("ifa: semantic expected edge %d in %s has an empty relationship_type, source_entity_id, or target_entity_id", i, path)
		}
		if e.RelationshipType != "CONTAINS" {
			return "", nil, fmt.Errorf("ifa: semantic expected edge %d in %s: relationship_type=%q, want CONTAINS", i, path, e.RelationshipType)
		}
		if e.RepoID != repoID {
			return "", nil, fmt.Errorf("ifa: semantic expected edge %d in %s: repo_id=%q, want the single fixture repository %q", i, path, e.RepoID, repoID)
		}
		// Same identity/property validation LoadExpectedEdges applies:
		// this family's writer MERGEs on endpoints alone, so any Identity
		// entry -- or a blank/overlapping asserted property -- is a
		// fixture error, not a silently-accepted edge.
		if err := validateExpectedEdgeIdentity(e.Identity, identity[e.RelationshipType]); err != nil {
			return "", nil, fmt.Errorf("ifa: semantic expected edge %d (%s) in %s: %w", i, e.RelationshipType, path, err)
		}
		if err := validateExpectedEdgeProperties(e.Properties, e.Identity); err != nil {
			return "", nil, fmt.Errorf("ifa: semantic expected edge %d (%s) in %s: %w", i, e.RelationshipType, path, err)
		}
		out[i] = ExpectedEdge(e)
	}
	return repoID, out, nil
}
