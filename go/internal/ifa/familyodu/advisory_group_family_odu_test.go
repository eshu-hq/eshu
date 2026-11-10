// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestAdvisoryGroupFamilyOduShape pins the fixture's load-bearing shape:
// two repository facts in their own git scopes, one vulnerability.source_snapshot
// anchor fact and six finding facts in the one findings scope, every
// stable key distinct, and every finding
// carrying its alias-group members under provenance — never at the
// payload top level, the shape no production writer emits whose seeded
// use masked #10475.
func TestAdvisoryGroupFamilyOduShape(t *testing.T) {
	t.Parallel()

	odu := AdvisoryGroupFamilyOdu().Odu
	if got, want := len(odu.Facts), 9; got != want {
		t.Fatalf("advisory_group Odù carries %d facts, want %d (2 repositories + 1 anchor + 6 findings)", got, want)
	}
	seen := make(map[string]struct{}, len(odu.Facts))
	repos, findings, anchors := 0, 0, 0
	for _, env := range odu.Facts {
		if _, dup := seen[env.StableFactKey]; dup {
			t.Fatalf("duplicate stable_fact_key %q", env.StableFactKey)
		}
		seen[env.StableFactKey] = struct{}{}
		switch env.FactKind {
		case "repository":
			repos++
		case "vulnerability.source_snapshot":
			anchors++
			if env.ScopeID != advisoryGroupFamilyFindingScopeID {
				t.Fatalf("anchor fact %q is outside the findings scope", env.StableFactKey)
			}
		case facts.ReducerSupplyChainImpactFindingFactKind:
			findings++
			if env.ScopeID != advisoryGroupFamilyFindingScopeID || env.GenerationID != advisoryGroupFamilyFindingGenerationID {
				t.Fatalf("finding %q is outside the single findings scope generation", env.StableFactKey)
			}
			if _, topLevel := env.Payload["advisory_group_members"]; topLevel {
				t.Fatalf("finding %q carries advisory_group_members at the payload top level; the production writer puts them under provenance (#10475)", env.StableFactKey)
			}
			provenance, ok := env.Payload["provenance"].(map[string]any)
			if !ok {
				t.Fatalf("finding %q carries no provenance map", env.StableFactKey)
			}
			members, ok := provenance["advisory_group_members"]
			if !ok {
				t.Fatalf("finding %q carries no provenance advisory_group_members", env.StableFactKey)
			}
			if rendered := renderMembersForTest(members); rendered != "CVE-2026-10466,CVE-2026-10467,GHSA-10466-aaaa-bbbb,GHSA-10466-cccc-dddd" {
				t.Fatalf("finding %q members = %q, want the four-member alias union", env.StableFactKey, rendered)
			}
		default:
			t.Fatalf("unexpected fact kind %q in the advisory_group Odù", env.FactKind)
		}
	}
	if repos != 2 || findings != 6 || anchors != 1 {
		t.Fatalf("Odù carries %d repository, %d finding, and %d anchor facts, want 2, 6, and 1", repos, findings, anchors)
	}
}

// renderMembersForTest renders an encoded members value (a []string out
// of the encoder, a []any out of JSON) as a comma join for comparison.
func renderMembersForTest(members any) string {
	var out string
	appendOne := func(member string) {
		if out != "" {
			out += ","
		}
		out += member
	}
	switch values := members.(type) {
	case []string:
		for _, member := range values {
			appendOne(member)
		}
	case []any:
		for _, member := range values {
			if s, ok := member.(string); ok {
				appendOne(s)
			}
		}
	}
	return out
}
