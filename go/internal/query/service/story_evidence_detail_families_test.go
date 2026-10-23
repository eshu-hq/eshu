// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestEntrypointFamiliesStoryHandlesProjectRows proves the handles view ships
// entrypoint_candidates and network_paths as identity rows. The dossier builds
// both lists at the 50-row cap with full-width rows, so before the projection
// a service heavy in both shipped 100 unshaped rows under handles (#8798). The
// lists keep their length, keep the labels that explain a row, and report the
// pre-cap total from raw_context_limits as the omission total.
func TestEntrypointFamiliesStoryHandlesProjectRows(t *testing.T) {
	t.Parallel()

	full := BuildServiceStoryResponse("checkout-service", testutil.OversizedServiceDossierWithEntrypointFamilies())
	handles := BuildServiceStoryResponse("checkout-service", testutil.OversizedServiceDossierWithEntrypointFamilies())
	omissions := querycontract.ApplyStoryEvidenceDetail(handles, querycontract.ContextEvidenceDetailHandles)

	families := []struct {
		key     string
		kept    []string
		dropped []string
	}{
		{"entrypoint_candidates", []string{"candidate", "classification", "reason"}, []string{"relative_path"}},
		{"network_paths", []string{"path_type", "from_type", "from", "to", "platform_kind", "environment", "visibility"}, []string{"to_type", "reason"}},
	}
	bySection := map[string]querycontract.TruthOmission{}
	for _, omission := range omissions {
		bySection[omission.Section] = omission
	}
	for _, family := range families {
		fullRows := querycontract.MapSliceValue(full, family.key)
		rows := querycontract.MapSliceValue(handles, family.key)
		if len(fullRows) != serviceStoryItemLimit {
			t.Fatalf("full %s = %d rows, want the %d-row cap so the fixture exercises it", family.key, len(fullRows), serviceStoryItemLimit)
		}
		if len(rows) != len(fullRows) {
			t.Fatalf("handles %s = %d rows, want %d (a list keeps its length)", family.key, len(rows), len(fullRows))
		}
		for i, row := range rows {
			for _, key := range family.kept {
				if row[key] != fullRows[i][key] || row[key] == nil {
					t.Fatalf("handles %s[%d].%s = %v, want the full row's %v", family.key, i, key, row[key], fullRows[i][key])
				}
			}
			for _, key := range family.dropped {
				if _, has := row[key]; has {
					t.Fatalf("handles %s[%d] still ships %s; want identity keys only", family.key, i, key)
				}
			}
		}
		limit := querycontract.MapValue(querycontract.MapValue(handles, "raw_context_limits"), family.key)
		if querycontract.IntVal(limit, "count") != 60 || !querycontract.BoolVal(limit, "truncated") {
			t.Fatalf("raw_context_limits.%s = %#v, want count 60 and truncated kept", family.key, limit)
		}
		omission, ok := bySection[family.key]
		if !ok || omission.Detail != querycontract.ContextEvidenceDetailHandles || omission.Total != 60 {
			t.Fatalf("omission for %s = %#v (present %v), want detail handles and the pre-cap total 60", family.key, omission, ok)
		}
	}

	want, _ := json.Marshal(querycontract.MapValue(full, "deployment_overview"))
	got, _ := json.Marshal(querycontract.MapValue(handles, "deployment_overview"))
	if string(got) != string(want) {
		t.Fatalf("deployment_overview changed under handles:\n got  %s\n want %s", got, want)
	}
}

// TestEntrypointFamiliesStoryHandlesMCPSize measures the story when every
// family, including entrypoint_candidates and network_paths, sits at its
// 50-row cap. It gates the single-copy size, which a single-copy result shape
// (#8800) sends and which the dispatch resource-only fallback sends today, and
// logs the dual size and the family bytes. The dual size is past the budget in
// this shape, which is why the budget doc names these two lists: the next
// family that grows shows up in this log line before it shows up in a tool
// response.
func TestEntrypointFamiliesStoryHandlesMCPSize(t *testing.T) {
	t.Parallel()

	full := BuildServiceStoryResponse("checkout-service", testutil.OversizedServiceDossierWithEntrypointFamilies())
	handles := BuildServiceStoryResponse("checkout-service", testutil.OversizedServiceDossierWithEntrypointFamilies())
	omissions := querycontract.ApplyStoryEvidenceDetail(handles, querycontract.ContextEvidenceDetailHandles)
	dual := mcpResultBytes(t, handles, omissions)
	textCopy := marshalStoryText(t, handles, omissions)
	for _, key := range []string{"entrypoint_candidates", "network_paths"} {
		fullRaw, _ := json.Marshal(full[key])
		handlesRaw, _ := json.Marshal(handles[key])
		t.Logf("%s bytes: full=%d handles=%d", key, len(fullRaw), len(handlesRaw))
		if len(handlesRaw) >= len(fullRaw) {
			t.Fatalf("%s = %d bytes under handles, want fewer than the %d under full", key, len(handlesRaw), len(fullRaw))
		}
	}
	t.Logf("all-families story: full data=%d bytes, handles data=%d bytes, MCP dual result=%d bytes, structured copy=%d bytes, escaped text copy=%d bytes, budget=%d",
		marshalStory(t, full), marshalStory(t, handles), dual, dual-textCopy, textCopy, storyMCPResponseBudget)
	if textCopy+storyMCPWrapperBytes > storyMCPResponseBudget {
		t.Fatalf("all-families escaped single copy = %d bytes plus %d wrapper, want <= the %d-byte budget",
			textCopy, storyMCPWrapperBytes, storyMCPResponseBudget)
	}
}
