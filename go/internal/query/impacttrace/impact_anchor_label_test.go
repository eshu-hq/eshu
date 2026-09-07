// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"fmt"
	"strings"
	"testing"
)

// assertNoImpactLabelDisjunction fails when a by-id anchor uses the label
// disjunction (`A|B|C`), which matches zero rows on the pinned NornicDB build.
func assertNoImpactLabelDisjunction(t *testing.T, cypher string) {
	t.Helper()
	if strings.Contains(cypher, impactAnchorLabelDisjunction) {
		t.Fatalf("by-id anchor must use per-label inline-property anchors, not the label disjunction: %s", cypher)
	}
}

// TestImpactAnchorResolveCypherIsPerLabelUnion guards the #5286 fix: the by-id
// label resolver must be a CALL{UNION} of per-label inline-property anchors, not
// a label-disjunction anchor (which matches zero rows on the pinned NornicDB).
func TestImpactAnchorResolveCypherIsPerLabelUnion(t *testing.T) {
	t.Parallel()

	// The resolver must be a CALL{UNION} of per-label inline-property anchors,
	// never the label disjunction, and never a `WHERE n.id` predicate that would
	// reintroduce the disjunction-shaped scan.
	resolve := impactAnchorResolveCypher("start_id")
	assertNoImpactLabelDisjunction(t, resolve)
	if !strings.Contains(resolve, "CALL {") {
		t.Errorf("resolve must wrap the per-label union in CALL {}: %s", resolve)
	}
	if strings.Contains(resolve, "WHERE") && strings.Contains(resolve, ".id =") {
		t.Errorf("resolve must anchor by inline property, not a WHERE id predicate: %s", resolve)
	}
	// It must anchor every label on BOTH id and name (callers pass human names).
	for _, label := range []string{"CloudResource", "Repository", "TerraformResource", "KubernetesWorkload"} {
		if !strings.Contains(resolve, "MATCH (n:"+label+" {id: $start_id})") {
			t.Errorf("resolve must anchor %s by id: %s", label, resolve)
		}
		if !strings.Contains(resolve, "MATCH (n:"+label+" {name: $start_id})") {
			t.Errorf("resolve must anchor %s by name: %s", label, resolve)
		}
	}

	// The traversal anchors a single resolved label inline (no disjunction) and
	// projects the raw relationships(path) list to a Repository target.
	traversal := fmt.Sprintf(ImpactRepoPathCypher, "(start:Repository {id: $start_id})", 8)
	assertNoImpactLabelDisjunction(t, traversal)
	if strings.Count(traversal, "MATCH") != 1 {
		t.Errorf("traversal must be a single anchoring MATCH: %s", traversal)
	}
	if !strings.Contains(traversal, "relationships(path) AS rels") || !strings.Contains(traversal, "(repo:Repository)") {
		t.Errorf("traversal must project relationships(path) to a Repository target: %s", traversal)
	}
}

// TestImpactAnchorLabelDisjunctionIncludesTerraformResource proves that
// TerraformResource is present in the impact anchor label disjunction.
// TerraformResource nodes are written with SET r.id = row.uid
// (go/internal/storage/cypher/tfstate_canonical_writer.go) so callers that
// pass a TerraformResource uid as start_id must resolve to a non-empty anchor.
// The prior unlabeled MATCH found them; the labeled disjunction must too.
func TestImpactAnchorLabelDisjunctionIncludesTerraformResource(t *testing.T) {
	t.Parallel()
	if !strings.Contains(impactAnchorLabelDisjunction, "TerraformResource") {
		t.Fatalf("impactAnchorLabelDisjunction must include TerraformResource (its .id is set to row.uid by tfstate_canonical_writer); got: %s", impactAnchorLabelDisjunction)
	}
}

// TestImpactAnchorLabelDisjunctionIncludesTerraformOutput proves TerraformOutput
// is present. TerraformOutput nodes are written with SET o.id = row.uid
// (go/internal/storage/cypher/tfstate_canonical_writer.go) so they share the
// same id-via-uid pattern as TerraformResource.
func TestImpactAnchorLabelDisjunctionIncludesTerraformOutput(t *testing.T) {
	t.Parallel()
	if !strings.Contains(impactAnchorLabelDisjunction, "TerraformOutput") {
		t.Fatalf("impactAnchorLabelDisjunction must include TerraformOutput (its .id is set to row.uid by tfstate_canonical_writer); got: %s", impactAnchorLabelDisjunction)
	}
}

// TestImpactAnchorLabelDisjunctionIncludesKubernetesWorkload proves
// KubernetesWorkload is present. KubernetesWorkload nodes are written with
// SET w.id = row.uid (go/internal/storage/cypher/kubernetes_workload_node_writer.go)
// so callers that pass a KubernetesWorkload uid as start_id must resolve.
func TestImpactAnchorLabelDisjunctionIncludesKubernetesWorkload(t *testing.T) {
	t.Parallel()
	if !strings.Contains(impactAnchorLabelDisjunction, "KubernetesWorkload") {
		t.Fatalf("impactAnchorLabelDisjunction must include KubernetesWorkload (its .id is set to row.uid by kubernetes_workload_node_writer); got: %s", impactAnchorLabelDisjunction)
	}
}
