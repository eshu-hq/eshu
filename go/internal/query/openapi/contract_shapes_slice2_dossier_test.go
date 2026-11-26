// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesSlice2Dossier pins the lane-B shape-completeness fixes
// (issue #11491): the service-dossier overview objects and the
// InvestigationEvidencePacket layers must name their properties in the
// served spec instead of bare {"type": "object"} shapes, and
// CodeFlowResponse must not carry the phantom scope its owner never
// emits. Each entry names the Go owner that emits the keys the contract
// asserts.
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
					_, hasProps := items["properties"]
					_, hasAdditional := items["additionalProperties"]
					if !hasProps && !hasAdditional {
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

	// requireScalarTypes pins the JSON scalar type of hand-written schema
	// properties. Presence-only checks pass a boolean where the server
	// emits an integer, so every hand-typed scalar needs its type pinned.
	requireScalarTypes := func(t *testing.T, props map[string]any, where string, want map[string]string) {
		t.Helper()
		for key, typ := range want {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if got, _ := node["type"].(string); got != typ {
				t.Errorf("%s: property %q has type %q, want %q", where, key, got, typ)
			}
		}
	}

	// requireExactKeys pins the closed property set of an object whose
	// owner emits a fixed key list: missing keys and phantom additions
	// both fail.
	requireExactKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		want := make(map[string]bool, len(keys))
		for _, key := range keys {
			want[key] = true
			if _, ok := props[key].(map[string]any); !ok {
				t.Errorf("%s: missing property %q", where, key)
			}
		}
		for key := range props {
			if !want[key] {
				t.Errorf("%s: phantom property %q not emitted by the owner", where, key)
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
		// Owner: service/story_dossier.go buildServiceDossierAPISurface plus
		// service/story_overview.go dropStoryOverviewEndpoints, which deletes
		// the endpoint rows and names their location instead.
		surface := child(t, propsOf(t, deployment, "service-story.deployment_overview"), "api_surface", "deployment_overview")
		surfaceProps := propsOf(t, surface, "deployment_overview.api_surface")
		requireKeys(t, surfaceProps, "deployment_overview.api_surface",
			"endpoints_shipped_at", "endpoint_count", "method_count", "spec_count", "truncated")
		if _, ok := surfaceProps["endpoints"]; ok {
			t.Error("deployment_overview.api_surface: phantom property \"endpoints\" is dropped by dropStoryOverviewEndpoints")
		}
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
		// The contract names exactly the 11 keys the builder emits.
		documentation := child(t, props, "documentation_overview", "WorkloadContext")
		documentationProps := propsOf(t, documentation, "WorkloadContext.documentation_overview")
		requireExactKeys(t, documentationProps, "WorkloadContext.documentation_overview",
			"repo_id", "repo_name", "portable_identifier", "docs_route_count", "api_spec_count",
			"entrypoint_host_count", "remote_url", "repo_slug", "has_remote",
			"local_path_present", "api_spec_paths")
		requireScalarTypes(t, documentationProps, "WorkloadContext.documentation_overview", map[string]string{
			"repo_id": "string", "repo_name": "string", "portable_identifier": "string",
			"docs_route_count": "integer", "api_spec_count": "integer",
			"entrypoint_host_count": "integer", "remote_url": "string", "repo_slug": "string",
			"has_remote": "boolean", "local_path_present": "boolean", "api_spec_paths": "array",
		})

		// Owner: the same buildServiceDeploymentOverviewWithContext builder
		// as the story route, but without dropStoryOverviewEndpoints, so the
		// context keeps the api_surface endpoint rows and never carries
		// endpoints_shipped_at.
		workloadDeployment := child(t, props, "deployment_overview", "WorkloadContext")
		workloadDeploymentProps := propsOf(t, workloadDeployment, "WorkloadContext.deployment_overview")
		requireExactKeys(t, workloadDeploymentProps, "WorkloadContext.deployment_overview",
			"instance_count", "environment_count", "materialized_environment_count",
			"config_environment_count", "platform_count", "platforms", "environments",
			"materialized_environments", "deployment_truth_tier", "config_environments",
			"hostname_count", "entrypoint_count", "network_path_count", "api_surface",
			"dependent_count", "consumer_repository_count", "provisioning_source_chain_count",
			"cloud_resource_count", "cloud_relationship_basis", "uncorrelated_cloud_resource_count",
			"missing_cloud_relationship", "deployment_tool_families",
			"deployment_evidence_artifact_count", "delivery_path_count",
			"delivery_workflow_count", "shared_config_path_count")
		workloadSurface := child(t, workloadDeploymentProps, "api_surface", "WorkloadContext.deployment_overview")
		workloadSurfaceProps := propsOf(t, workloadSurface, "WorkloadContext api_surface")
		requireKeys(t, workloadSurfaceProps, "WorkloadContext api_surface",
			"endpoint_count", "method_count", "spec_count", "endpoints", "truncated")
		if _, ok := workloadSurfaceProps["endpoints_shipped_at"]; ok {
			t.Error("WorkloadContext api_surface: phantom property \"endpoints_shipped_at\" is only added by the story route drop")
		}
	})

	t.Run("code flow response carries no scope", func(t *testing.T) {
		t.Parallel()
		components, ok := spec["components"].(map[string]any)
		if !ok {
			t.Fatal("spec has no components object")
		}
		schemas := child(t, components, "schemas", "components")
		response := child(t, schemas, "CodeFlowResponse", "components.schemas")
		props := propsOf(t, response, "CodeFlowResponse")

		// Owner: codequery/flow.go buildCodeFlowPayload emits only coverage,
		// ambiguity, bounds, and one result family; writeCodeFlowResponse
		// adds only query and coverage. A scope property would be a
		// phantom: assert its absence.
		if _, ok := props["scope"]; ok {
			t.Error("CodeFlowResponse: phantom property \"scope\" is not emitted by the owner")
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

		// Owner: querycontract/packet/envelope.go PacketIdentity.
		identity := child(t, props, "identity", "packet")
		identityProps := propsOf(t, identity, "packet.identity")
		requireExactKeys(t, identityProps, "packet.identity",
			"family", "subject", "question", "generation", "profile", "backend", "basis")
		requireScalarTypes(t, identityProps, "packet.identity", map[string]string{
			"family": "string", "subject": "object", "question": "string",
			"generation": "string", "profile": "string", "backend": "string", "basis": "string",
		})
		freshness := child(t, props, "freshness", "packet")
		requireKeys(t, propsOf(t, freshness, "packet.freshness"), "packet.freshness",
			"state")
		// Owner: querycontract/packet/envelope.go PacketAnswer.
		answer := child(t, props, "answer", "packet")
		answerProps := propsOf(t, answer, "packet.answer")
		requireExactKeys(t, answerProps, "packet.answer",
			"truth_class", "summary", "supported", "partial", "limitations", "unsupported_reasons")
		requireScalarTypes(t, answerProps, "packet.answer", map[string]string{
			"truth_class": "string", "summary": "string", "supported": "boolean",
			"partial": "boolean", "limitations": "array", "unsupported_reasons": "array",
		})
		// Owner: querycontract/packet/envelope.go PacketBounds.
		bounds := child(t, props, "bounds", "packet")
		boundsProps := propsOf(t, bounds, "packet.bounds")
		requireExactKeys(t, boundsProps, "packet.bounds",
			"max_source_facts", "max_reducer_decisions", "max_graph_answers", "max_citations",
			"max_semantic_observations", "max_missing_evidence", "truncated", "truncated_layers")
		requireScalarTypes(t, boundsProps, "packet.bounds", map[string]string{
			"max_source_facts": "integer", "max_reducer_decisions": "integer",
			"max_graph_answers": "integer", "max_citations": "integer",
			"max_semantic_observations": "integer", "max_missing_evidence": "integer",
			"truncated": "boolean", "truncated_layers": "array",
		})
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
		// Owner: envelope.go Limitations plus build.go dedupeStrings.
		limitations, ok := props["limitations"].(map[string]any)
		if !ok {
			t.Fatal("packet: missing property \"limitations\"")
		}
		if typ, _ := limitations["type"].(string); typ != "array" {
			t.Errorf("packet: limitations has type %q, want array", typ)
		}
	})
}
