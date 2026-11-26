// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesSlice2Dossier pins the lane-B shape-completeness fixes
// (issue #11491): the service-dossier overview objects, the CodeFlowResponse
// scope free-form marker, and the InvestigationEvidencePacket layers must
// name their properties in the served spec instead of bare
// {"type": "object"} shapes. Each entry names the Go owner that emits the
// keys the contract asserts.
func TestContractShapesSlice2Dossier(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}

	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing or non-object property %q", where, key)
		}
		return next
	}

	propsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		props, ok := node["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: object schema has no properties", where)
		}
		return props
	}

	itemsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		items, ok := node["items"].(map[string]any)
		if !ok {
			t.Fatalf("%s: array schema has no items", where)
		}
		return items
	}

	requireKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if node["type"] == "object" {
				_, hasProps := node["properties"]
				_, hasAdditional := node["additionalProperties"]
				if !hasProps && !hasAdditional {
					t.Errorf("%s: property %q is a bare object with no properties", where, key)
				}
			}
			if node["type"] == "array" {
				items, ok := node["items"].(map[string]any)
				if !ok {
					t.Errorf("%s: property %q is an array with no items", where, key)
					continue
				}
				if items["type"] == "object" {
					if _, hasProps := items["properties"]; !hasProps {
						t.Errorf("%s: property %q has bare object items", where, key)
					}
				}
			}
		}
	}

	requireDescribed := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if desc, _ := node["description"].(string); desc == "" {
				t.Errorf("%s: property %q has no description", where, key)
			}
		}
	}

	t.Run("service dossier overviews", func(t *testing.T) {
		t.Parallel()
		paths, ok := spec["paths"].(map[string]any)
		if !ok {
			t.Fatal("spec has no paths object")
		}
		route, ok := paths["/api/v0/services/{service_name}/story"].(map[string]any)
		if !ok {
			t.Fatal("spec paths missing /api/v0/services/{service_name}/story")
		}
		op := child(t, route, "get", "service-story")
		responses := child(t, op, "responses", "service-story get")
		okResp := child(t, responses, "200", "service-story get")
		content := child(t, okResp, "content", "service-story get 200")
		body := child(t, content, "application/json", "service-story get 200")
		schema := child(t, body, "schema", "service-story get 200")
		props := propsOf(t, schema, "service-story")

		// Owner: service/story_overview.go buildServiceDeploymentOverviewWithContext.
		deployment := child(t, props, "deployment_overview", "service-story")
		requireKeys(t, propsOf(t, deployment, "service-story.deployment_overview"), "service-story.deployment_overview",
			"instance_count", "environment_count", "platform_count", "platforms")
		requireDescribed(t, propsOf(t, deployment, "service-story.deployment_overview"), "service-story.deployment_overview",
			"instance_count", "environment_count", "platform_count")

		// Owner: service/story_overview.go buildServiceDocumentationOverview.
		documentation := child(t, props, "documentation_overview", "service-story")
		requireKeys(t, propsOf(t, documentation, "service-story.documentation_overview"), "service-story.documentation_overview",
			"repo_id", "repo_name", "docs_route_count", "api_spec_count", "entrypoint_host_count")
		requireDescribed(t, propsOf(t, documentation, "service-story.documentation_overview"), "service-story.documentation_overview",
			"repo_id", "docs_route_count", "api_spec_count")
	})

	t.Run("workload context documentation overview", func(t *testing.T) {
		t.Parallel()
		components, ok := spec["components"].(map[string]any)
		if !ok {
			t.Fatal("spec has no components object")
		}
		schemas := child(t, components, "schemas", "components")
		context := child(t, schemas, "WorkloadContext", "components.schemas")
		props := propsOf(t, context, "WorkloadContext")

		// Owner: service/story_overview.go buildServiceDocumentationOverview.
		documentation := child(t, props, "documentation_overview", "WorkloadContext")
		requireKeys(t, propsOf(t, documentation, "WorkloadContext.documentation_overview"), "WorkloadContext.documentation_overview",
			"repo_id", "repo_name", "docs_route_count", "api_spec_count", "entrypoint_host_count")
	})

	t.Run("code flow scope is free-form", func(t *testing.T) {
		t.Parallel()
		components, ok := spec["components"].(map[string]any)
		if !ok {
			t.Fatal("spec has no components object")
		}
		schemas := child(t, components, "schemas", "components")
		response := child(t, schemas, "CodeFlowResponse", "components.schemas")
		props := propsOf(t, response, "CodeFlowResponse")

		// Owner: codequery/flow.go buildCodeFlowPayload. The scope carries no
		// fixed keys, so the contract documents it as free-form instead of
		// leaving a bare undescribed object.
		scope, ok := props["scope"].(map[string]any)
		if !ok {
			t.Fatal("CodeFlowResponse: missing property \"scope\"")
		}
		if desc, _ := scope["description"].(string); desc == "" {
			t.Error("CodeFlowResponse: property \"scope\" has no description")
		}
	})

	t.Run("investigation evidence packet layers", func(t *testing.T) {
		t.Parallel()
		components, ok := spec["components"].(map[string]any)
		if !ok {
			t.Fatal("spec has no components object")
		}
		schemas := child(t, components, "schemas", "components")
		packet := child(t, schemas, "InvestigationEvidencePacket", "components.schemas")
		props := propsOf(t, packet, "InvestigationEvidencePacket")

		// Owner: querycontract/packet/envelope.go + build.go.
		identity := child(t, props, "identity", "packet")
		requireKeys(t, propsOf(t, identity, "packet.identity"), "packet.identity",
			"family", "subject", "basis")
		freshness := child(t, props, "freshness", "packet")
		requireKeys(t, propsOf(t, freshness, "packet.freshness"), "packet.freshness",
			"state")
		answer := child(t, props, "answer", "packet")
		requireKeys(t, propsOf(t, answer, "packet.answer"), "packet.answer",
			"truth_class", "supported", "partial")
		bounds := child(t, props, "bounds", "packet")
		requireKeys(t, propsOf(t, bounds, "packet.bounds"), "packet.bounds",
			"max_source_facts", "truncated")
		redaction := child(t, props, "redaction", "packet")
		requireKeys(t, propsOf(t, redaction, "packet.redaction"), "packet.redaction",
			"profile", "version")
		validation := child(t, props, "validation", "packet")
		requireKeys(t, propsOf(t, validation, "packet.validation"), "packet.validation",
			"status", "checks")
		checks := child(t, propsOf(t, validation, "packet.validation"), "checks", "packet.validation")
		requireKeys(t, propsOf(t, itemsOf(t, checks, "packet.validation.checks"), "packet.validation.checks[]"), "packet.validation.checks[]",
			"id", "ok")

		sourceFacts := child(t, props, "source_facts", "packet")
		requireKeys(t, propsOf(t, itemsOf(t, sourceFacts, "packet.source_facts"), "packet.source_facts[]"), "packet.source_facts[]",
			"summary")
		reducerDecisions := child(t, props, "reducer_decisions", "packet")
		requireKeys(t, propsOf(t, itemsOf(t, reducerDecisions, "packet.reducer_decisions"), "packet.reducer_decisions[]"), "packet.reducer_decisions[]",
			"state")
		graphAnswers := child(t, props, "graph_answers", "packet")
		requireKeys(t, propsOf(t, itemsOf(t, graphAnswers, "packet.graph_answers"), "packet.graph_answers[]"), "packet.graph_answers[]",
			"present")
		citations := child(t, props, "citations", "packet")
		requireKeys(t, propsOf(t, itemsOf(t, citations, "packet.citations"), "packet.citations[]"), "packet.citations[]",
			"kind", "repo_id")
		missing := child(t, props, "missing_evidence", "packet")
		requireKeys(t, propsOf(t, itemsOf(t, missing, "packet.missing_evidence"), "packet.missing_evidence[]"), "packet.missing_evidence[]",
			"hop", "reason")
		semantic := child(t, props, "semantic_observations", "packet")
		requireKeys(t, propsOf(t, itemsOf(t, semantic, "packet.semantic_observations"), "packet.semantic_observations[]"), "packet.semantic_observations[]",
			"label", "observation")
		reproduce := child(t, props, "reproduce", "packet")
		requireKeys(t, propsOf(t, itemsOf(t, reproduce, "packet.reproduce"), "packet.reproduce[]"), "packet.reproduce[]",
			"description")
	})
}
