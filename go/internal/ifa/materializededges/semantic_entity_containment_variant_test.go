// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"path/filepath"
	"testing"

	entity "github.com/eshu-hq/eshu/go/internal/storage/cypher/semantic/entity"
)

// semanticEntityContainmentVariantRelPath is the NornicDB expected-edge
// variant: same four pairs as the base set, with the canonical-owned rows
// stamped by the canonical writer instead of the semantic writer (#8359).
const semanticEntityContainmentVariantRelPath = "go/internal/ifa/testdata/semanticentitycontainment/ifa-semantic-entity-containment-family-expected-edges.nornicdb.json"

// TestSemanticEntityContainmentVariantMirrorsBasePairs pins the NornicDB
// variant to the base expected set: identical (source, target) pair
// multiset and single fixture repo, so the two files can never drift into
// asserting different endpoint populations. The stamp split is the only
// allowed difference: the semantic-owned Variable edge keeps the semantic
// stamp, the three canonical-owned rows carry the canonical writer's stamp.
func TestSemanticEntityContainmentVariantMirrorsBasePairs(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootDir(t)
	baseRepo, base, err := LoadSemanticEntityContainmentExpectedEdges(
		filepath.Join(repoRoot, semanticEntityContainmentExpectedEdgesRelPath),
	)
	if err != nil {
		t.Fatalf("load base expected edges: %v", err)
	}
	variantRepo, variant, err := LoadSemanticEntityContainmentExpectedEdges(
		filepath.Join(repoRoot, semanticEntityContainmentVariantRelPath),
	)
	if err != nil {
		t.Fatalf("load variant expected edges: %v", err)
	}
	if variantRepo != baseRepo {
		t.Errorf("variant repo_id = %q, want base %q", variantRepo, baseRepo)
	}

	pairs := func(edges []ExpectedEdge) map[string]int {
		out := make(map[string]int, len(edges))
		for _, e := range edges {
			out[e.RelationshipType+"\x00"+e.SourceEntityID+"\x00"+e.TargetEntityID]++
		}
		return out
	}
	basePairs, variantPairs := pairs(base), pairs(variant)
	if len(basePairs) != 4 || len(variantPairs) != 4 {
		t.Fatalf("pair counts base/variant = %d/%d, want 4/4", len(basePairs), len(variantPairs))
	}
	for pair := range basePairs {
		if variantPairs[pair] != basePairs[pair] {
			t.Errorf("variant pair counts differ for %q: base=%d variant=%d", pair, basePairs[pair], variantPairs[pair])
		}
	}

	const variableTarget = "content-entity:sec-ex-var"
	for _, e := range variant {
		stamp := e.Properties["evidence_source"]
		if e.TargetEntityID == variableTarget {
			if stamp != entity.SemanticEntityEvidenceSource {
				t.Errorf("variable edge stamp = %q, want semantic %q", stamp, entity.SemanticEntityEvidenceSource)
			}
			continue
		}
		if stamp == entity.SemanticEntityEvidenceSource {
			t.Errorf("canonical-owned edge %s->%s keeps the semantic stamp; want the canonical writer's stamp", e.SourceEntityID, e.TargetEntityID)
		}
		// No hermetic pin from the variant stamp back to the canonical
		// writer template here: that template lives in a package this one
		// must not import for a string-contains check. Template-to-variant
		// agreement is proven live instead — a writer stamp change surfaces
		// in the determinism matrix as missing-plus-extra, never silence.
	}

	for _, e := range base {
		if stamp := e.Properties["evidence_source"]; stamp != entity.SemanticEntityEvidenceSource {
			t.Errorf("base edge %s->%s stamp = %q, want all-semantic %q", e.SourceEntityID, e.TargetEntityID, stamp, entity.SemanticEntityEvidenceSource)
		}
	}
}
