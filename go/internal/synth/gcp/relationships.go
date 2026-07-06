// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// relationshipTypeToken is the normalized synthetic relationship type every
// generated edge carries. A single bounded token keeps the corpus reviewable;
// real collectors emit provider-specific verbs, but the schema only requires
// a non-empty string, so one deterministic token satisfies conformance
// without inventing a taxonomy this generator does not own.
const relationshipTypeToken = "synthetic_contained_in"

// buildRelationshipFacts derives one gcp_cloud_relationship fact per
// consecutive pair of already-generated resources (resource[i] -> resource
// [i-1]), so every relationship's endpoints are real, previously generated
// resource identities rather than fabricated join keys. The first resource
// has no predecessor and gets no relationship.
func (g *generation) buildRelationshipFacts() ([]cassette.Fact, error) {
	if len(g.resources) < 2 {
		return nil, nil
	}
	facts := make([]cassette.Fact, 0, len(g.resources)-1)
	for i := 1; i < len(g.resources); i++ {
		source := g.resources[i]
		target := g.resources[i-1]
		relationship := buildOneRelationship(source, target)

		payload, err := factschema.EncodeGCPCloudRelationship(relationship)
		if err != nil {
			return nil, fmt.Errorf("synth/gcp: encode gcp_cloud_relationship[%d]: %w", i, err)
		}
		fact, err := generateFact(factschema.FactKindGCPCloudRelationship, factKindSchemaVersions[factschema.FactKindGCPCloudRelationship], payload)
		if err != nil {
			return nil, err
		}
		fact.StableFactKey = fmt.Sprintf("gcp:project:%s:relationship:%s:%s->%s",
			g.opts.ProjectID, relationshipTypeToken, source.FullResourceName, target.FullResourceName)
		facts = append(facts, fact)
	}
	return facts, nil
}

// buildOneRelationship builds a schema-valid gcpv1.Relationship whose
// endpoints are two real generated resources.
func buildOneRelationship(source, target gcpv1.Resource) gcpv1.Relationship {
	relationshipType := relationshipTypeToken
	supportState := "supported"
	return gcpv1.Relationship{
		SourceFullResourceName: source.FullResourceName,
		TargetFullResourceName: target.FullResourceName,
		RelationshipType:       relationshipType,
		SourceAssetType:        &source.AssetType,
		TargetAssetType:        &target.AssetType,
		SupportState:           &supportState,
	}
}
