// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// mcpResponseByteBudget mirrors go/internal/mcp defaultToolResponseByteBudget
// (256 KiB). It is copied, not imported: package mcp imports package query,
// which reaches this package, so importing it here would be an import cycle.
const mcpResponseByteBudget = 256 * 1024

type keyBytes struct {
	key   string
	bytes int
}

// TestResponseAtCapSize is a measurement, not an assertion on a bound: it
// logs the serialized size of the trace_deployment_chain response with every
// family populated at its documented cap (#7174).
func TestResponseAtCapSize(t *testing.T) {
	scenarios := []testutil.TraceAtCapScenario{
		{Name: "at-cap, 1 platform/instance, enrichment 25 (default max_depth)", PerFamily: 50, EnrichmentRows: 25, PlatformsPerInst: 1},
		{Name: "at-cap, 1 platform/instance, enrichment 50", PerFamily: 50, EnrichmentRows: 50, PlatformsPerInst: 1},
		{Name: "at-cap, 1 platform/instance, enrichment 50, overview carries hostname/entrypoint/api copies", PerFamily: 50, EnrichmentRows: 50, PlatformsPerInst: 1, OverviewCopiesRows: true},
		{Name: "worst, 5 platforms/instance (<=2500 route cap), enrichment 100 (max_depth clamp)", PerFamily: 50, EnrichmentRows: 100, PlatformsPerInst: 5},
	}
	for _, sc := range scenarios {
		ctx := testutil.TraceAtCapWorkloadContext(sc)
		resp := BuildDeploymentTraceResponse("payments-api", ctx, testutil.TraceAtCapOverview(ctx, sc.OverviewCopiesRows))
		envelope := map[string]any{"data": resp, "truth": map[string]any{
			"level": "derived", "profile": "production",
			"freshness": map[string]any{"state": "fresh"},
		}, "error": nil}
		encoded, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("%s: marshal envelope: %v", sc.Name, err)
		}
		// resource copy is the envelope JSON embedded as a JSON string (escaped).
		resourceCopy, _ := json.Marshal(string(encoded))
		est := len(encoded) + len(resourceCopy) // structuredContent + resource.text; summary text excluded
		sizes := make([]keyBytes, 0, len(resp))
		for k, v := range resp {
			b, _ := json.Marshal(v)
			sizes = append(sizes, keyBytes{k, len(b)})
		}
		sort.Slice(sizes, func(i, j int) bool { return sizes[i].bytes > sizes[j].bytes })
		dataBytes, _ := json.Marshal(resp)
		t.Logf("=== scenario: %s", sc.Name)
		t.Logf("data bytes=%d envelope bytes=%d escaped resource copy bytes=%d", len(dataBytes), len(encoded), len(resourceCopy))
		t.Logf("MCP estimateResponseBytes ~= structuredContent+resource.text = %d (budget %d, %.1f%%); envelope*2 = %d (%.1f%%); resource-only fallback fits=%v",
			est, mcpResponseByteBudget, 100*float64(est)/float64(mcpResponseByteBudget),
			2*len(encoded), 100*float64(2*len(encoded))/float64(mcpResponseByteBudget), len(resourceCopy) <= mcpResponseByteBudget)
		for i, s := range sizes {
			if i >= 12 {
				break
			}
			t.Logf("  %2d. %-28s %8d B  %5.1f%% of data", i+1, s.key, s.bytes, 100*float64(s.bytes)/float64(len(dataBytes)))
		}
		if sc.PerFamily == 50 && sc.EnrichmentRows == 50 && !sc.OverviewCopiesRows {
			logRowWeights(t, resp)
		}
	}
}

// logRowWeights reports the per-row JSON weight of the dominant row families
// and what a handle-only projection of the same rows would weigh.
func logRowWeights(t *testing.T, resp map[string]any) {
	t.Helper()
	handle := func(row map[string]any, keys ...string) int {
		h := map[string]any{}
		for _, k := range keys {
			if v, ok := row[k]; ok {
				h[k] = v
			}
		}
		b, _ := json.Marshal(h)
		return len(b)
	}
	report := func(key string, keys ...string) {
		rows := querycontract.MapSliceValue(resp, key)
		if len(rows) == 0 {
			return
		}
		full, _ := json.Marshal(rows)
		handleTotal := 0
		for _, r := range rows {
			handleTotal += handle(r, keys...)
		}
		t.Logf("  row weight %-22s rows=%d avg full=%4d B  handle-only(%v)=%4d B avg", key, len(rows), len(full)/len(rows), keys, handleTotal/len(rows))
	}
	report("delivery_paths", "type", "target", "target_id", "resolved_id", "path")
	report("instances", "instance_id", "environment", "platform_name")
	report("deployment_sources", "repo_id", "repo_name", "relationship_type")
	report("cloud_resources", "id", "name", "kind")
	report("k8s_resources", "entity_id", "entity_name", "kind", "relative_path")
	report("controller_entities", "entity_id", "entity_name", "relative_path")
	report("image_registry_truth", "image_ref", "digest")
	report("topology_edges", "relationship_type", "source_id", "target_id")
	report("deployment_facts", "type", "target", "source")
	report("k8s_relationships", "type", "source_id", "target_id")
	if de := querycontract.MapValue(resp, "deployment_evidence"); de != nil {
		arts := querycontract.MapSliceValue(de, "artifacts")
		if len(arts) > 0 {
			full, _ := json.Marshal(arts)
			h := 0
			for _, a := range arts {
				h += handle(a, "id", "relationship_type", "path", "source_repo_name", "resolved_id")
			}
			t.Logf("  row weight deployment_evidence.artifacts rows=%d avg full=%4d B handle-only=%4d B avg", len(arts), len(full)/len(arts), h/len(arts))
		}
	}
}
