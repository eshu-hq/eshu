// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// Second-generation answer parity tests (issue #1937).
//
// The first-generation parity tests pin the canonical HTTP/MCP envelope. These
// tests pin the derived answer-experience surfaces that are pure views over that
// envelope today: AnswerPacket, QueryPlaybook resolution, and VisualizationPacket
// builders. Route/MCP wiring for playbooks and visualizations is deliberately
// follow-up work in query docs, so this gate proves those views stay transport
// only: HTTP, MCP, and CLI-style JSON maps derive identical packet/playbook/
// visualization contracts from the same canonical data.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestSecondGenerationAnswerPacketParityHostedProfileModes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		profile     query.QueryProfile
		reader      query.GraphQuery
		limit       int
		wantError   bool
		wantPartial bool
	}{
		{
			name:      "hosted_full_stack_supported",
			profile:   query.ProfileLocalFullStack,
			reader:    presentBothCompareReader(),
			limit:     50,
			wantError: false,
		},
		{
			name:        "hosted_full_stack_truncated",
			profile:     query.ProfileLocalFullStack,
			reader:      truncatedCompareReader{},
			limit:       1,
			wantError:   false,
			wantPartial: true,
		},
		{
			name:      "local_lightweight_no_policy_refused",
			profile:   query.ProfileLocalLightweight,
			reader:    presentBothCompareReader(),
			limit:     50,
			wantError: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := mountCompareHandler(t, tc.profile, tc.reader)
			args := map[string]any{
				"workload_id": "workload:service-edge-api",
				"left":        "qa",
				"right":       "prod",
				"limit":       tc.limit,
			}
			body := map[string]any{
				"workload_id": "workload:service-edge-api",
				"left":        "qa",
				"right":       "prod",
				"limit":       tc.limit,
			}

			httpEnv := httpEnvelope(t, handler, http.MethodPost, "/api/v0/compare/environments", body)
			mcpEnv, summary := mcpEnvelope(t, handler, "compare_environments", args)
			requireParity(t, "http canonical envelope", "mcp canonical envelope",
				extractComparable(t, httpEnv), extractComparable(t, mcpEnv))

			httpPacket := answerPacketForParity(t, httpEnv, "compare_environments", "POST /api/v0/compare/environments")
			mcpPacket := answerPacketForParity(t, mcpEnv, "compare_environments", "POST /api/v0/compare/environments")
			requireAnswerPacketParity(t, "http answer_packet", "mcp answer_packet", httpPacket, mcpPacket)

			if got, want := httpPacket.Supported, !tc.wantError; got != want {
				t.Fatalf("hosted_profile answer_packet supported=%t, want %t", got, want)
			}
			if got := httpPacket.Partial; got != tc.wantPartial {
				t.Fatalf("hosted_profile answer_packet partial=%t, want %t", got, tc.wantPartial)
			}
			if strings.TrimSpace(summary) == "" {
				t.Fatal("text summary empty, want MCP convenience summary alongside structured answer_packet parity")
			}
		})
	}
}

func TestSecondGenerationPlaybookResolveParity(t *testing.T) {
	t.Parallel()

	registry := readOnlyToolRegistry()
	for _, playbook := range query.PlaybookCatalog() {
		playbook := playbook
		t.Run(playbook.ID, func(t *testing.T) {
			t.Parallel()

			for _, tool := range query.PlaybookToolNames() {
				if _, ok := registry[tool]; !ok {
					t.Fatalf("playbook registry drift: tool %q is not in ReadOnlyTools", tool)
				}
			}
			listed, ok := query.LookupPlaybook(playbook.ID)
			if !ok {
				t.Fatalf("playbook list/resolve parity: LookupPlaybook(%q) returned false", playbook.ID)
			}
			requirePlaybookDefinitionParity(t, "catalog playbook", "lookup playbook", playbook, listed)

			resolved, err := playbook.Resolve(samplePlaybookInputs(playbook))
			if err != nil {
				t.Fatalf("playbook resolve failed for %q: %v", playbook.ID, err)
			}
			var cliDecoded query.ResolvedPlaybook
			decodeCanonicalJSON(t, resolved, &cliDecoded)
			requireResolvedPlaybookParity(t, "api/mcp resolved playbook", "cli json resolved playbook", resolved, cliDecoded)
		})
	}
}

func TestSecondGenerationVisualizationPacketParityFromCanonicalData(t *testing.T) {
	t.Parallel()

	truth := &query.TruthEnvelope{
		Level:      query.TruthLevelExact,
		Capability: "platform_impact.context_overview",
		Profile:    query.ProfileLocalFullStack,
		Basis:      query.TruthBasisAuthoritativeGraph,
		Freshness:  query.TruthFreshness{State: query.FreshnessFresh},
	}
	cases := []struct {
		name  string
		build func(map[string]any, *query.TruthEnvelope) query.VisualizationPacket
		data  map[string]any
	}{
		{
			name:  "service_story",
			build: query.BuildServiceStoryVisualizationPacket,
			data:  serviceStoryVisualizationCanonicalData(),
		},
		{
			name:  "evidence_citation",
			build: query.BuildEvidenceCitationVisualizationPacketFromMap,
			data:  evidenceCitationVisualizationCanonicalData(),
		},
		{
			name:  "incident_context",
			build: query.BuildIncidentContextVisualizationPacketFromMap,
			data:  incidentContextVisualizationCanonicalData(),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			httpPacket := tc.build(tc.data, truth)
			mcpPacket := tc.build(canonicalDataMap(t, tc.data), truth)
			cliPacket := tc.build(canonicalDataMap(t, tc.data), truth)

			requireVisualizationPacketParity(t, "http visualization "+tc.name, "mcp visualization "+tc.name, httpPacket, mcpPacket)
			requireVisualizationPacketParity(t, "http visualization "+tc.name, "cli visualization "+tc.name, httpPacket, cliPacket)
			if !httpPacket.Supported {
				t.Fatalf("visualization %s unsupported: limitations=%v", tc.name, httpPacket.Limitations)
			}
			if len(httpPacket.Nodes) == 0 {
				t.Fatalf("visualization %s has no nodes, want renderable subgraph", tc.name)
			}
		})
	}
}

func answerPacketForParity(t *testing.T, env *query.ResponseEnvelope, tool, route string) query.AnswerPacket {
	t.Helper()

	var data map[string]any
	if env != nil {
		data, _ = env.Data.(map[string]any)
	}
	handles := packetEvidenceHandlesFromCompareData(data)
	missing := packetMissingEvidenceFromCompareData(data)
	return query.NewAnswerPacket(query.AnswerPacketInput{
		PromptFamily:         packetPromptFamily(env),
		Question:             "compare workload environment drift",
		PrimaryTool:          tool,
		PrimaryRoute:         route,
		Summary:              query.StringVal(data, "story"),
		ResultRef:            "eshu://parity/compare_environments",
		EmbedResult:          true,
		Limitations:          packetLimitationsFromData(data),
		Truncated:            boolFromMap(data, "truncated"),
		NoEvidence:           env != nil && env.Error == nil && len(handles) == 0,
		EvidenceHandles:      handles,
		MissingEvidence:      missing,
		RecommendedNextCalls: mapSliceFromData(data, "recommended_next_calls"),
		Envelope:             env,
	})
}

func packetPromptFamily(env *query.ResponseEnvelope) string {
	if env != nil && env.Truth != nil && env.Truth.Capability != "" {
		return env.Truth.Capability
	}
	if env != nil && env.Error != nil && env.Error.Capability != "" {
		return env.Error.Capability
	}
	return "platform_impact.environment_compare"
}

func packetEvidenceHandlesFromCompareData(data map[string]any) []query.EvidenceCitationHandle {
	if data == nil {
		return nil
	}
	handles := []query.EvidenceCitationHandle{}
	for _, side := range []string{"left", "right"} {
		snap, ok := data[side].(map[string]any)
		if !ok {
			continue
		}
		for _, raw := range asSlice(snap["cloud_resources"]) {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if id := query.StringVal(row, "id"); id != "" {
				handles = append(handles, query.EvidenceCitationHandle{
					Kind:           "entity",
					EntityID:       id,
					EvidenceFamily: "cloud_resource",
					Reason:         side + "_environment_resource",
				})
			}
		}
	}
	return handles
}

func packetMissingEvidenceFromCompareData(data map[string]any) []query.EvidenceCitationHandle {
	missing := missingEvidenceFromData(data)
	if len(missing) == 0 {
		return nil
	}
	handles := make([]query.EvidenceCitationHandle, 0, len(missing))
	for _, reason := range missing {
		handles = append(handles, query.EvidenceCitationHandle{
			Kind:           "entity",
			EvidenceFamily: "environment",
			Reason:         reason,
		})
	}
	return handles
}

func packetLimitationsFromData(data map[string]any) []string {
	rows := mapSliceFromData(data, "limitations")
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if reason := query.StringVal(row, "reason"); reason != "" {
			out = append(out, reason)
		}
	}
	return out
}

func mapSliceFromData(data map[string]any, key string) []map[string]any {
	if data == nil {
		return nil
	}
	rawRows, ok := data[key].([]any)
	if !ok {
		return nil
	}
	rows := make([]map[string]any, 0, len(rawRows))
	for _, raw := range rawRows {
		row, ok := raw.(map[string]any)
		if ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func boolFromMap(data map[string]any, key string) bool {
	if data == nil {
		return false
	}
	v, _ := data[key].(bool)
	return v
}

func requireAnswerPacketParity(t *testing.T, surfaceA, surfaceB string, a, b query.AnswerPacket) {
	t.Helper()

	if gotA, gotB := canonicalJSON(t, a), canonicalJSON(t, b); gotA != gotB {
		t.Fatalf("answer_packet parity drift:\n%s=%s\n%s=%s", surfaceA, gotA, surfaceB, gotB)
	}
}

func requirePlaybookDefinitionParity(t *testing.T, surfaceA, surfaceB string, a, b query.QueryPlaybook) {
	t.Helper()

	if gotA, gotB := canonicalJSON(t, a), canonicalJSON(t, b); gotA != gotB {
		t.Fatalf("playbook definition parity drift:\n%s=%s\n%s=%s", surfaceA, gotA, surfaceB, gotB)
	}
}

func requireResolvedPlaybookParity(t *testing.T, surfaceA, surfaceB string, a, b query.ResolvedPlaybook) {
	t.Helper()

	if gotA, gotB := canonicalJSON(t, a), canonicalJSON(t, b); gotA != gotB {
		t.Fatalf("playbook resolved-call parity drift:\n%s=%s\n%s=%s", surfaceA, gotA, surfaceB, gotB)
	}
	if len(a.Calls) == 0 {
		t.Fatalf("playbook resolved-call parity: %s produced no calls", surfaceA)
	}
	for _, call := range a.Calls {
		if len(call.Arguments) == 0 && call.Tool != "check_documentation_evidence_packet_freshness" {
			t.Fatalf("playbook resolved-call parity: step %q has no bounded arguments", call.StepID)
		}
	}
}

func requireVisualizationPacketParity(t *testing.T, surfaceA, surfaceB string, a, b query.VisualizationPacket) {
	t.Helper()

	if gotA, gotB := canonicalJSON(t, a), canonicalJSON(t, b); gotA != gotB {
		t.Fatalf("visualization packet parity drift:\n%s=%s\n%s=%s", surfaceA, gotA, surfaceB, gotB)
	}
}

func readOnlyToolRegistry() map[string]struct{} {
	registry := map[string]struct{}{}
	for _, tool := range ReadOnlyTools() {
		registry[tool.Name] = struct{}{}
	}
	return registry
}

func samplePlaybookInputs(playbook query.QueryPlaybook) map[string]string {
	inputs := map[string]string{}
	for _, input := range playbook.RequiredInputs {
		switch input.Name {
		case "service_name":
			inputs[input.Name] = "workload:service-edge-api"
		case "environment":
			inputs[input.Name] = "prod"
		case "repo_id":
			inputs[input.Name] = "repo-service-edge-api"
		case "topic":
			inputs[input.Name] = "admission control"
		case "finding_id":
			inputs[input.Name] = "documentation-finding:runtime-readiness"
		default:
			if input.Required {
				inputs[input.Name] = "sample-" + input.Name
			}
		}
	}
	return inputs
}

func serviceStoryVisualizationCanonicalData() map[string]any {
	return map[string]any{
		"service_identity": map[string]any{
			"service_id":   "workload:service-edge-api",
			"service_name": "service-edge-api",
			"repo_id":      "repo-service-edge-api",
		},
		"evidence_graph": map[string]any{
			"nodes": []map[string]any{
				{"id": "repo-service-edge-api", "label": "service-edge-api", "kind": "repository", "category": "service"},
				{"id": "repo-gateway", "label": "gateway", "kind": "repository", "category": "upstream"},
			},
			"edges": []map[string]any{
				{"source": "repo-gateway", "target": "repo-service-edge-api", "relationship_type": "DEPENDS_ON", "confidence": 0.9},
			},
		},
		"upstream_dependencies": []map[string]any{},
		"downstream_consumers":  map[string]any{},
	}
}

func evidenceCitationVisualizationCanonicalData() map[string]any {
	return map[string]any{
		"question": "cite the runtime readiness evidence",
		"citations": []map[string]any{
			{
				"citation_id":     "citation:runtime-readiness",
				"rank":            1,
				"kind":            "entity",
				"evidence_family": "source",
				"entity_id":       "go:func:RuntimeReadiness",
				"entity_name":     "RuntimeReadiness",
				"excerpt":         "bounded excerpt",
			},
		},
		"coverage": map[string]any{
			"query_shape":        "bounded_evidence_citation_packet",
			"input_handle_count": 1,
			"resolved_count":     1,
			"limit":              10,
		},
		"recommended_next_calls": []map[string]any{},
	}
}

func incidentContextVisualizationCanonicalData() map[string]any {
	return map[string]any{
		"incident": map[string]any{
			"provider":             "pagerduty",
			"provider_incident_id": "incident-123",
			"title":                "checkout latency",
		},
		"evidence_path": []map[string]any{
			{"slot": "incident", "truth_label": "exact", "explanation": "provider incident record matched"},
			{"slot": "service", "truth_label": "exact", "explanation": "service matched incident record"},
			{"slot": "commit", "truth_label": "derived", "explanation": "commit matched deploy window"},
		},
		"missing_evidence": []map[string]any{},
		"truncated":        false,
	}
}

func canonicalDataMap(t *testing.T, data map[string]any) map[string]any {
	t.Helper()

	var decoded map[string]any
	decodeCanonicalJSON(t, data, &decoded)
	return decoded
}

func decodeCanonicalJSON(t *testing.T, in, out any) {
	t.Helper()

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal canonical json: %v", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("unmarshal canonical json: %v", err)
	}
}

type truncatedCompareReader struct{}

func (truncatedCompareReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	return parityCompareReader{}.RunSingle(ctx, cypher, params)
}

func (truncatedCompareReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if !strings.Contains(cypher, "MATCH (i:WorkloadInstance)-[r:USES]->(c:CloudResource)") {
		return nil, nil
	}
	instance, _ := params["instance_id"].(string)
	env := strings.TrimPrefix(instance, "instance:")
	rows := []map[string]any{}
	for i := 0; i < 2; i++ {
		rows = append(rows, map[string]any{
			"id":         fmt.Sprintf("cloud:%s-resource-%d", env, i),
			"name":       fmt.Sprintf("%s-resource-%d", env, i),
			"kind":       "queue",
			"provider":   "aws",
			"confidence": 0.9,
			"reason":     "materialized_cloud_dependency",
		})
	}
	return rows, nil
}
