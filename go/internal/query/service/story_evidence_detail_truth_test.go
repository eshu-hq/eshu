// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// oversizedStoryShape is one reachable shape of a large service: the fixture
// builder and the omission families that shape must reduce.
type oversizedStoryShape struct {
	name    string
	build   func() map[string]any
	omitted []string
}

// oversizedStoryShapes are the two reachable shapes of a large service. The
// story enrichment attaches cloud_resources or, when none attached, the
// uncorrelated candidates, never both, so each truth test runs over each shape
// to cover both families. omitted names the cloud family each shape owns, so
// dropping it from a fixture fails loudly instead of skipping the check.
var oversizedStoryShapes = []oversizedStoryShape{
	{name: "attached_cloud_resources", build: testutil.OversizedServiceDossierContext, omitted: []string{"cloud_resources"}},
	{name: "candidates_only", build: testutil.OversizedServiceDossierCandidatesOnly, omitted: []string{"uncorrelated_cloud_resources"}},
}

// forEachOversizedStoryShape runs body as a parallel subtest per reachable
// shape with the full story, its handles shape, and the handles omissions.
func forEachOversizedStoryShape(t *testing.T, body func(t *testing.T, shape oversizedStoryShape, full, handles map[string]any, omissions []querycontract.TruthOmission)) {
	t.Helper()
	for _, shape := range oversizedStoryShapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			full := BuildServiceStoryResponse("checkout-service", shape.build())
			handles := BuildServiceStoryResponse("checkout-service", shape.build())
			omissions := querycontract.ApplyStoryEvidenceDetail(handles, querycontract.ContextEvidenceDetailHandles)
			body(t, shape, full, handles, omissions)
		})
	}
}

// forEachOversizedStory is forEachOversizedStoryShape for tests that do not
// need the shape.
func forEachOversizedStory(t *testing.T, body func(t *testing.T, full, handles map[string]any, omissions []querycontract.TruthOmission)) {
	t.Helper()
	forEachOversizedStoryShape(t, func(t *testing.T, _ oversizedStoryShape, full, handles map[string]any, omissions []querycontract.TruthOmission) {
		body(t, full, handles, omissions)
	})
}

// walkRowLists calls visit for every []map[string]any reachable from value,
// with its dotted path, so a test covers lists a new family adds later.
func walkRowLists(path string, value any, visit func(path string, rows []map[string]any)) {
	switch typed := value.(type) {
	case []map[string]any:
		visit(path, typed)
		for i, row := range typed {
			for key, inner := range row {
				walkRowLists(fmt.Sprintf("%s[%d].%s", path, i, key), inner, visit)
			}
		}
	case map[string]any:
		for key, inner := range typed {
			walkRowLists(path+"."+key, inner, visit)
		}
	}
}

// TestStoryHandlesNeverShipAnEmptyIdentityRow proves every row of every list
// the handles view ships carries at least one key, so a caller can always tell
// which entity a row names. It walks the whole body instead of a fixed list of
// families (#8798 review P2-1).
func TestStoryHandlesNeverShipAnEmptyIdentityRow(t *testing.T) {
	t.Parallel()

	forEachOversizedStory(t, func(t *testing.T, full, handles map[string]any, _ []querycontract.TruthOmission) {
		empty := 0
		walkRowLists("story", handles, func(path string, rows []map[string]any) {
			for i, row := range rows {
				if len(row) == 0 {
					empty++
					t.Errorf("%s[%d] is an empty row under handles", path, i)
				}
			}
		})
		if empty > 0 {
			t.Fatalf("%d empty handle rows", empty)
		}

		segments := map[string][]map[string]any{}
		for _, segment := range querycontract.MapSliceValue(querycontract.MapValue(handles, "code_to_runtime_trace"), "segments") {
			segments[querycontract.StringVal(segment, "name")] = querycontract.MapSliceValue(segment, "evidence")
		}
		for name, key := range map[string]string{
			"service_identity": "service_id",
			"image_package":    "digest",
		} {
			if len(segments[name]) == 0 || querycontract.StringVal(segments[name][0], key) == "" {
				t.Fatalf("segment %s first handle row = %#v, want %s kept", name, segments[name], key)
			}
		}
		if got := querycontract.StringVal(segments["image_package"][0], "sbom_attachment_id"); got == "" {
			t.Fatalf("image_package handle = %#v, want the SBOM attachment identity kept", segments["image_package"][0])
		}
		if got := querycontract.StringVal(segments["ci_cd"][0], "evidence_summary_shipped_at"); got != "ci_cd_evidence" {
			t.Fatalf("ci_cd handle = %#v, want evidence_summary_shipped_at=ci_cd_evidence", segments["ci_cd"][0])
		}
		if len(querycontract.MapValue(full, "ci_cd_evidence")) == 0 {
			t.Fatal("fixture has no ci_cd_evidence; the test no longer proves the pointer")
		}
		repos := querycontract.MapSliceValue(querycontract.MapValue(handles, "investigation"), "repositories_considered")
		if len(repos) == 0 {
			t.Fatal("fixture has no investigation repositories")
		}
		marker := repos[len(repos)-1]
		if querycontract.StringVal(marker, "repo_id") != "__truncated__" || querycontract.StringVal(marker, "note") == "" {
			t.Fatalf("investigation truncation marker = %#v, want repo_id __truncated__ with its note kept", marker)
		}
	})
}

// TestStoryHandlesOmissionTotalsArePreCapTotals proves that for every family the
// shaper reduces, truth.omissions carries the pre-cap total read from the full
// story's own limits, never the capped row count (#8798 review P2-2). The
// fixture's pre-cap totals sit above the 50-row cap, so a total read from the
// wrong source shows as 50.
func TestStoryHandlesOmissionTotalsArePreCapTotals(t *testing.T) {
	t.Parallel()

	forEachOversizedStoryShape(t, func(t *testing.T, shape oversizedStoryShape, full, _ map[string]any, omissions []querycontract.TruthOmission) {
		rawLimits := querycontract.MapValue(full, "raw_context_limits")
		rawCount := func(key string) int { return querycontract.IntVal(querycontract.MapValue(rawLimits, key), "count") }
		downstream := querycontract.MapValue(full, "downstream_consumers")
		want := map[string]int{
			"upstream_dependencies":                   querycontract.IntVal(querycontract.MapValue(full, "result_limits"), "upstream_count"),
			"dependents":                              rawCount("dependents"),
			"consumer_repositories":                   rawCount("consumer_repositories"),
			"provisioning_source_chains":              rawCount("provisioning_source_chains"),
			"hostnames":                               rawCount("hostnames"),
			"entrypoints":                             rawCount("entrypoints"),
			"cloud_resources":                         rawCount("cloud_resources"),
			"uncorrelated_cloud_resources":            rawCount("uncorrelated_cloud_resources"),
			"deployment_evidence.artifacts":           querycontract.IntVal(querycontract.MapValue(querycontract.MapValue(rawLimits, "deployment_evidence"), "artifacts"), "count"),
			"deployment_evidence.delivery_paths":      querycontract.IntVal(querycontract.MapValue(querycontract.MapValue(rawLimits, "deployment_evidence"), "delivery_paths"), "count"),
			"deployment_evidence.shared_config_paths": querycontract.IntVal(querycontract.MapValue(querycontract.MapValue(rawLimits, "deployment_evidence"), "shared_config_paths"), "count"),
			"api_surface.endpoints":                   querycontract.IntVal(querycontract.MapValue(full, "api_surface"), "endpoint_count"),
			"downstream_consumers.graph_dependents":   querycontract.IntVal(downstream, "graph_dependent_count"),
			"downstream_consumers.content_consumers":  querycontract.IntVal(downstream, "content_consumer_count"),
			"evidence_graph.edges":                    querycontract.IntVal(querycontract.MapValue(full, "evidence_graph"), "edge_count"),
			"evidence_graph.nodes":                    len(querycontract.MapSliceValue(querycontract.MapValue(full, "evidence_graph"), "nodes")),
			// repositories_considered carries the packet's pre-cap repository count,
			// taken before its 50-row cap (#8802). repositories_with_evidence has
			// no count past the cap: repositories_with_evidence_count counts only
			// the rows returned, so its omission total is the 50 shipped rows.
			"investigation.repositories_considered":    querycontract.IntVal(querycontract.MapValue(querycontract.MapValue(full, "investigation"), "coverage_summary"), "repository_count"),
			"investigation.repositories_with_evidence": serviceStoryItemLimit,
		}
		for _, segment := range querycontract.MapSliceValue(querycontract.MapValue(full, "code_to_runtime_trace"), "segments") {
			if len(querycontract.MapSliceValue(segment, "evidence")) > 0 {
				want["code_to_runtime_trace."+querycontract.StringVal(segment, "name")+".evidence"] = querycontract.IntVal(segment, "evidence_count")
			}
		}

		got := map[string]int{}
		for _, omission := range omissions {
			got[omission.Section] = omission.Total
		}
		sections := make([]string, 0, len(got))
		for section := range got {
			sections = append(sections, section)
		}
		sort.Strings(sections)
		for _, section := range sections {
			expected, known := want[section]
			if !known {
				if strings.HasPrefix(section, "deployment_evidence.") {
					continue // content-derived drop, already pinned by the context route tests
				}
				t.Errorf("omission %s has no true-total source in this test; add one", section)
				continue
			}
			if section == "investigation.repositories_considered" && expected <= serviceStoryItemLimit {
				t.Errorf("fixture repository_count = %d, want past the %d-row cap so a wrong source shows", expected, serviceStoryItemLimit)
			}
			if expected <= serviceStoryItemLimit && !strings.HasPrefix(section, "investigation.") && !strings.HasPrefix(section, "code_to_runtime_trace.") && section != "evidence_graph.nodes" && section != "evidence_graph.edges" {
				t.Errorf("fixture pre-cap total for %s = %d, want past the %d-row cap so a wrong source shows", section, expected, serviceStoryItemLimit)
			}
			if got[section] != expected {
				t.Errorf("omission %s total = %d, want the pre-cap total %d", section, got[section], expected)
			}
		}
		for _, family := range shape.omitted {
			if want[family] <= serviceStoryItemLimit {
				t.Errorf("shape %s owns %s but its pre-cap total is %d, want past the %d-row cap", shape.name, family, want[family], serviceStoryItemLimit)
			}
			if _, ok := got[family]; !ok {
				t.Errorf("shape %s owns %s but truth.omissions has no entry for it", shape.name, family)
			}
		}
		for section, expected := range want {
			if expected == 0 {
				continue // a family another shape owns is absent from this one
			}
			if _, ok := got[section]; !ok && !strings.HasPrefix(section, "code_to_runtime_trace.") && !strings.HasPrefix(section, "deployment_evidence.") && !strings.HasPrefix(section, "investigation.repositories_with") {
				t.Errorf("no omission for %s; the fixture no longer exercises it", section)
			}
		}
	})
}

// storyRowTruthLabels are the per-row fields that say how much to trust a row,
// how it was established, or why it is not promoted. A handle row must keep
// each one its full row has. The per-family decisions over rows built by the
// production builders live in TestStoryHandlesKeepProductionRowTruthLabels;
// this test runs the same labels over every list of the large fixture.
var storyRowTruthLabels = []string{
	"confidence", "candidate_status", "missing_relationship", "relationship_basis",
	"resolution_mode", "evidence_source", "service_anchor_source",
	"service_anchor_status", "service_anchor_reason", "materialization_status",
	"relationship_types", "graph_relationship_types", "consumer_kinds", "evidence_kinds",
	"identity_outcome", "identity_strength", "sbom_attachment_status",
}

// TestStoryHandlesKeepRowTruthLabels proves the handles view drops row detail
// but keeps the per-row truth labels (#8798 review P2-3). The reused context
// route projection of deployment_evidence.artifacts is exempt: it keeps the
// artifact handle keys that trace_deployment_chain shares, and the full row is
// one get_relationship_evidence call away by resolved_id.
func TestStoryHandlesKeepRowTruthLabels(t *testing.T) {
	t.Parallel()

	forEachOversizedStory(t, func(t *testing.T, full, handles map[string]any, _ []querycontract.TruthOmission) {
		fullLists := map[string][]map[string]any{}
		walkRowLists("story", full, func(path string, rows []map[string]any) { fullLists[path] = rows })
		checked := 0
		walkRowLists("story", handles, func(path string, rows []map[string]any) {
			if path == "story.deployment_evidence.artifacts" {
				return
			}
			fullRows, ok := fullLists[path]
			if !ok {
				return
			}
			for i, row := range rows {
				if i >= len(fullRows) || row["repo_id"] == "__truncated__" {
					continue
				}
				for _, label := range storyRowTruthLabels {
					fullValue, present := fullRows[i][label]
					if !present || fullValue == "" || fullValue == nil {
						continue
					}
					checked++
					if !reflect.DeepEqual(row[label], fullValue) {
						t.Errorf("%s[%d].%s = %v under handles, want %v", path, i, label, row[label], fullValue)
					}
				}
			}
		})
		if checked == 0 {
			t.Fatal("no truth label was compared; the fixture no longer carries any")
		}
	})
}
