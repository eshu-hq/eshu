// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"bytes"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// sectionBudgetCeiling is 80% of the MCP dispatcher's 256 KiB
// defaultToolResponseByteBudget (mcpResponseByteBudget, copied in
// response_at_cap_size_test.go because importing package mcp would cycle).
const sectionBudgetCeiling = mcpResponseByteBudget * 8 / 10

// wantHandleKeys is the #7174 handle-row contract, written out here rather
// than read from the implementation so a drift in either side fails.
var wantHandleKeys = map[string][]string{
	"instances":                    {"instance_id", "environment", "platform_name", "platform_kind"},
	"deployment_sources":           {"repo_id", "repo_name", "relationship_type"},
	"cloud_resources":              {"id", "name", "kind"},
	"uncorrelated_cloud_resources": {"id", "name", "kind"},
	"k8s_resources":                {"entity_id", "entity_name", "kind"},
	"image_registry_truth":         {"image_ref", "digest"},
	"provisioned_platforms":        {"platform_id", "platform_name", "platform_kind"},
	"hostnames":                    {"hostname", "environment"},
	"dependents":                   {"repository", "repo_id"},
	"consumer_repositories":        {"repository", "repo_id"},
	"provisioning_source_chains":   {"repository", "repo_id"},
}

// wantDerivedOmitted are the families the handles default drops because each
// is a pure derivation of a primary family.
var wantDerivedOmitted = []string{
	"delivery_paths", "deployment_facts", "controller_driven_paths", "k8s_relationships",
	"topology_edges", "artifact_lineage", "network_paths", "entrypoints",
}

func atCapDefaultResponse(t *testing.T, sc testutil.TraceAtCapScenario) map[string]any {
	t.Helper()
	ctx := testutil.TraceAtCapWorkloadContext(sc)
	return BuildDeploymentTraceResponse("payments-api", ctx, testutil.TraceAtCapOverview(ctx, sc.OverviewCopiesRows))
}

// twoCopyEstimate mirrors the MCP dispatcher's accounting: the envelope as
// structuredContent plus the same envelope embedded as an escaped resource
// string. The resource-only fallback is deliberately not counted as fitting.
func twoCopyEstimate(t *testing.T, data map[string]any, omissions []querycontract.TruthOmission) int {
	t.Helper()
	// BuildTruthEnvelope needs the capability matrix loaded; a literal with
	// the same fields weighs the same on the wire.
	truth := querycontract.TruthEnvelope{
		Level: "derived", Capability: "platform_impact.deployment_chain", Profile: querycontract.ProfileProduction,
		Basis: querycontract.TruthBasisHybrid, Freshness: querycontract.TruthFreshness{State: "fresh"},
		Reason: "resolved from deployment topology and service evidence", Omissions: omissions,
	}
	encoded, err := json.Marshal(map[string]any{"data": data, "truth": truth, "error": nil})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	resourceCopy, _ := json.Marshal(string(encoded))
	return len(encoded) + len(resourceCopy)
}

func TestApplySectionSelectionHandlesDefaultFitsBudget(t *testing.T) {
	t.Parallel()

	sc := testutil.TraceAtCapScenario{Name: "default", PerFamily: 50, EnrichmentRows: 25, PlatformsPerInst: 1}
	resp := atCapDefaultResponse(t, sc)
	fullBytes := twoCopyEstimate(t, resp, nil)
	preLen := map[string]int{}
	for family := range wantHandleKeys {
		preLen[family] = len(querycontract.MapSliceValue(resp, family))
	}
	for _, family := range wantDerivedOmitted {
		preLen[family] = len(querycontract.MapSliceValue(resp, family))
	}

	omissions := ApplySectionSelection(resp, SectionSelection{EvidenceDetail: EvidenceDetailHandles})
	got := twoCopyEstimate(t, resp, omissions)
	t.Logf("two-copy counted bytes: full=%d handles=%d ceiling=%d (%.1f%% of %d)",
		fullBytes, got, sectionBudgetCeiling, 100*float64(got)/float64(mcpResponseByteBudget), mcpResponseByteBudget)
	logKeyBreakdown(t, resp)
	if got > sectionBudgetCeiling {
		t.Fatalf("handles default at cap = %d counted bytes, want <= %d (80%% of budget)", got, sectionBudgetCeiling)
	}
	if detail := querycontract.StringVal(resp, "evidence_detail"); detail != EvidenceDetailHandles {
		t.Fatalf("evidence_detail = %q, want handles", detail)
	}

	sectionDetail := querycontract.MapValue(resp, "section_detail")
	if len(sectionDetail) != len(SectionNames()) || len(SectionNames()) == 0 {
		t.Fatalf("section_detail has %d families, want every selectable family (%d)", len(sectionDetail), len(SectionNames()))
	}
	for family, keys := range wantHandleKeys {
		entry := querycontract.MapValue(sectionDetail, family)
		if total := querycontract.IntVal(entry, "total"); total != preLen[family] {
			t.Errorf("section_detail[%s].total = %d, want pre-projection length %d", family, total, preLen[family])
		}
		rows := querycontract.MapSliceValue(resp, family)
		if preLen[family] == 0 {
			continue
		}
		if querycontract.StringVal(entry, "detail") != EvidenceDetailHandles {
			t.Errorf("section_detail[%s].detail = %v, want handles", family, entry["detail"])
		}
		if len(rows) != preLen[family] || querycontract.IntVal(entry, "returned") != len(rows) {
			t.Errorf("%s: emitted %d rows (returned=%v), want all %d as handles", family, len(rows), entry["returned"], preLen[family])
		}
		for _, row := range rows {
			if gotKeys := rowKeys(row); !slices.Equal(gotKeys, sortedCopy(keys)) {
				t.Fatalf("%s handle row keys = %v, want %v", family, gotKeys, sortedCopy(keys))
			}
		}
		assertDrilldown(t, entry, family)
	}
	for _, family := range wantDerivedOmitted {
		if _, present := resp[family]; present {
			t.Errorf("derived family %s emitted under handles default; want key absent", family)
		}
		entry := querycontract.MapValue(sectionDetail, family)
		if querycontract.StringVal(entry, "detail") != "omitted" || querycontract.IntVal(entry, "total") != preLen[family] {
			t.Errorf("section_detail[%s] = %v, want omitted with total %d", family, entry, preLen[family])
		}
		assertDrilldown(t, entry, family)
	}
	assertOmissionListed(t, omissions, "delivery_paths", "omitted", preLen["delivery_paths"])
	assertOmissionListed(t, omissions, "instances", EvidenceDetailHandles, preLen["instances"])

	for _, entity := range querycontract.MapSliceValue(querycontract.MapValue(resp, "controller_overview"), "entities") {
		if gotKeys := rowKeys(entity); !slices.Equal(gotKeys, []string{"controller_kind", "entity_id", "entity_name"}) {
			t.Fatalf("controller_overview.entities handle keys = %v", gotKeys)
		}
	}
	evidence := querycontract.MapValue(resp, "deployment_evidence")
	if _, ok := evidence["evidence_index"]; ok {
		t.Fatal("deployment_evidence.evidence_index emitted under handles")
	}
	if querycontract.IntVal(evidence, "artifact_count") != sc.PerFamily {
		t.Fatalf("deployment_evidence.artifact_count = %v, want the full count %d", evidence["artifact_count"], sc.PerFamily)
	}
	for _, artifact := range querycontract.MapSliceValue(evidence, "artifacts") {
		if gotKeys := rowKeys(artifact); !slices.Equal(gotKeys, []string{"id", "relationship_type", "resolved_id"}) {
			t.Fatalf("deployment_evidence.artifacts handle keys = %v", gotKeys)
		}
	}
	api := querycontract.MapValue(resp, "api_surface")
	if _, ok := api["endpoints"]; ok || querycontract.IntVal(api, "endpoint_count") != sc.PerFamily {
		t.Fatalf("api_surface under handles = %v, want counts kept and endpoints dropped", api)
	}
	overview := querycontract.MapValue(resp, "deployment_overview")
	if querycontract.IntVal(overview, "instance_count") != sc.PerFamily || querycontract.IntVal(overview, "k8s_resource_count") != sc.PerFamily {
		t.Fatalf("deployment_overview counts changed by projection: %v", overview)
	}
	for _, key := range []string{"service_name", "workload_id", "subject", "image_refs", "deployment_fact_summary", "drilldowns", "story"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("always-emitted key %s missing under handles", key)
		}
	}
}

// TestApplySectionSelectionLogsNonDefaultWorstCase records, without
// asserting, the two-copy size of the non-default worst case under handles.
func TestApplySectionSelectionLogsNonDefaultWorstCase(t *testing.T) {
	t.Parallel()

	for _, sc := range []testutil.TraceAtCapScenario{
		{Name: "enrichment 50, overview copies", PerFamily: 50, EnrichmentRows: 50, PlatformsPerInst: 1, OverviewCopiesRows: true},
		{Name: "worst: 5 platforms/instance, enrichment 100", PerFamily: 50, EnrichmentRows: 100, PlatformsPerInst: 5},
	} {
		resp := atCapDefaultResponse(t, sc)
		full := twoCopyEstimate(t, resp, nil)
		omissions := ApplySectionSelection(resp, SectionSelection{EvidenceDetail: EvidenceDetailHandles})
		got := twoCopyEstimate(t, resp, omissions)
		t.Logf("%s: full=%d handles=%d two-copy bytes (%.1f%% of %d)", sc.Name, full, got, 100*float64(got)/float64(mcpResponseByteBudget), mcpResponseByteBudget)
	}
}

func TestApplySectionSelectionFullIsByteIdenticalToToday(t *testing.T) {
	t.Parallel()

	for name, build := range map[string]func() map[string]any{
		"sample dossier": func() map[string]any {
			return BuildDeploymentTraceResponse("sample-service-api", testutil.SampleServiceDossierContext(), map[string]any{})
		},
		"at cap": func() map[string]any {
			return atCapDefaultResponse(t, testutil.TraceAtCapScenario{PerFamily: 50, EnrichmentRows: 25, PlatformsPerInst: 1})
		},
	} {
		today, _ := json.Marshal(build())
		shaped := build()
		omissions := ApplySectionSelection(shaped, SectionSelection{})
		if len(omissions) != 0 {
			t.Fatalf("%s: full mode returned omissions %v, want none", name, omissions)
		}
		if querycontract.StringVal(shaped, "evidence_detail") != EvidenceDetailFull {
			t.Fatalf("%s: evidence_detail = %v, want full", name, shaped["evidence_detail"])
		}
		for family, entry := range querycontract.MapValue(shaped, "section_detail") {
			detail, _ := entry.(map[string]any)
			if detail["detail"] != EvidenceDetailFull {
				t.Fatalf("%s: section_detail[%s] = %v, want full", name, family, detail)
			}
			if _, ok := detail["drilldown_tool"]; ok {
				t.Fatalf("%s: section_detail[%s] carries a drilldown in full mode", name, family)
			}
		}
		delete(shaped, "evidence_detail")
		delete(shaped, "section_detail")
		after, _ := json.Marshal(shaped)
		if !bytes.Equal(today, after) {
			t.Fatalf("%s: full mode changed the response beyond evidence_detail/section_detail", name)
		}
	}
}

func TestApplySectionSelectionExplicitSections(t *testing.T) {
	t.Parallel()

	resp := atCapDefaultResponse(t, testutil.TraceAtCapScenario{PerFamily: 50, EnrichmentRows: 25, PlatformsPerInst: 1})
	omissions := ApplySectionSelection(resp, SectionSelection{EvidenceDetail: EvidenceDetailFull, Sections: []string{"delivery_paths", "k8s_resources"}})
	if len(querycontract.MapSliceValue(resp, "delivery_paths")) == 0 {
		t.Fatal("selected derived family delivery_paths not emitted")
	}
	if rows := querycontract.MapSliceValue(resp, "k8s_resources"); len(rows) == 0 || len(rows[0]) <= 3 {
		t.Fatalf("selected k8s_resources not emitted in full: %v", rows)
	}
	for _, family := range []string{"instances", "deployment_facts", "story", "deployment_overview", "controller_overview"} {
		if _, ok := resp[family]; ok {
			t.Errorf("unselected %s emitted", family)
		}
	}
	for _, key := range []string{"service_name", "image_refs", "deployment_fact_summary", "drilldowns", "k8s_resource_limits", "section_detail"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("always-emitted key %s missing under explicit sections", key)
		}
	}
	assertOmissionListed(t, omissions, "instances", "omitted", 50)
	for _, omission := range omissions {
		if omission.Section == "delivery_paths" || omission.Section == "k8s_resources" {
			t.Fatalf("selected family %s reported as omitted", omission.Section)
		}
	}
}

func TestSectionSelectionValidate(t *testing.T) {
	t.Parallel()

	if err := (SectionSelection{}).Validate(); err != nil {
		t.Fatalf("empty selection: %v", err)
	}
	if err := (SectionSelection{EvidenceDetail: "handles", Sections: SectionNames()}).Validate(); err != nil {
		t.Fatalf("every known section: %v", err)
	}
	err := (SectionSelection{Sections: []string{"instances", "bogus"}}).Validate()
	if err == nil || !strings.Contains(err.Error(), "bogus") || !strings.Contains(err.Error(), "delivery_paths") {
		t.Fatalf("unknown section error = %v, want it to name the value and the allowed values", err)
	}
	err = (SectionSelection{EvidenceDetail: "summary"}).Validate()
	if err == nil || !strings.Contains(err.Error(), "full") || !strings.Contains(err.Error(), "handles") {
		t.Fatalf("unknown evidence_detail error = %v, want it to name the allowed values", err)
	}
}

// logKeyBreakdown logs the ten heaviest response keys for the evidence note.
func logKeyBreakdown(t *testing.T, resp map[string]any) {
	t.Helper()
	type keySize struct {
		key   string
		bytes int
	}
	sizes := make([]keySize, 0, len(resp))
	for key, value := range resp {
		encoded, _ := json.Marshal(value)
		sizes = append(sizes, keySize{key, len(encoded)})
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i].bytes > sizes[j].bytes })
	for i, size := range sizes {
		if i >= 10 {
			break
		}
		t.Logf("  %2d. %-28s %7d B", i+1, size.key, size.bytes)
	}
}

func assertDrilldown(t *testing.T, entry map[string]any, family string) {
	t.Helper()
	if querycontract.StringVal(entry, "drilldown_tool") != "trace_deployment_chain" {
		t.Errorf("section_detail[%s].drilldown_tool = %v", family, entry["drilldown_tool"])
		return
	}
	args := querycontract.MapValue(entry, "drilldown_arguments")
	sections, _ := args["sections"].([]string)
	if args["service_name"] != "payments-api" || args["evidence_detail"] != EvidenceDetailFull || !slices.Equal(sections, []string{family}) {
		t.Errorf("section_detail[%s].drilldown_arguments = %v", family, args)
	}
}

func assertOmissionListed(t *testing.T, omissions []querycontract.TruthOmission, section, detail string, total int) {
	t.Helper()
	for _, omission := range omissions {
		if omission.Section == section {
			if omission.Detail != detail || omission.Total != total {
				t.Fatalf("omission %s = %+v, want detail %s total %d", section, omission, detail, total)
			}
			return
		}
	}
	t.Fatalf("truth omissions %v do not list %s", omissions, section)
}

func rowKeys(row map[string]any) []string {
	keys := make([]string, 0, len(row))
	for key := range row {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedCopy(values []string) []string {
	out := slices.Clone(values)
	sort.Strings(out)
	return out
}

// TestApplySectionSelectionDrilldownReplaysTheOriginalRequest pins review F1
// of #7174. A drilldown that carries only service_name, sections and
// evidence_detail is replayed by the MCP adapter with its own defaults
// (direct_only=true, max_depth=0), so a family built only for the caller's
// non-default arguments (consumer_repositories needs direct_only=false) comes
// back absent and section_detail reports it as full with total 0. The
// drilldown must carry the arguments that decide which rows exist.
func TestApplySectionSelectionDrilldownReplaysTheOriginalRequest(t *testing.T) {
	t.Parallel()

	response := atCapDefaultResponse(t, testutil.TraceAtCapScenario{Name: "replay", PerFamily: 5, EnrichmentRows: 5, PlatformsPerInst: 1})
	replay := map[string]any{"direct_only": false, "max_depth": 10, "include_related_module_usage": true}
	ApplySectionSelection(response, SectionSelection{EvidenceDetail: EvidenceDetailHandles, Replay: replay})

	entry := querycontract.MapValue(querycontract.MapValue(response, "section_detail"), "consumer_repositories")
	args := querycontract.MapValue(entry, "drilldown_arguments")
	if args["direct_only"] != false || args["max_depth"] != 10 || args["include_related_module_usage"] != true {
		t.Fatalf("drilldown_arguments = %v, want the caller's direct_only=false max_depth=10 include_related_module_usage=true", args)
	}
	if args["evidence_detail"] != EvidenceDetailFull || args["service_name"] != "payments-api" {
		t.Fatalf("drilldown_arguments = %v, want service_name and evidence_detail full kept", args)
	}
}

// TestApplySectionSelectionCountsAndDropsContentDerivedEvidenceLists pins
// review F3: when there is no graph evidence, deployment_evidence carries lists
// built from repository content (shared_config_paths, delivery_paths, and so
// on) with no row cap. Handles mode drops those lists and section_detail counts
// them, so the total and returned figures stay truthful and the size bound
// does not depend on how many files the repository has.
func TestApplySectionSelectionCountsAndDropsContentDerivedEvidenceLists(t *testing.T) {
	t.Parallel()

	build := func() map[string]any {
		return map[string]any{
			"service_name": "payments-api",
			"deployment_evidence": map[string]any{
				"artifacts": []map[string]any{
					{"id": "a1", "relationship_type": "DEPLOYS_FROM", "resolved_id": "r1", "name": "long"},
					{"id": "a2", "relationship_type": "DEPLOYS_FROM", "resolved_id": "r2", "name": "long"},
				},
				"shared_config_paths": []any{"config/a.yaml", "config/b.yaml", "config/c.yaml"},
				"delivery_paths":      []any{map[string]any{"path": "x"}, map[string]any{"path": "y"}, map[string]any{"path": "z"}, map[string]any{"path": "w"}},
				// deployment_artifacts is a map of lists in production
				// (repositoryartifacts.MergeDeploymentArtifactMaps), and the
				// story keys are []string (repository/deployment_overview_story.go).
				"deployment_artifacts": map[string]any{
					"controller_artifacts": []any{map[string]any{"path": "c1"}, map[string]any{"path": "c2"}},
					"workflow_artifacts":   []any{map[string]any{"path": "w1"}},
					"config_paths":         []any{map[string]any{"path": "p1"}, map[string]any{"path": "p2"}},
				},
				"topology_story":        []string{"story one", "story two"},
				"delivery_family_story": []string{"family one"},
			},
		}
	}

	handles := build()
	ApplySectionSelection(handles, SectionSelection{EvidenceDetail: EvidenceDetailHandles})
	evidence := querycontract.MapValue(handles, "deployment_evidence")
	for _, key := range []string{"shared_config_paths", "delivery_paths", "deployment_artifacts", "topology_story", "delivery_family_story"} {
		if _, present := evidence[key]; present {
			t.Errorf("handles mode kept the content-derived value %s", key)
		}
	}
	entry := querycontract.MapValue(querycontract.MapValue(handles, "section_detail"), "deployment_evidence")
	// 2 graph artifacts + 3 config paths + 4 delivery paths + 5 rows in the
	// deployment_artifacts map + 2 topology sentences + 1 family sentence.
	if entry["detail"] != EvidenceDetailHandles || entry["total"] != 17 || entry["returned"] != 2 {
		t.Errorf("section_detail.deployment_evidence = %v, want handles, total 17, returned 2", entry)
	}

	full := build()
	ApplySectionSelection(full, SectionSelection{EvidenceDetail: EvidenceDetailFull})
	entry = querycontract.MapValue(querycontract.MapValue(full, "section_detail"), "deployment_evidence")
	if entry["detail"] != EvidenceDetailFull || entry["total"] != 17 || entry["returned"] != 17 {
		t.Errorf("full mode section_detail.deployment_evidence = %v, want full, total 17, returned 17", entry)
	}
}

// TestApplySectionSelectionImageRegistryHandleKeepsTheAmbiguityQualifier pins
// review F5: an ambiguous registry row has no digest, so a handle of
// {image_ref, digest} would read as a plain unresolved image. match_strength
// travels with it.
func TestApplySectionSelectionImageRegistryHandleKeepsTheAmbiguityQualifier(t *testing.T) {
	t.Parallel()

	response := map[string]any{
		"service_name": "payments-api",
		"image_registry_truth": []map[string]any{
			{"image_ref": "registry/app:1", "digest": "sha256:aa", "match_strength": "exact", "note": "long"},
			{"image_ref": "registry/app:2", "ambiguous": true, "match_strength": "ambiguous", "digest_candidates": []any{"sha256:bb", "sha256:cc"}},
		},
	}
	ApplySectionSelection(response, SectionSelection{EvidenceDetail: EvidenceDetailHandles})
	rows := response["image_registry_truth"].([]map[string]any)
	if rows[1]["match_strength"] != "ambiguous" || rows[0]["match_strength"] != "exact" {
		t.Fatalf("image_registry_truth handles = %v, want match_strength kept on every row", rows)
	}
	if _, leaked := rows[1]["digest_candidates"]; leaked {
		t.Fatalf("handle leaked the digest_candidates list: %v", rows[1])
	}
}
