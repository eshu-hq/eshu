// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// documentationExpectedEdgesRelPath is the repo-root-relative path to the
// hand-derived expected documentation-edge set (#5994).
//
// It lives under this package's testdata/ tree rather than
// testdata/cassettes/: it is a gate ASSERTION file, and the offline cassette
// validator globs every testdata/cassettes/*/*.json as a replay cassette.
// Same split the SQL family uses, and for the same reason.
const documentationExpectedEdgesRelPath = "go/internal/ifa/testdata/documentation/ifa-documentation-family-expected-edges.json"

// documentationFamilyExpectedEdgesPath joins repoRoot onto the fixture path.
func documentationFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, documentationExpectedEdgesRelPath)
}

// documentationExpectedEdge is one hand-derived expected documentation edge.
//
// The identity is the pair the extractor dedups on (section_uid ->
// target_entity_id) plus target_kind, which decides whether the edge is
// emitted at all. mention_kind is deliberately NOT part of the identity: two
// mentions of the same entity from the same section collapse to one edge, and
// which mention_kind survives that dedup is first-write-wins rather than a
// contract.
type documentationExpectedEdge struct {
	SectionUID     string `json:"section_uid"`
	TargetEntityID string `json:"target_entity_id"`
	TargetKind     string `json:"target_kind"`
}

type documentationExpectedEdgesFile struct {
	Odu   string                      `json:"odu"`
	Edges []documentationExpectedEdge `json:"edges"`
}

// loadDocumentationExpectedEdges reads and parses the expected-edge fixture.
func loadDocumentationExpectedEdges(path string) ([]documentationExpectedEdge, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return nil, fmt.Errorf("ifa: read documentation expected edges %s: %w", path, err)
	}
	var parsed documentationExpectedEdgesFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("ifa: parse documentation expected edges %s: %w", path, err)
	}
	if len(parsed.Edges) == 0 {
		return nil, fmt.Errorf("ifa: documentation expected edges %s declares no edges; an empty expected set would make the gate vacuous", path)
	}
	return parsed.Edges, nil
}

// documentationRowsToExpectedEdges projects extractor rows onto the comparable
// identity.
func documentationRowsToExpectedEdges(rows []map[string]any) []documentationExpectedEdge {
	out := make([]documentationExpectedEdge, 0, len(rows))
	for _, row := range rows {
		out = append(out, documentationExpectedEdge{
			SectionUID:     anyToStringValue(row["section_uid"]),
			TargetEntityID: anyToStringValue(row["target_entity_id"]),
			TargetKind:     anyToStringValue(row["target_kind"]),
		})
	}
	return out
}

// documentationEdgeKey renders one edge in a stable comparable form.
func documentationEdgeKey(e documentationExpectedEdge) string {
	return e.SectionUID + "\x00" + e.TargetEntityID + "\x00" + e.TargetKind
}

// compareDocumentationExpectedSets reports the exact-set mismatch between the
// hand-derived expectation and what the extractor actually produced, naming
// both directions.
//
// Both directions matter and for different reasons: a MISSING edge means the
// extractor stopped projecting something the graph is supposed to carry, and an
// EXTRA edge means it started projecting something nobody derived — which is
// how a gate that only checked "did we get at least the expected ones" would
// certify a quietly larger graph.
func compareDocumentationExpectedSets(oduName string, expected, actual []documentationExpectedEdge) string {
	expectedSet := make(map[string]struct{}, len(expected))
	for _, e := range expected {
		expectedSet[documentationEdgeKey(e)] = struct{}{}
	}
	actualSet := make(map[string]struct{}, len(actual))
	for _, e := range actual {
		actualSet[documentationEdgeKey(e)] = struct{}{}
	}

	var missing, extra []string
	for _, e := range expected {
		if _, ok := actualSet[documentationEdgeKey(e)]; !ok {
			missing = append(missing, fmt.Sprintf("%s -[MENTIONS]-> %s (%s)", e.SectionUID, e.TargetEntityID, e.TargetKind))
		}
	}
	for _, e := range actual {
		if _, ok := expectedSet[documentationEdgeKey(e)]; !ok {
			extra = append(extra, fmt.Sprintf("%s -[MENTIONS]-> %s (%s)", e.SectionUID, e.TargetEntityID, e.TargetKind))
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) == 0 && len(extra) == 0 {
		if len(actual) != len(expected) {
			return fmt.Sprintf("odù %q: extractor produced %d row(s) for %d distinct expected edge(s); the extractor's own dedup should have collapsed them",
				oduName, len(actual), len(expected))
		}
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "odù %q: documentation edge set does not match %d hand-derived edge(s)", oduName, len(expected))
	if len(missing) > 0 {
		fmt.Fprintf(&b, "; MISSING (the extractor no longer projects these): %s", strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		fmt.Fprintf(&b, "; EXTRA (nobody derived these): %s", strings.Join(extra, ", "))
	}
	return b.String()
}

// resolveDocumentationEdgeMaterializedEdges is the documentation_edges family's
// vacuity guard (#5994).
//
// It runs the production extractor over the Odù's facts and asserts the result
// equals the hand-derived set exactly. A decode failure is fatal rather than
// ignored: quarantined facts mean the fixture no longer validates against the
// contract, and silently projecting the survivors would let the fixture rot
// into a smaller proof than it claims to be.
func resolveDocumentationEdgeMaterializedEdges(odu Odu, expectedEdgesPath string) (bool, string) {
	expected, err := loadDocumentationExpectedEdges(expectedEdgesPath)
	if err != nil {
		return false, err.Error()
	}

	rows, quarantined, extractErr := reducer.ExtractDocumentationEdgeRowsWithQuarantine(odu.Facts, documentationFamilyScopeID)
	if extractErr != nil {
		return false, fmt.Sprintf("odù %q: ExtractDocumentationEdgeRowsWithQuarantine failed: %v", odu.Name, extractErr)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the documentation contract, so any edge set derived from the survivors understates what it claims to prove",
			odu.Name, len(quarantined))
	}

	actual := documentationRowsToExpectedEdges(rows)
	if mismatch := compareDocumentationExpectedSets(odu.Name, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf("odù %q: ExtractDocumentationEdgeRowsWithQuarantine reproduces the expected %d-edge set exactly, with %d mention(s) correctly deriving no edge",
		odu.Name, len(expected), len(odu.Facts)-len(rows))
}
