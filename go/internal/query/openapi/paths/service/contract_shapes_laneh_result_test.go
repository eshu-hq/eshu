// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLaneHResult pins the Lane H gap-2 fix:
// sections[].answer.result in GET
// /api/v0/services/{service_name}/intelligence-report carries a type and an
// owner-traced description. The handler composes through
// serviceintel.Compose, which never sets EmbedResult, so the key is absent
// unless a composer embeds the envelope copy; result_ref stays canonical.
func TestContractShapesLaneHResult(t *testing.T) {
	t.Parallel()

	trimmed := strings.TrimSpace(IntelligenceReport)
	trimmed = strings.TrimSuffix(trimmed, ",")
	var doc map[string]any
	if err := json.Unmarshal([]byte("{"+trimmed+"}"), &doc); err != nil {
		t.Fatalf("fragment is not valid JSON: %v", err)
	}
	route, ok := doc["/api/v0/services/{service_name}/intelligence-report"].(map[string]any)
	if !ok {
		t.Fatal("fragment paths missing /api/v0/services/{service_name}/intelligence-report")
	}
	get, ok := route["get"].(map[string]any)
	if !ok {
		t.Fatal("route missing get")
	}
	responses, ok := get["responses"].(map[string]any)
	if !ok {
		t.Fatal("get missing responses")
	}
	okResp, ok := responses["200"].(map[string]any)
	if !ok {
		t.Fatal("get missing 200")
	}
	content, ok := okResp["content"].(map[string]any)
	if !ok {
		t.Fatal("200 missing content")
	}
	body, ok := content["application/json"].(map[string]any)
	if !ok {
		t.Fatal("200 missing application/json")
	}
	schema, ok := body["schema"].(map[string]any)
	if !ok {
		t.Fatal("200 missing schema")
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("200 schema missing properties")
	}
	sections, ok := props["sections"].(map[string]any)
	if !ok {
		t.Fatal("200 schema missing sections")
	}
	items, ok := sections["items"].(map[string]any)
	if !ok {
		t.Fatal("sections has no items")
	}
	sectionProps, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatal("sections items has no properties")
	}
	answer, ok := sectionProps["answer"].(map[string]any)
	if !ok {
		t.Fatal("section missing answer")
	}
	answerProps, ok := answer["properties"].(map[string]any)
	if !ok {
		t.Fatal("answer has no properties")
	}
	result, ok := answerProps["result"].(map[string]any)
	if !ok {
		t.Fatal("answer missing result")
	}
	if _, ok := result["type"]; !ok {
		t.Errorf("answer.result has no type (want an open-map shape with a type)")
	}
	desc, _ := result["description"].(string)
	if desc == "" {
		t.Errorf("answer.result has no description")
	}
	if !strings.Contains(desc, "answer.AnswerPacket.Result") {
		t.Errorf("answer.result description does not name its answer.AnswerPacket.Result owner")
	}
	if !strings.Contains(desc, "absent") {
		t.Errorf("answer.result description carries no presence note (want when the key is absent)")
	}
}
