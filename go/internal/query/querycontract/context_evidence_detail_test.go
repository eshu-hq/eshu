// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func evidenceDetailContext() map[string]any {
	artifacts := []map[string]any{
		{"id": "a1", "relationship_type": "DEPLOYS_FROM", "resolved_id": "r1", "path": "deploy/a.yaml", "extractor": "x"},
		{"id": "a2", "relationship_type": "PROVISIONS_DEPENDENCY_FOR", "resolved_id": "r2", "path": "deploy/b.yaml", "extractor": "y"},
	}
	endpoints := []map[string]any{
		{"id": "e1", "path": "/v1/a", "methods": []string{"get"}, "source_paths": []string{"spec.yaml"}, "operation_ids": []string{"getA"}},
	}
	return map[string]any{
		"api_surface":         map[string]any{"endpoint_count": 78, "endpoints": endpoints, "spec_paths": []string{"spec.yaml"}},
		"deployment_evidence": map[string]any{"artifact_count": 2, "artifacts": artifacts, "evidence_index": map[string]any{"lookup_basis": "resolved_id"}, "environments": []string{"qa"}},
		"result_limits":       map[string]any{"artifact_count": 2},
	}
}

// TestApplyContextEvidenceDetailHandlesProjectsRowsToIdentityKeys proves
// handles mode keeps only the identity keys of artifact and endpoint rows,
// keeps every count, drops the evidence_index regrouping, and reports each
// reduced family as a truth omission with its true total (#7129).
func TestApplyContextEvidenceDetailHandlesProjectsRowsToIdentityKeys(t *testing.T) {
	t.Parallel()

	ctx := evidenceDetailContext()

	omissions := ApplyContextEvidenceDetail(ctx, ContextEvidenceDetailHandles)

	artifacts := MapSliceValue(MapValue(ctx, "deployment_evidence"), "artifacts")
	if len(artifacts) != 2 {
		t.Fatalf("artifacts len = %d, want 2 (every row stays as a handle)", len(artifacts))
	}
	for _, row := range artifacts {
		if len(row) != 3 || row["id"] == nil || row["relationship_type"] == nil || row["resolved_id"] == nil {
			t.Fatalf("artifact row = %#v, want only id, relationship_type, resolved_id", row)
		}
	}
	endpoints := MapSliceValue(MapValue(ctx, "api_surface"), "endpoints")
	if len(endpoints) != 1 || len(endpoints[0]) != 3 || endpoints[0]["path"] != "/v1/a" || endpoints[0]["methods"] == nil {
		t.Fatalf("endpoint rows = %#v, want only id, path, methods", endpoints)
	}
	if _, has := MapValue(ctx, "deployment_evidence")["evidence_index"]; has {
		t.Fatal("evidence_index present in handles mode; its rows are regrouped handles already")
	}
	if got := IntVal(MapValue(ctx, "deployment_evidence"), "artifact_count"); got != 2 {
		t.Fatalf("artifact_count = %d, want 2 (counts stay)", got)
	}
	if got := IntVal(MapValue(ctx, "api_surface"), "endpoint_count"); got != 78 {
		t.Fatalf("endpoint_count = %d, want 78 (counts stay)", got)
	}
	if got := StringVal(ctx, "evidence_detail"); got != ContextEvidenceDetailHandles {
		t.Fatalf("evidence_detail = %q, want %q", got, ContextEvidenceDetailHandles)
	}
	want := map[string]int{"deployment_evidence.artifacts": 2, "api_surface.endpoints": 78}
	if len(omissions) != len(want) {
		t.Fatalf("omissions = %#v, want one per reduced family", omissions)
	}
	for _, omission := range omissions {
		total, ok := want[omission.Section]
		if !ok || omission.Detail != ContextEvidenceDetailHandles || omission.Total != total {
			t.Fatalf("omission = %#v, want handles with total %d for a known section", omission, total)
		}
	}
	drilldown := MapValue(ctx, "evidence_detail_drilldown")
	if drilldown == nil {
		t.Fatal("evidence_detail_drilldown absent; handles mode must say how to fetch the full rows")
	}
	if text := StringVal(drilldown, "full_rows"); !strings.Contains(text, fmt.Sprintf("past the %d shipped", ContextStoryItemLimit)) {
		t.Fatalf("drilldown full_rows = %q, want it to name the row limit %d", text, ContextStoryItemLimit)
	}
}

// TestApplyContextEvidenceDetailReportsRowsReadNotRowsShipped proves the
// artifact omission total is the row count the read held, not the number of
// rows left after the 50-row cap. On the graph fallback 100 rows are read and 50
// ship; reporting 50 would read as a complete answer.
func TestApplyContextEvidenceDetailReportsRowsReadNotRowsShipped(t *testing.T) {
	t.Parallel()

	ctx := evidenceDetailContext()
	ctx["result_limits"] = map[string]any{"artifact_count": 100}

	omissions := ApplyContextEvidenceDetail(ctx, ContextEvidenceDetailHandles)

	for _, omission := range omissions {
		if omission.Section == "deployment_evidence.artifacts" && omission.Total != 100 {
			t.Fatalf("artifacts omission total = %d, want 100 (rows read, 2 shipped)", omission.Total)
		}
	}
}

// contentEvidenceContext adds the content-derived deployment_evidence values,
// which have no row cap of their own on the context routes, to the base fixture.
// delivery_paths is one the row cap already cut to 50 of 120.
func contentEvidenceContext() map[string]any {
	ctx := evidenceDetailContext()
	evidence := MapValue(ctx, "deployment_evidence")
	evidence["deployment_artifacts"] = map[string]any{
		"controller_artifacts": []map[string]any{{"path": "a"}, {"path": "b"}, {"path": "c"}},
		"config_paths":         []map[string]any{{"path": "d"}, {"path": "e"}},
	}
	evidence["delivery_family_paths"] = []map[string]any{{"path": "p1"}, {"path": "p2"}, {"path": "p3"}, {"path": "p4"}}
	evidence["delivery_family_story"] = []string{"s1", "s2"}
	evidence["topology_story"] = []string{"t1"}
	evidence["relationship_overview"] = map[string]any{"relationship_count": 7, "relationships": []map[string]any{{"id": "r"}}}
	evidence["delivery_paths"] = budgetRows("path", 50)
	evidence["raw_limits"] = map[string]any{"delivery_paths": map[string]any{"count": 120, "limit": 50, "truncated": true}}
	evidence["empty_story"] = []string{}
	return ctx
}

// TestApplyContextEvidenceDetailHandlesDropsContentDerivedValues proves handles
// mode drops the content-derived deployment_evidence values that have no row cap
// of their own, reports each as an omitted section with the rows it held (the
// pre-cut count for a list the row cap already cut), and leaves a value that
// holds no rows alone. This bounds the MCP default constructively instead of
// leaving the uncapped families to headroom (#7129).
func TestApplyContextEvidenceDetailHandlesDropsContentDerivedValues(t *testing.T) {
	t.Parallel()

	ctx := contentEvidenceContext()

	omissions := ApplyContextEvidenceDetail(ctx, ContextEvidenceDetailHandles)

	evidence := MapValue(ctx, "deployment_evidence")
	for _, key := range []string{"deployment_artifacts", "delivery_family_paths", "delivery_family_story", "topology_story", "relationship_overview", "delivery_paths"} {
		if _, has := evidence[key]; has {
			t.Fatalf("deployment_evidence.%s present in handles mode, want it dropped", key)
		}
	}
	if _, has := evidence["empty_story"]; !has {
		t.Fatal("a value that holds no rows was dropped")
	}
	want := map[string]int{
		"deployment_evidence.deployment_artifacts":  5,
		"deployment_evidence.delivery_family_paths": 4,
		"deployment_evidence.delivery_family_story": 2,
		"deployment_evidence.topology_story":        1,
		"deployment_evidence.relationship_overview": 7,
		"deployment_evidence.delivery_paths":        120,
	}
	got := map[string]TruthOmission{}
	for _, omission := range omissions {
		got[omission.Section] = omission
	}
	for section, total := range want {
		omission, ok := got[section]
		if !ok || omission.Detail != "omitted" || omission.Total != total {
			t.Fatalf("omission for %s = %#v, want detail omitted and total %d", section, omission, total)
		}
	}
}

// TestApplyContextEvidenceDetailHandlesLeavesSharedContentValues proves the
// drop is applied to a copy, so a map shared with a read model keeps its values.
func TestApplyContextEvidenceDetailHandlesLeavesSharedContentValues(t *testing.T) {
	t.Parallel()

	ctx := contentEvidenceContext()
	shared := MapValue(ctx, "deployment_evidence")

	ApplyContextEvidenceDetail(ctx, ContextEvidenceDetailHandles)

	for _, key := range []string{"deployment_artifacts", "delivery_family_paths", "topology_story"} {
		if _, has := shared[key]; !has {
			t.Fatalf("shared evidence map lost %s", key)
		}
	}
}

// TestApplyContextEvidenceDetailHandlesKeepsEmptyContentValues proves a
// content-derived value that holds no rows stays, with no omission, so an
// empty list never reads as a reduced family with a total of zero.
func TestApplyContextEvidenceDetailHandlesKeepsEmptyContentValues(t *testing.T) {
	t.Parallel()

	ctx := evidenceDetailContext()
	evidence := MapValue(ctx, "deployment_evidence")
	evidence["topology_story"] = []string{}
	evidence["delivery_family_story"] = []string{}
	evidence["deployment_artifacts"] = map[string]any{"controller_artifacts": []map[string]any{}}

	omissions := ApplyContextEvidenceDetail(ctx, ContextEvidenceDetailHandles)

	kept := MapValue(ctx, "deployment_evidence")
	for _, key := range []string{"topology_story", "delivery_family_story", "deployment_artifacts"} {
		if _, has := kept[key]; !has {
			t.Fatalf("deployment_evidence.%s dropped although it holds no rows", key)
		}
	}
	for _, omission := range omissions {
		switch omission.Section {
		case "deployment_evidence.topology_story", "deployment_evidence.delivery_family_story", "deployment_evidence.deployment_artifacts":
			t.Fatalf("omission %#v reported for a value that holds no rows", omission)
		}
	}
}

// TestApplyContextEvidenceDetailHandlesDropsContentValuesWithoutArtifacts
// proves the content-derived drop works when there are no graph artifacts, and
// that it still copies: the evidence map a caller holds keeps its values.
func TestApplyContextEvidenceDetailHandlesDropsContentValuesWithoutArtifacts(t *testing.T) {
	t.Parallel()

	shared := map[string]any{
		"artifact_count": 0,
		"topology_story": []string{"t1", "t2"},
	}
	ctx := map[string]any{"deployment_evidence": shared}

	omissions := ApplyContextEvidenceDetail(ctx, ContextEvidenceDetailHandles)

	if _, has := MapValue(ctx, "deployment_evidence")["topology_story"]; has {
		t.Fatal("topology_story present in handles mode, want it dropped")
	}
	if _, has := shared["topology_story"]; !has {
		t.Fatal("the original evidence map lost topology_story; the drop must act on a copy")
	}
	if len(omissions) != 1 || omissions[0].Section != "deployment_evidence.topology_story" || omissions[0].Total != 2 {
		t.Fatalf("omissions = %#v, want one for topology_story with total 2", omissions)
	}
}

// TestApplyContextEvidenceDetailFullKeepsContentDerivedValues proves full mode
// keeps every content-derived value and reports nothing for them.
func TestApplyContextEvidenceDetailFullKeepsContentDerivedValues(t *testing.T) {
	t.Parallel()

	ctx := contentEvidenceContext()

	omissions := ApplyContextEvidenceDetail(ctx, ContextEvidenceDetailFull)

	if len(omissions) != 0 {
		t.Fatalf("omissions = %#v, want none", omissions)
	}
	if _, has := MapValue(ctx, "deployment_evidence")["deployment_artifacts"]; !has {
		t.Fatal("deployment_artifacts dropped in full mode")
	}
}

// TestApplyContextEvidenceDetailFullChangesNothing proves full mode (the HTTP
// default) keeps every row and reports no omission.
func TestApplyContextEvidenceDetailFullChangesNothing(t *testing.T) {
	t.Parallel()

	for _, detail := range []string{"", ContextEvidenceDetailFull} {
		ctx := evidenceDetailContext()

		omissions := ApplyContextEvidenceDetail(ctx, detail)

		if len(omissions) != 0 {
			t.Fatalf("detail %q: omissions = %#v, want none", detail, omissions)
		}
		artifacts := MapSliceValue(MapValue(ctx, "deployment_evidence"), "artifacts")
		if len(artifacts[0]) != 5 {
			t.Fatalf("detail %q: artifact row = %#v, want every field", detail, artifacts[0])
		}
		if _, has := MapValue(ctx, "deployment_evidence")["evidence_index"]; !has {
			t.Fatalf("detail %q: evidence_index dropped in full mode", detail)
		}
		if got := StringVal(ctx, "evidence_detail"); got != ContextEvidenceDetailFull {
			t.Fatalf("detail %q: evidence_detail = %q, want full", detail, got)
		}
		if _, has := ctx["evidence_detail_drilldown"]; has {
			t.Fatalf("detail %q: drilldown present in full mode", detail)
		}
	}
}

// TestApplyContextEvidenceDetailDoesNotMutateSharedMaps proves handles mode
// projects into new maps, so a map shared with a read model keeps its rows.
func TestApplyContextEvidenceDetailDoesNotMutateSharedMaps(t *testing.T) {
	t.Parallel()

	ctx := evidenceDetailContext()
	sharedEvidence := MapValue(ctx, "deployment_evidence")
	sharedSurface := MapValue(ctx, "api_surface")

	ApplyContextEvidenceDetail(ctx, ContextEvidenceDetailHandles)

	if got := len(MapSliceValue(sharedEvidence, "artifacts")[0]); got != 5 {
		t.Fatalf("shared artifact row has %d fields, want 5 (untouched)", got)
	}
	if _, has := sharedEvidence["evidence_index"]; !has {
		t.Fatal("shared evidence map lost evidence_index")
	}
	if got := len(MapSliceValue(sharedSurface, "endpoints")[0]); got != 5 {
		t.Fatalf("shared endpoint row has %d fields, want 5 (untouched)", got)
	}
}

// TestApplyContextEvidenceDetailHandlesSkipsAbsentFamilies proves a context
// without evidence or an API surface reports no omission and gains no key.
func TestApplyContextEvidenceDetailHandlesSkipsAbsentFamilies(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{"name": "svc"}

	omissions := ApplyContextEvidenceDetail(ctx, ContextEvidenceDetailHandles)

	if len(omissions) != 0 {
		t.Fatalf("omissions = %#v, want none", omissions)
	}
	if _, has := ctx["deployment_evidence"]; has {
		t.Fatal("deployment_evidence appeared on a context that had none")
	}
	if _, has := ctx["evidence_detail_drilldown"]; has {
		t.Fatal("drilldown present although nothing was reduced")
	}
}

// TestValidateContextEvidenceDetail proves an unknown value is rejected with a
// named error that lists the accepted values.
func TestValidateContextEvidenceDetail(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"", "full", "handles"} {
		if err := ValidateContextEvidenceDetail(ok); err != nil {
			t.Fatalf("ValidateContextEvidenceDetail(%q) = %v, want nil", ok, err)
		}
	}
	err := ValidateContextEvidenceDetail("compact")
	if !errors.Is(err, ErrInvalidContextEvidenceDetail) {
		t.Fatalf("ValidateContextEvidenceDetail(compact) = %v, want ErrInvalidContextEvidenceDetail", err)
	}
	if msg := err.Error(); !containsAll(msg, "compact", "full", "handles") {
		t.Fatalf("error %q does not name the bad and accepted values", msg)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		found := false
		for i := 0; i+len(part) <= len(s); i++ {
			if s[i:i+len(part)] == part {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
