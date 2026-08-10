// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestInfraRelationshipsWhatRunsLambdaImageSurfacesOutgoingEdge is the #5738
// regression guard for the CloudResource(Lambda)-[:AWS_lambda_function_uses_image]->
// ContainerImage edge (#5450), which storage/cypher/cloud_resource_container_image_edge_writer.go
// writes but which had no semantic query_type alias before this change --
// #5751 (P1-B/P1-C) fixed the raw canonical-value lookup and the catalog
// entry, but the MCP analyze_infra_relationships tool's query_type enum only
// ever advertised semantic aliases (what_deploys, what_provisions,
// who_consumes_xrd, module_consumers, what_runs_image), never a raw edge-type
// string, so an MCP caller had no way to reach this edge. Anchored on a
// CloudResource, what_runs_lambda_image must bound the Cypher to
// :AWS_lambda_function_uses_image and the response must surface the resolved
// image as an outgoing relationship.
//
// The fake reader only returns the edge when the Cypher's relationship-type
// filter admits the stored-case type, so this proves the edge reaches the
// caller through the actual filter rather than a hardcoded fixture.
func TestInfraRelationshipsWhatRunsLambdaImageSurfacesOutgoingEdge(t *testing.T) {
	t.Parallel()

	lambdaImageEdge := map[string]any{
		"direction":     "outgoing",
		"type":          "AWS_lambda_function_uses_image",
		"target_name":   "supply-chain-demo",
		"target_id":     "oci-descriptor://123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo@sha256:0000000000000000000000000000000000000000000000000000000000aaaacc",
		"target_labels": []any{"ContainerImage"},
	}

	handler := &InfraHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeRepoGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				outgoing := []any{}
				if strings.Contains(cypher, ":AWS_lambda_function_uses_image") {
					outgoing = []any{lambdaImageEdge}
				}
				return map[string]any{
					"id":       "cloudresource:lambda-supply-chain-demo",
					"name":     "supply-chain-demo",
					"labels":   []any{"CloudResource"},
					"outgoing": outgoing,
					"incoming": []any{},
				}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", bytes.NewBufferString(`{"entity_id":"cloudresource:lambda-supply-chain-demo","relationship_type":"what_runs_lambda_image"}`))
	rec := httptest.NewRecorder()
	handler.getRelationships(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	payload := decodeInfraData(t, rec.Body.Bytes())
	outgoing, ok := payload["outgoing"].([]any)
	if !ok || len(outgoing) != 1 {
		t.Fatalf("what_runs_lambda_image dropped the AWS_lambda_function_uses_image edge: outgoing = %#v", payload["outgoing"])
	}
	edge, _ := outgoing[0].(map[string]any)
	if got := StringVal(edge, "type"); got != "AWS_lambda_function_uses_image" {
		t.Fatalf("outgoing[0].type = %q, want AWS_lambda_function_uses_image", got)
	}
}

// TestInfraRelationshipsWhatRunsLambdaImageSurfacesIncomingEdge proves the same
// edge is readable anchored the other direction: from a ContainerImage,
// what_runs_lambda_image must surface the Lambda CloudResource(s) that use it
// as incoming relationships (the reverse of the outgoing case above),
// matching the bidirectional pattern getRelationships already uses for every
// other alias.
func TestInfraRelationshipsWhatRunsLambdaImageSurfacesIncomingEdge(t *testing.T) {
	t.Parallel()

	lambdaImageEdge := map[string]any{
		"direction":     "incoming",
		"type":          "AWS_lambda_function_uses_image",
		"source_name":   "supply-chain-demo",
		"source_id":     "cloudresource:lambda-supply-chain-demo",
		"source_labels": []any{"CloudResource"},
	}

	handler := &InfraHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeRepoGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				incoming := []any{}
				if strings.Contains(cypher, ":AWS_lambda_function_uses_image") {
					incoming = []any{lambdaImageEdge}
				}
				return map[string]any{
					"id":       "oci-descriptor://123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo@sha256:0000000000000000000000000000000000000000000000000000000000aaaacc",
					"name":     "supply-chain-demo",
					"labels":   []any{"ContainerImage"},
					"outgoing": []any{},
					"incoming": incoming,
				}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", bytes.NewBufferString(`{"entity_id":"oci-descriptor://123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo@sha256:0000000000000000000000000000000000000000000000000000000000aaaacc","relationship_type":"AWS_lambda_function_uses_image"}`))
	rec := httptest.NewRecorder()
	handler.getRelationships(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	payload := decodeInfraData(t, rec.Body.Bytes())
	incoming, ok := payload["incoming"].([]any)
	if !ok || len(incoming) != 1 {
		t.Fatalf("AWS_lambda_function_uses_image (canonical) dropped the incoming Lambda edge: incoming = %#v", payload["incoming"])
	}
	edge, _ := incoming[0].(map[string]any)
	if got := StringVal(edge, "type"); got != "AWS_lambda_function_uses_image" {
		t.Fatalf("incoming[0].type = %q, want AWS_lambda_function_uses_image", got)
	}
	if got := StringVal(edge, "source_id"); got != "cloudresource:lambda-supply-chain-demo" {
		t.Fatalf("incoming[0].source_id = %q, want the Lambda CloudResource id", got)
	}
}
