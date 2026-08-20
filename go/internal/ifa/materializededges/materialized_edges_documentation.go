// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// documentationExpectedEdgesRelPath is the repo-root-relative path to the
// hand-derived expected documentation-edge set (#5994).
//
// It lives under this package's testdata/ tree rather than
// testdata/cassettes/: it is a gate ASSERTION file, and the offline cassette
// validator globs every testdata/cassettes/*/*.json as a replay cassette.
// Same split the SQL and code-call families use, and for the same reason.
//
// This is the SAME file the live gate scripts pass to `eshu-ifa assert-edges`
// (scripts/lib/ifa_documentation_live.sh), in the shared ExpectedEdge
// {relationship_type, source_entity_id, target_entity_id} shape LoadExpectedEdges
// reads -- not a family-specific schema. A prior version of this file used its
// own 3-field {section_uid, target_entity_id, target_kind} schema pointed at a
// SEPARATE offline-only fixture; that let the pure guard and the live gate
// silently assert two different graphs (the offline fixture's targets were
// invented shapes, "func:payments.Charge" and "sqltable:public.payments",
// that never matched a real graph uid -- exactly the false-green class commit
// 42e188ba1 was withdrawn for). documentationFamilyOdu() now projects the
// committed cassette (documentation_family_catalog.go), so the resolver and
// the live gate read the same fact set and the same expected-edge fixture and
// cannot drift.
const documentationExpectedEdgesRelPath = "go/internal/ifa/testdata/documentation/ifa-documentation-family-live-expected-edges.json"

// documentationFamilyExpectedEdgesPath joins repoRoot onto the fixture path.
func documentationFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, documentationExpectedEdgesRelPath)
}

// documentationFamilyOduName duplicates the
// documentation family's Odù name and scope ID from ifa's
// documentation_family_catalog.go: this package cannot reach those unexported
// constants across the package boundary, and documentation_family_catalog.go
// / documentation_family_odu.go / documentation_family_odu_test.go still need
// their own copies to stay in ifa, so this is a copy, not a move.
const (
	documentationFamilyOduName = "odu:ifa-documentation-family"
)

// resolveDocumentationEdgeMaterializedEdges is the documentation_edges family's
// vacuity guard (#5994). Mirrors resolveCodeCallMaterializedEdges's shape.
//
// It runs the production extractor over the Odù's facts and asserts the result
// equals the hand-derived set exactly, using the SAME shared ExpectedEdge
// fixture format and loader the live `eshu-ifa assert-edges -domain
// documentation_edges` call reads. A decode failure is fatal rather than
// ignored: quarantined facts mean the fixture no longer validates against the
// contract, and silently projecting the survivors would let the fixture rot
// into a smaller proof than it claims to be.
//
// target_kind is deliberately NOT asserted here, and that is a narrowing from
// the family-specific fixture this guard replaced. ExpectedEdge carries only
// the identity triple (type, source, target), so the writer's target_kind-driven
// template routing -- "workload" to the Workload template keyed on id,
// everything else to the entity template keyed on uid
// (edge_writer_documentation_labels.go) -- is proven by the live assert-edges
// call, not by this guard. A mention whose target_kind flipped while its
// target_entity_id stayed put would read identical here and route to a template
// that finds nothing live; assert-edges reports that as a MISSING edge, and the
// gate triggers cover both the writer and the reducer's extraction. Restoring
// target_kind to this guard would mean re-splitting the fixture format, which is
// what bound the coverage rows to an artifact the live gate never drove.
func resolveDocumentationEdgeMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, err := LoadExpectedEdges(expectedEdgesPath, "documentation_edges")
	if err != nil {
		return false, err.Error()
	}
	if len(expected) == 0 {
		return false, fmt.Sprintf("odù %q: documentation expected edges %s declares no edges; an empty expected set would make the gate vacuous", odu.Name, expectedEdgesPath)
	}
	registry, err := MaterializedEdgeDomainEdgeTypes("documentation_edges")
	if err != nil {
		return false, err.Error()
	}
	if missing := missingDocumentationExpectedTypes(expected, registry); len(missing) > 0 {
		return false, fmt.Sprintf("odù %q: expected-edge set does not cover all registry types, missing: %v", odu.Name, missing)
	}

	rows, quarantined, extractErr := reducer.ExtractDocumentationEdgeRowsWithQuarantine(odu.Facts, ifa.DocumentationFamilyScopeID)
	if extractErr != nil {
		return false, fmt.Sprintf("odù %q: ExtractDocumentationEdgeRowsWithQuarantine failed: %v", odu.Name, extractErr)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the documentation contract, so any edge set derived from the survivors understates what it claims to prove",
			odu.Name, len(quarantined))
	}

	mentionCount := 0
	for _, env := range odu.Facts {
		if env.FactKind == facts.DocumentationEntityMentionFactKind {
			mentionCount++
		}
	}

	actual := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		actual = append(actual, ExpectedEdge{
			RelationshipType: "DOCUMENTS",
			SourceEntityID:   anyToStringValue(row["section_uid"]),
			TargetEntityID:   anyToStringValue(row["target_entity_id"]),
		})
	}
	if mismatch := compareDocumentationExpectedEdges(odu.Name, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf("odù %q: ExtractDocumentationEdgeRowsWithQuarantine reproduces the expected %d-edge set exactly, with %d of %d mention(s) correctly deriving no edge",
		odu.Name, len(expected), mentionCount-len(rows), mentionCount)
}

// missingDocumentationExpectedTypes reports any registry-owned DOCUMENTS
// relationship type the expected-edge fixture never exercises. Single-type
// today (only "DOCUMENTS"), but registry-derived rather than hand-checked so a
// second documentation writer relationship type cannot go silently unasserted
// the way #5994's review found this guard had no such check at all.
func missingDocumentationExpectedTypes(expected []ExpectedEdge, registry map[string]struct{}) []string {
	present := make(map[string]struct{}, len(expected))
	for _, edge := range expected {
		present[edge.RelationshipType] = struct{}{}
	}
	var missing []string
	for edgeType := range registry {
		if _, ok := present[edgeType]; !ok {
			missing = append(missing, edgeType)
		}
	}
	sort.Strings(missing)
	return missing
}

// compareDocumentationExpectedEdges reports the exact-set mismatch between the
// hand-derived expectation and what the extractor actually produced, naming
// both directions.
//
// Both directions matter and for different reasons: a MISSING edge means the
// extractor stopped projecting something the graph is supposed to carry, and an
// EXTRA edge means it started projecting something nobody derived — which is
// how a gate that only checked "did we get at least the expected ones" would
// certify a quietly larger graph.
func compareDocumentationExpectedEdges(oduName string, expected, actual []ExpectedEdge) string {
	want := make(map[string]int, len(expected))
	got := make(map[string]int, len(actual))
	for _, edge := range expected {
		want[edge.Key()]++
	}
	for _, edge := range actual {
		got[edge.Key()]++
	}
	var missing, extra []string
	for key, count := range want {
		if got[key] < count {
			missing = append(missing, key)
		}
	}
	for key, count := range got {
		if count > want[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	return fmt.Sprintf("odù %q: documentation edge set does not match %d hand-derived edge(s); MISSING: %s; EXTRA: %s",
		oduName, len(expected), strings.Join(missing, ", "), strings.Join(extra, ", "))
}
