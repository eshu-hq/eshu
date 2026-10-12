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
	case "relationships_catalog":
		return validateRelationshipCatalog(payload)
	case "mcp_index_status":
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
		if err := validateIndexStatus(structured); err != nil {
			return err
		}
		content, ok := result["content"].([]any)
		if !ok || len(content) != 2 {
			return fmt.Errorf("MCP tool content missing summary or resource")
		}
		resourceBlock, ok := content[1].(map[string]any)
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
	default:
		return fmt.Errorf("unknown pilot expectation %q", expect)
	}
	return nil
}

func object(value any) bool {
	_, ok := value.(map[string]any)
	return ok
}

func validateIndexStatus(payload map[string]any) error {
	// The gate's graph seed has no Repository nodes. An unexpectedly nonzero
	// count means the measured database was not the isolated seeded corpus.
	status, ok := payload["status"].(string)
	if payload["repository_count"] != float64(0) || !object(payload["queue"]) || !ok || status == "" {
		return fmt.Errorf("index status does not match seeded zero-repository corpus")
	}
	return nil
}

func validateRelationshipCatalog(payload map[string]any) error {
	// This independently specified fixture is deliberately not read from the
	// production catalog. The latency seed creates nodes, never typed edges.
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
		if !ok || count != 0 {
			return fmt.Errorf("catalog verb %q has edges in the zero-edge seed", name)
		}
		layers[layer] = true
	}
	if payload["total_edges"] != float64(0) || payload["layer_count"] != float64(len(layers)) {
		return fmt.Errorf("catalog total or layer count mismatch")
	}
	return nil
}
