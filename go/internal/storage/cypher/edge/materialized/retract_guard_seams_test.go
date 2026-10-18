// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialized

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// retractMatchNodeLabel matches the labeled node a detach retract MATCHes,
// e.g. MATCH (e:CodeTaintEvidence). It is the same anchor
// matchNodeLabelFromRetract read, hoisted to a package var so the
// detach-shape seam reads it without *testing.T.
var retractMatchNodeLabel = regexp.MustCompile(`MATCH\s*\(\s*\w+\s*:\s*([A-Za-z_][A-Za-z0-9_]*)\b`)

// retractRelTypesInVariant returns the set of relationship types a retract
// template deletes, read out of the template text itself. It is the pure core
// behind the alternation seam: empty when the template names no type, in
// which case the seam reports that as a violation.
func retractRelTypesInVariant(variant string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, m := range relTypeAlternation.FindAllStringSubmatch(variant, -1) {
		for _, relType := range strings.Split(m[1], "|") {
			if relType = strings.TrimSpace(relType); relType != "" {
				out[relType] = struct{}{}
			}
		}
	}
	return out
}

// matchNodeLabelInRetract returns the node label a detach retract MATCHes,
// read out of the template text itself. The second result is false when the
// template MATCHes no labeled node, in which case the seam reports that as a
// violation.
func matchNodeLabelInRetract(variant string) (string, bool) {
	m := retractMatchNodeLabel.FindStringSubmatch(variant)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// relAlternationRetractViolations pins one held retract variant's named
// alternation against the family's declared edge types, in both directions:
// a type the retract deletes but the registry omits is one the Ifá baseline
// gate ignores while calling the family exhaustive, and a type the registry
// names but the retract does not delete survives every reprojection as stale
// truth. It is the pure seam behind checkRelAlternationRetract: the messages
// match the guard's, and an empty extraction is a violation here rather than
// a Fatalf, so every held variant reports instead of aborting the subtest.
func relAlternationRetractViolations(family, variant string, edgeTypes map[string]string) []string {
	retracted := retractRelTypesInVariant(variant)
	if len(retracted) == 0 {
		return []string{fmt.Sprintf("family %q: retract template names no relationship type; a retract that reaps nothing leaves every edge of this family stale after reprojection:\n%s", family, variant)}
	}

	var missingFromRegistry, missingFromRetract []string
	for relType := range retracted {
		if _, ok := edgeTypes[relType]; !ok {
			missingFromRegistry = append(missingFromRegistry, relType)
		}
	}
	for relType := range edgeTypes {
		if _, ok := retracted[relType]; !ok {
			missingFromRetract = append(missingFromRetract, relType)
		}
	}
	sort.Strings(missingFromRegistry)
	sort.Strings(missingFromRetract)

	var violations []string
	if len(missingFromRegistry) > 0 {
		violations = append(violations, fmt.Sprintf("retract deletes %v but the registry omits them; the Ifá baseline gate would ignore those edges while calling %s exhaustive",
			missingFromRegistry, family))
	}
	if len(missingFromRetract) > 0 {
		violations = append(violations, fmt.Sprintf("registry names %v but the retract does not delete them; those edges would survive every reprojection as stale truth",
			missingFromRetract))
	}
	return violations
}

// closedVocabularyPredicateViolations pins what the #6228 split verdict's
// kind-C refactor must keep while naming the alternation: the
// evidence_source plus scope predicates on the variant, no bare [rel] that
// would reap a foreign type sharing the endpoints and source, and no DETACH
// DELETE (node deletion belongs to evidenceNodeDetach families with a
// sole-creator census). It is the pure seam behind
// checkClosedVocabularyPredicates.
func closedVocabularyPredicateViolations(variant string) []string {
	var violations []string
	if !strings.Contains(variant, "evidence_source") {
		violations = append(violations, fmt.Sprintf("retract variant dropped the evidence_source predicate; it would reap edges another writer owns:\n%s", variant))
	}
	if !strings.Contains(variant, "scope_id") {
		violations = append(violations, fmt.Sprintf("retract variant dropped the scope predicate; it would reap edges another scope owns:\n%s", variant))
	}
	if bareRelDelete.MatchString(variant) {
		violations = append(violations, fmt.Sprintf("retract variant still carries a bare [rel]; scoped only by evidence_source and scope it reaps any foreign type sharing the endpoints:\n%s", variant))
	}
	if strings.Contains(variant, "DETACH DELETE") {
		violations = append(violations, fmt.Sprintf("relAlternation retract variant DETACH DELETEs nodes; node deletion belongs to evidenceNodeDetach families with a sole-creator census:\n%s", variant))
	}
	return violations
}

// evidenceNodeDetachRetractViolations pins one held retract variant's detach
// shape: MATCH on the evidence label, DETACH DELETE of that node, and the
// evidence_source plus scope predicates that keep the delete to this writer's
// generation. The edge cascade needs no type alternation because the
// sole-creator census proves no other writer's edges hang off these nodes.
// It is the pure seam behind checkEvidenceNodeDetachRetract; a template that
// MATCHes no labeled node is a violation here rather than a Fatalf.
func evidenceNodeDetachRetractViolations(family, variant, evidenceLabel string) []string {
	var violations []string
	got, ok := matchNodeLabelInRetract(variant)
	if !ok {
		violations = append(violations, fmt.Sprintf("family %q: retract MATCHes no labeled node; a detach retract must anchor its evidence label:\n%s", family, variant))
	} else if got != evidenceLabel {
		violations = append(violations, fmt.Sprintf("retract MATCHes node label %q, want the evidence label %q derived from the held write template; a detach on the wrong label deletes nodes it does not own", got, evidenceLabel))
	}
	if !strings.Contains(variant, "DETACH DELETE") {
		violations = append(violations, fmt.Sprintf("evidenceNodeDetach retract variant does not DETACH DELETE; without the detach the evidence nodes (and their edges) survive reprojection:\n%s", variant))
	}
	if !strings.Contains(variant, "evidence_source") {
		violations = append(violations, fmt.Sprintf("retract variant dropped the evidence_source predicate; it would detach evidence nodes another writer owns:\n%s", variant))
	}
	if !strings.Contains(variant, "scope_id") {
		violations = append(violations, fmt.Sprintf("retract variant dropped the scope predicate; it would detach evidence nodes another scope owns:\n%s", variant))
	}
	return violations
}

// TestClosedVocabularyPredicateViolationsSeeded proves the predicate seam can
// fail: each planted row drops exactly one pinned property and the single
// violation names it, while the clean row passes. If this pair ever stops
// distinguishing, the multi-type direct predicate guard is a comment, not a
// check.
func TestClosedVocabularyPredicateViolationsSeeded(t *testing.T) {
	t.Parallel()

	const clean = `MATCH (a:SgRule)-[r:ALLOWS_INGRESS]->(b:SgRule) WHERE r.evidence_source = $evidence_source AND r.scope_id IN $scope_ids DELETE r`

	tests := []struct {
		name      string
		variant   string
		wantToken string // empty means want zero violations
	}{
		{"clean variant passes", clean, ""},
		{
			"dropped evidence_source names it",
			`MATCH (a:SgRule)-[r:ALLOWS_INGRESS]->(b:SgRule) WHERE r.writer = $writer AND r.scope_id IN $scope_ids DELETE r`,
			"evidence_source",
		},
		{
			"dropped scope predicate names it",
			`MATCH (a:SgRule)-[r:ALLOWS_INGRESS]->(b:SgRule) WHERE r.evidence_source = $evidence_source AND r.shard IN $shards DELETE r`,
			"scope predicate",
		},
		{
			"bare rel names it",
			`MATCH (a:SgRule)-[r]->(b:SgRule) WHERE r.evidence_source = $evidence_source AND r.scope_id IN $scope_ids DELETE r`,
			"bare [rel]",
		},
		{
			"detach delete names it",
			clean + " DETACH DELETE a",
			"DETACH DELETE",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			violations := closedVocabularyPredicateViolations(tt.variant)
			if tt.wantToken == "" {
				if len(violations) != 0 {
					t.Fatalf("clean variant reported violations, want none: %v", violations)
				}
				return
			}
			if len(violations) != 1 {
				t.Fatalf("planted variant reported %d violations, want exactly 1 naming %q: %v", len(violations), tt.wantToken, violations)
			}
			if !strings.Contains(violations[0], tt.wantToken) {
				t.Fatalf("violation does not name the planted failure %q: %v", tt.wantToken, violations)
			}
		})
	}
}

// TestEvidenceNodeDetachRetractViolationsSeeded proves the detach-shape seam
// can fail: each planted row breaks exactly one pinned property and the
// single violation names it, while the clean row passes.
func TestEvidenceNodeDetachRetractViolationsSeeded(t *testing.T) {
	t.Parallel()

	const label = "SeedEvidence"
	const clean = `MATCH (e:SeedEvidence) WHERE e.evidence_source = $evidence_source AND e.scope_id IN $scope_ids DETACH DELETE e`

	tests := []struct {
		name      string
		variant   string
		wantToken string // empty means want zero violations
	}{
		{"clean variant passes", clean, ""},
		{
			"wrong match label names it",
			`MATCH (e:ForeignEvidence) WHERE e.evidence_source = $evidence_source AND e.scope_id IN $scope_ids DETACH DELETE e`,
			"ForeignEvidence",
		},
		{
			"unlabeled match names it",
			`MATCH (e) WHERE e.evidence_source = $evidence_source AND e.scope_id IN $scope_ids DETACH DELETE e`,
			"no labeled node",
		},
		{
			"missing detach names it",
			`MATCH (e:SeedEvidence) WHERE e.evidence_source = $evidence_source AND e.scope_id IN $scope_ids DELETE e`,
			"DETACH DELETE",
		},
		{
			"dropped evidence_source names it",
			`MATCH (e:SeedEvidence) WHERE e.writer = $writer AND e.scope_id IN $scope_ids DETACH DELETE e`,
			"evidence_source",
		},
		{
			"dropped scope predicate names it",
			`MATCH (e:SeedEvidence) WHERE e.evidence_source = $evidence_source AND e.shard IN $shards DETACH DELETE e`,
			"scope predicate",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			violations := evidenceNodeDetachRetractViolations("seeded_family", tt.variant, label)
			if tt.wantToken == "" {
				if len(violations) != 0 {
					t.Fatalf("clean variant reported violations, want none: %v", violations)
				}
				return
			}
			if len(violations) != 1 {
				t.Fatalf("planted variant reported %d violations, want exactly 1 naming %q: %v", len(violations), tt.wantToken, violations)
			}
			if !strings.Contains(violations[0], tt.wantToken) {
				t.Fatalf("violation does not name the planted failure %q: %v", tt.wantToken, violations)
			}
		})
	}
}

// TestRelAlternationRetractViolationsSeeded proves the alternation seam checks
// every held variant, not just the primary: variants[0] stands in for the
// primary RetractCypher and variants[1:] for the held ByUIDs-style variants,
// and a planted violation at any index is reported naming the offending type.
// The clean row passes.
func TestRelAlternationRetractViolationsSeeded(t *testing.T) {
	t.Parallel()

	edgeTypes := map[string]string{"TYPE_A": "seeded", "TYPE_B": "seeded"}
	const cleanPrimary = `MATCH (a:X)-[r:TYPE_A|TYPE_B]->(b:Y) WHERE r.scope_id IN $scope_ids DELETE r`
	const cleanByUIDs = `MATCH (a:X)-[r:TYPE_A|TYPE_B]->(b:Y) WHERE a.uid IN $uids DELETE r`

	tests := []struct {
		name       string
		variants   []string
		wantFailAt map[int]string // variant index -> token the violation must name
	}{
		{"clean primary and ByUIDs pass", []string{cleanPrimary, cleanByUIDs}, nil},
		{
			"ByUIDs variant missing a type names it",
			[]string{cleanPrimary, `MATCH (a:X)-[r:TYPE_A]->(b:Y) WHERE a.uid IN $uids DELETE r`},
			map[int]string{1: "TYPE_B"},
		},
		{
			"primary deleting a foreign type names it",
			[]string{`MATCH (a:X)-[r:TYPE_A|TYPE_B|TYPE_Z]->(b:Y) WHERE r.scope_id IN $scope_ids DELETE r`, cleanByUIDs},
			map[int]string{0: "TYPE_Z"},
		},
		{
			"typeless variant names it",
			[]string{cleanPrimary, `MATCH (a:X)-[r]->(b:Y) WHERE a.uid IN $uids DELETE r`},
			map[int]string{1: "no relationship type"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for i, variant := range tt.variants {
				violations := relAlternationRetractViolations("seeded_family", variant, edgeTypes)
				wantToken, fail := tt.wantFailAt[i]
				if !fail {
					if len(violations) != 0 {
						t.Errorf("variant %d: clean variant reported violations, want none: %v", i, violations)
					}
					continue
				}
				if len(violations) == 0 {
					t.Errorf("variant %d: planted violation reported nothing; the seam cannot detect the drift it exists to catch", i)
					continue
				}
				if joined := strings.Join(violations, "\n"); !strings.Contains(joined, wantToken) {
					t.Errorf("variant %d: violation does not name the planted failure %q: %v", i, wantToken, violations)
				}
			}
		})
	}
}
