// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// validatePilotResponse checks the selected read's seeded result contract.
// A successful status with an empty or unrelated payload is not a latency proof.
func validatePilotResponse(expect string, body []byte) error {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		return fmt.Errorf("invalid JSON object")
	}
	switch expect {
	case "ingester_status":
		if payload["ingester"] != "repository" || payload["runtime_family"] != "ingester" || !object(payload["queue"]) || !object(payload["health"]) {
			return fmt.Errorf("repository ingester status fields do not match")
		}
	case "repository_story":
		return validateRepositoryStory(payload)
	case "mcp_file_search", "mcp_entity_search":
		return validateMCPSearchResponse(payload, expect)
	case "relationships_catalog":
		return validateRelationshipCatalog(payload)
	case "mcp_hosted_readiness", "mcp_ingesters_list", "mcp_operator_controlplane":
		structured, err := validateStatusMCPEnvelope(payload)
		if err != nil {
			return err
		}
		return validateStatusMCPIdentity(expect, structured)
	case "mcp_index_status":
		result, structured, err := validateMCPEnvelopeHead(payload)
		if err != nil {
			return err
		}
		if err := validateIndexStatus(structured); err != nil {
			return err
		}
		if err := validateMCPContentMirror(result, structured); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown pilot expectation %q", expect)
	}
	return nil
}

func object(value any) bool {
	_, ok := value.(map[string]any)
	return ok
}

// validateStatusMCPEnvelope checks the tools/call envelope shared by the
// #7009 status-proxy MCP pilots: the JSON-RPC identity, a non-error result,
// structured content, a content layout in either result shape, and the resource text
// mirroring structured content (dual shape). A successful status with an empty or
// unrelated payload is not a latency proof.
func validateStatusMCPEnvelope(payload map[string]any) (map[string]any, error) {
	result, structured, err := validateMCPEnvelopeHead(payload)
	if err != nil {
		return nil, err
	}
	if err := validateMCPContentMirror(result, structured); err != nil {
		return nil, err
	}
	return structured, nil
}

// validateMCPEnvelopeHead checks the tools/call head shared by every MCP
// pilot: the JSON-RPC identity, a non-error result, and structured content.
func validateMCPEnvelopeHead(payload map[string]any) (map[string]any, map[string]any, error) {
	if payload["jsonrpc"] != "2.0" || payload["id"] != float64(1) || payload["error"] != nil {
		return nil, nil, fmt.Errorf("MCP response identity or error mismatch")
	}
	result, ok := payload["result"].(map[string]any)
	if !ok || result["isError"] == true {
		return nil, nil, fmt.Errorf("MCP tool result missing or failed")
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("MCP structured content missing")
	}
	return result, structured, nil
}

// validateMCPContentMirror checks the content layout of either result shape
// (#8800). The dual shape holds a text summary and an embedded resource whose
// text mirrors structured content. The structured shape holds exactly one
// non-empty text summary and no resource. Any other layout fails, so a result
// that drifts from both shapes is not accepted as a latency proof.
func validateMCPContentMirror(result, structured map[string]any) error {
	content, ok := result["content"].([]any)
	if !ok {
		return fmt.Errorf("MCP tool content missing summary or resource")
	}
	switch len(content) {
	case 1:
		return validateMCPSummaryBlock(content[0])
	case 2:
		if err := validateMCPSummaryBlock(content[0]); err != nil {
			return err
		}
		return validateMCPResourceMirror(content[1], structured)
	default:
		return fmt.Errorf("MCP tool content missing summary or resource")
	}
}

// validateMCPSummaryBlock checks the leading text summary both shapes carry.
func validateMCPSummaryBlock(block any) error {
	summary, ok := block.(map[string]any)
	if !ok || summary["type"] != "text" || summary["resource"] != nil {
		return fmt.Errorf("MCP summary block missing")
	}
	if text, ok := summary["text"].(string); !ok || text == "" {
		return fmt.Errorf("MCP summary text missing")
	}
	return nil
}

// validateMCPResourceMirror checks the dual shape's embedded resource block
// and that its text mirrors structured content.
func validateMCPResourceMirror(block any, structured map[string]any) error {
	resourceBlock, ok := block.(map[string]any)
	if !ok || resourceBlock["type"] != "resource" {
		return fmt.Errorf("MCP resource block missing")
	}
	resource, ok := resourceBlock["resource"].(map[string]any)
	if !ok {
		return fmt.Errorf("MCP resource missing")
	}
	resourceText, ok := resource["text"].(string)
	if !ok {
		return fmt.Errorf("MCP resource text missing")
	}
	var duplicate map[string]any
	if json.Unmarshal([]byte(resourceText), &duplicate) != nil || !reflect.DeepEqual(duplicate, structured) {
		return fmt.Errorf("MCP resource and structured payload differ")
	}
	return nil
}

// validateStatusMCPIdentity checks the route identity inside structured
// content of a status-proxy MCP pilot: the seeded corpus serves exactly one
// repository ingester, the readiness state is one of the two builder states,
// and the control plane carries its queue and reducer-domain read model.
func validateStatusMCPIdentity(expect string, structured map[string]any) error {
	switch expect {
	case "mcp_hosted_readiness":
		if state, ok := structured["state"].(string); !ok || (state != "ready" && state != "not_ready") {
			return fmt.Errorf("hosted readiness state is not a builder state")
		}
	case "mcp_ingesters_list":
		ingesters, ok := structured["ingesters"].([]any)
		if !ok || len(ingesters) == 0 {
			return fmt.Errorf("ingesters list is empty")
		}
		first, ok := ingesters[0].(map[string]any)
		if !ok || first["name"] != "repository" {
			return fmt.Errorf("ingesters list does not start with the repository ingester")
		}
	case "mcp_operator_controlplane":
		if !object(structured["queue"]) {
			return fmt.Errorf("control plane has no queue read model")
		}
		if _, ok := structured["reducer_domains"].([]any); !ok {
			return fmt.Errorf("control plane has no reducer domains")
		}
	default:
		return fmt.Errorf("unknown pilot expectation %q", expect)
	}
	return nil
}

func validateIndexStatus(payload map[string]any) error {
	// The seeded corpus holds exactly the story Repository nodes (the anchor
	// plus its related set). Any other count means the measured database was
	// not the isolated seeded corpus.
	status, ok := payload["status"].(string)
	if payload["repository_count"] != float64(SeedStoryRepositoryCount) || !object(payload["queue"]) || !ok || status == "" {
		return fmt.Errorf("index status does not match seeded %d-repository corpus", SeedStoryRepositoryCount)
	}
	return nil
}

// validateRepositoryStory checks the seeded story answer: the anchor's id, a
// repository subject, a deployment overview and non-empty story sections. It
// does not pin content details (row counts move with the corpus), only that
// a real story was served for the seeded anchor.
func validateRepositoryStory(payload map[string]any) error {
	repository, ok := payload["repository"].(map[string]any)
	if !ok || repository["id"] != SeedStoryAnchorID {
		return fmt.Errorf("story repository is not the seeded anchor")
	}
	subject, ok := payload["subject"].(map[string]any)
	if !ok || subject["type"] != "repository" || subject["id"] != SeedStoryAnchorID {
		return fmt.Errorf("story subject is not the seeded anchor")
	}
	if !object(payload["deployment_overview"]) {
		return fmt.Errorf("story has no deployment overview")
	}
	sections, ok := payload["story_sections"].([]any)
	if !ok || len(sections) == 0 {
		return fmt.Errorf("story has no story sections")
	}
	return nil
}

// validateMCPSearchResponse checks a tools/call envelope for an unscoped
// search: the JSON-RPC identity, a non-error result, and structured content
// carrying the canonical {data, truth} envelope of an EXACT page. data.partial
// must be absent: at the seeded corpus size the walk proves the page inside
// its budget, so a partial page is a regression, not an accepted outcome. A
// truncated page from the request limit (more matches past the page) stays
// legal: only the budget-cut partial is refused.
func validateMCPSearchResponse(payload map[string]any, expect string) error {
	if payload["jsonrpc"] != "2.0" || payload["id"] != float64(1) || payload["error"] != nil {
		return fmt.Errorf("MCP response identity or error mismatch")
	}
	result, ok := payload["result"].(map[string]any)
	if !ok || result["isError"] == true {
		return fmt.Errorf("MCP tool result missing or failed")
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		return fmt.Errorf("MCP structured content missing")
	}
	data, ok := structured["data"].(map[string]any)
	if !ok {
		return fmt.Errorf("%s structured content has no data envelope", expect)
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) == 0 {
		return fmt.Errorf("%s data.results is empty: the seeded token must match", expect)
	}
	if _, hasPartial := data["partial"]; hasPartial {
		return fmt.Errorf("%s data.partial is present: the seeded page must be exact", expect)
	}
	truth, ok := structured["truth"].(map[string]any)
	if !ok {
		return fmt.Errorf("%s structured content has no truth envelope", expect)
	}
	if level := truth["level"]; level != "exact" && level != "derived" {
		return fmt.Errorf("%s truth.level = %v, want exact or derived (never partial)", expect, level)
	}
	return nil
}

func validateRelationshipCatalog(payload map[string]any) error {
	// This independently specified fixture is deliberately not read from the
	// production catalog. The latency seed creates nodes plus the story
	// Repository->Repository DEPENDS_ON fan-out (SeedStoryRelatedCount edges,
	// pinned by VerifyRepositoryGraphCounts); every other verb stays at zero.
	expected := map[string]string{
		"CALLS": "code", "IMPORTS": "code", "INHERITS": "code",
		"REFERENCES": "code", "OVERRIDES": "code", "QUERIES_TABLE": "code",
		"DEPLOYS_FROM": "deploy", "INSTANCE_OF": "deploy", "RECONCILES_FROM": "deploy",
		"PROVISIONS_DEPENDENCY_FOR": "infra", "USES_MODULE": "infra",
		"DISCOVERS_CONFIG_IN": "infra", "MANAGES": "infra",
		"ATLANTIS_DEPENDS_ON": "infra", "USES_WORKFLOW": "infra",
		"RUNS_ON": "runtime", "AWS_lambda_function_uses_image": "runtime", "DEPENDS_ON": "runtime",
		"INVOKES_CLOUD_ACTION": "security", "READS_CONFIG_FROM": "ops", "TAINT_FLOWS_TO": "ops",
	}
	expectedCounts := map[string]float64{"DEPENDS_ON": float64(SeedStoryRelatedCount)}
	verbs, ok := payload["verbs"].([]any)
	if !ok || len(verbs) != len(expected) || payload["verb_count"] != float64(len(expected)) {
		return fmt.Errorf("catalog verbs/count mismatch")
	}
	layers := map[string]bool{}
	seen := map[string]bool{}
	for _, value := range verbs {
		verb, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("catalog verb identity missing")
		}
		name, nameOK := verb["verb"].(string)
		layer, layerOK := verb["layer"].(string)
		if !nameOK || !layerOK || expected[name] != layer || layer == "" || seen[name] {
			return fmt.Errorf("catalog verb %q identity/layer differs from seeded contract", name)
		}
		seen[name] = true
		count, ok := verb["count"].(float64)
		if !ok || count != expectedCounts[name] {
			return fmt.Errorf("catalog verb %q count = %v, want %v", name, verb["count"], expectedCounts[name])
		}
		layers[layer] = true
	}
	if payload["total_edges"] != float64(SeedStoryRelatedCount) || payload["layer_count"] != float64(len(layers)) {
		return fmt.Errorf("catalog total or layer count mismatch")
	}
	return nil
}
