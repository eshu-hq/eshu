// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// countInheritanceFactInputs returns the number of content_entity facts loaded
// for the inheritance materialization and, of those, how many carry an
// inheritable entity type AND actually declare a parent (a base, an implemented
// interface, or a trait adaptation) — i.e. entities that can produce an edge.
// These feed the handler's completion log so an intermittent rc-12 (INHERITS)
// gate flake — which does not reproduce locally or on a single remote host
// (#3873) — can be root-caused from logs alone: a low content_entity_facts count
// points to a partial upstream fact set (ordering stall), while
// entities_with_declared_parent > 0 paired with edge_count = 0 points to declared
// parents that resolved to no in-corpus entity. Counting only parent-declaring
// entities (not every Class/Interface) avoids misclassifying a repo of
// parentless classes — a genuinely empty inheritance input — as a resolution
// failure.
func countInheritanceFactInputs(envelopes []facts.Envelope) (contentEntities, withDeclaredParent int) {
	for i := range envelopes {
		if envelopes[i].FactKind != "content_entity" {
			continue
		}
		contentEntities++
		payload := envelopes[i].Payload
		if _, ok := inheritableEntityTypes[semanticPayloadString(payload, "entity_type")]; !ok {
			continue
		}
		if len(inheritancePayloadBases(payload)) > 0 ||
			len(inheritancePayloadImplementedInterfaces(payload)) > 0 ||
			len(inheritancePayloadTraitAdaptations(payload)) > 0 {
			withDeclaredParent++
		}
	}
	return contentEntities, withDeclaredParent
}

// collectInheritanceRepoIDs returns sorted, deduplicated repository IDs from
// content entity envelopes.
func collectInheritanceRepoIDs(envelopes []facts.Envelope) []string {
	seen := make(map[string]struct{})
	repoIDs := make([]string, 0)
	for _, env := range envelopes {
		if env.FactKind != "content_entity" {
			continue
		}
		repoID := semanticPayloadString(env.Payload, "repo_id")
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		repoIDs = append(repoIDs, repoID)
	}
	sort.Strings(repoIDs)
	return repoIDs
}

// inheritancePayloadBases extracts the bases string slice from the entity
// metadata in a content_entity fact payload.
func inheritancePayloadBases(payload map[string]any) []string {
	return semanticPayloadMetadataStringSlice(payload, "bases")
}
