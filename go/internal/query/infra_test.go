// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingInfraGraphReader struct {
	runRows    []map[string]any
	lastCypher string
	lastParams map[string]any
}

func (r *recordingInfraGraphReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.lastCypher = cypher
	r.lastParams = params
	return r.runRows, nil
}

func (*recordingInfraGraphReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func TestSearchInfraResourcesUsesInfrastructureLabelsForCategory(t *testing.T) {
	t.Parallel()

	reader := &recordingInfraGraphReader{
		runRows: []map[string]any{
			{
				"id":          "k8s:configmap:sample-service",
				"name":        "sample-service-api",
				"labels":      []any{"K8sResource"},
				"kind":        "ConfigMap",
				"provider":    "kubernetes",
				"environment": "qa",
				"source":      "deploy/qa/configmap.yaml",
				"config_path": "",
			},
		},
	}
	handler := &InfraHandler{Neo4j: reader}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"query":"sample-service-api","category":"k8s","limit":5}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if strings.Contains(reader.lastCypher, "Platform") || strings.Contains(reader.lastCypher, "Workload") {
		t.Fatalf("cypher = %q, want infrastructure-only label search", reader.lastCypher)
	}
	if strings.Contains(reader.lastCypher, "any(label IN labels(n)") {
		t.Fatalf("cypher = %q, want direct label predicates for NornicDB compatibility", reader.lastCypher)
	}
	for _, fragment := range []string{"n:K8sResource", "n:KustomizeOverlay"} {
		if !strings.Contains(reader.lastCypher, fragment) {
			t.Fatalf("cypher = %q, want fragment %q", reader.lastCypher, fragment)
		}
	}
	if _, ok := reader.lastParams["labels"]; ok {
		t.Fatalf("params[labels] = %#v, want no dynamic label parameter", reader.lastParams["labels"])
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := int(resp["count"].(float64)), 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
}

func TestSearchInfraResourcesProbesOneExtraRowForTruncation(t *testing.T) {
	t.Parallel()

	reader := &recordingInfraGraphReader{
		runRows: []map[string]any{
			{"id": "resource-1", "name": "one", "labels": []any{"K8sResource"}},
			{"id": "resource-2", "name": "two", "labels": []any{"K8sResource"}},
		},
	}
	handler := &InfraHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"query":"resource","category":"k8s","limit":1}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := reader.lastParams["limit"], 2; got != want {
		t.Fatalf("params[limit] = %#v, want %#v", got, want)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := int(resp["count"].(float64)), 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if got, want := resp["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}

func TestSearchInfraResourcesFiltersTerraformClassification(t *testing.T) {
	t.Parallel()

	reader := &recordingInfraGraphReader{
		runRows: []map[string]any{
			{
				"id":                "terraform:aws_s3_bucket.logs",
				"name":              "aws_s3_bucket.logs",
				"labels":            []any{"TerraformResource"},
				"kind":              "aws_s3_bucket",
				"provider":          "aws",
				"resource_type":     "aws_s3_bucket",
				"resource_service":  "s3",
				"resource_category": "storage",
			},
		},
	}
	handler := &InfraHandler{Neo4j: reader}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"query":"aws_s3","category":"terraform","kind":" aws_s3_bucket ","provider":" aws ","resource_category":" storage ","resource_service":" s3 ","limit":5}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	for _, fragment := range []string{"n:TerraformResource", "n:TerraformDataSource", "coalesce(n.resource_type, n.data_type, '')", "n.provider", "n.resource_service", "n.resource_category"} {
		if !strings.Contains(reader.lastCypher, fragment) {
			t.Fatalf("cypher = %q, want fragment %q", reader.lastCypher, fragment)
		}
	}
	for _, fragment := range []string{
		"coalesce(n.provider, '') as provider",
		"coalesce(n.source_system, '') as source_system",
		"coalesce(n.config_path, '') as config_path",
		"coalesce(n.resource_id, '') as resource_id",
		"coalesce(n.arn, '') as arn",
	} {
		if !strings.Contains(reader.lastCypher, fragment) {
			t.Fatalf("cypher = %q, want NornicDB-safe optional projection %q", reader.lastCypher, fragment)
		}
	}
	if strings.Contains(reader.lastCypher, "CONTAINS $query\n") || strings.Contains(reader.lastCypher, "CONTAINS $query OR\n") {
		t.Fatalf("cypher = %q, want one-line CloudResource free-text predicate for NornicDB compatibility", reader.lastCypher)
	}
	for _, forbidden := range []string{
		"coalesce(n.provider, n.source_system, '') = $provider",
		"n.source_system = $provider",
	} {
		if strings.Contains(reader.lastCypher, forbidden) {
			t.Fatalf("cypher = %q, must not use provenance source_system as Terraform provider filter", reader.lastCypher)
		}
	}
	for key, want := range map[string]any{
		"kind":              "aws_s3_bucket",
		"provider":          "aws",
		"resource_category": "storage",
		"resource_service":  "s3",
	} {
		if got := reader.lastParams[key]; got != want {
			t.Fatalf("params[%s] = %#v, want %#v", key, got, want)
		}
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := int(resp["count"].(float64)), 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	results := resp["results"].([]any)
	terraform, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("results[0] type = %T, want map[string]any", results[0])
	}
	if got, want := terraform["resource_type"], "aws_s3_bucket"; got != want {
		t.Fatalf("resource_type = %#v, want %#v", got, want)
	}
	if got, want := terraform["resource_service"], "s3"; got != want {
		t.Fatalf("resource_service = %#v, want %#v", got, want)
	}
	if got, want := terraform["resource_category"], "storage"; got != want {
		t.Fatalf("resource_category = %#v, want %#v", got, want)
	}
}

func TestSearchInfraResourcesMatchesResourceTypeAsFreeText(t *testing.T) {
	t.Parallel()

	reader := &recordingInfraGraphReader{
		runRows: []map[string]any{
			{
				"id":            "cloudformation:sample-function",
				"name":          "SampleFunction",
				"labels":        []any{"CloudFormationResource"},
				"resource_type": "AWS::Serverless::Function",
			},
		},
	}
	handler := &InfraHandler{Neo4j: reader}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"query":"AWS::Serverless::Function","category":"terraform","limit":5}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !strings.Contains(reader.lastCypher, "coalesce(n.resource_type, n.data_type, '') = $resource_type_query") {
		t.Fatalf("cypher = %q, want exact resource type identifier predicate", reader.lastCypher)
	}
	if strings.Contains(reader.lastCypher, "n.name CONTAINS $query") {
		t.Fatalf("cypher = %q, want exact resource type query outside generic free-text predicate", reader.lastCypher)
	}
	if got := reader.lastParams["query"]; got != "AWS::Serverless::Function" {
		t.Fatalf("params[query] = %#v, want resource type query", got)
	}
	if got := reader.lastParams["resource_type_query"]; got != "AWS::Serverless::Function" {
		t.Fatalf("params[resource_type_query] = %#v, want resource type query", got)
	}
}

func TestSearchInfraResourcesIncludesCloudResources(t *testing.T) {
	t.Parallel()

	reader := &recordingInfraGraphReader{
		runRows: []map[string]any{
			{
				"id":            "cloud-resource:ssm-parameter",
				"name":          "/configd/sample-service/database",
				"labels":        []any{"CloudResource"},
				"provider":      "aws",
				"resource_type": "aws_ssm_parameter",
				"resource_id":   "/configd/sample-service/database",
				"arn":           "arn:aws:ssm:us-east-1:111122223333:parameter/configd/sample-service/database",
				"account_id":    "111122223333",
				"region":        "us-east-1",
				"service_kind":  "ssm",
				"source":        "aws",
			},
		},
	}
	handler := &InfraHandler{Neo4j: reader}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"query":"sample-service","category":"cloud","provider":"aws","resource_service":"ssm","limit":5}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	for _, fragment := range []string{
		"n:CloudResource",
		"coalesce(n.arn, '') CONTAINS $query",
		"coalesce(n.resource_id, '') CONTAINS $query",
		"coalesce(n.service_kind, '') CONTAINS $query",
		"n.source_system = $provider",
		"n.service_kind = $resource_service",
	} {
		if !strings.Contains(reader.lastCypher, fragment) {
			t.Fatalf("cypher = %q, want fragment %q", reader.lastCypher, fragment)
		}
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	results := resp["results"].([]any)
	if got, want := len(results), 1; got != want {
		t.Fatalf("results len = %d, want %d", got, want)
	}
	resource := results[0].(map[string]any)
	for key, want := range map[string]any{
		"provider":      "aws",
		"resource_type": "aws_ssm_parameter",
		"resource_id":   "/configd/sample-service/database",
		"account_id":    "111122223333",
		"region":        "us-east-1",
		"service_kind":  "ssm",
	} {
		if got := resource[key]; got != want {
			t.Fatalf("%s = %#v, want %#v", key, got, want)
		}
	}
}

func TestSearchInfraResourcesScopesSourceSystemProviderFallbackToCloudResources(t *testing.T) {
	t.Parallel()

	reader := &recordingInfraGraphReader{
		runRows: []map[string]any{
			{
				"id":            "cloud-resource:ssm",
				"name":          "cloud ssm",
				"labels":        []any{"CloudResource"},
				"source_system": "aws",
			},
			{
				"id":            "terraform:state-resource",
				"name":          "tf state resource",
				"labels":        []any{"TerraformResource"},
				"source_system": "terraform_state",
			},
		},
	}
	handler := &InfraHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"query":"sample-service","provider":"aws","limit":5}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	for _, want := range []string{
		"n.provider = $provider",
		"(n:CloudResource AND n.source_system = $provider)",
		"coalesce(n.source_system, '') as source_system",
	} {
		if !strings.Contains(reader.lastCypher, want) {
			t.Fatalf("cypher = %q, want CloudResource-scoped provider fallback fragment %q", reader.lastCypher, want)
		}
	}
	for _, forbidden := range []string{
		"coalesce(n.provider, n.source_system, '') = $provider",
		"coalesce(n.provider, n.source_system, '') as provider",
	} {
		if strings.Contains(reader.lastCypher, forbidden) {
			t.Fatalf("cypher = %q, must not let source_system masquerade as provider for non-cloud nodes", reader.lastCypher)
		}
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	results := resp["results"].([]any)
	if got, want := len(results), 2; got != want {
		t.Fatalf("results len = %d, want %d", got, want)
	}
	cloud := results[0].(map[string]any)
	if got, want := cloud["provider"], "aws"; got != want {
		t.Fatalf("cloud provider = %#v, want %#v", got, want)
	}
	terraform := results[1].(map[string]any)
	if got, want := terraform["provider"], ""; got != want {
		t.Fatalf("terraform provider = %#v, want no provider fallback from source_system", got)
	}
}

func TestSearchInfraResourcesKeepsCloudResourceCandidatesExplicitAndRedacted(t *testing.T) {
	t.Parallel()

	reader := &recordingInfraGraphReader{
		runRows: []map[string]any{
			{
				"id":            "cloud-resource:ssm-parameter",
				"name":          "/configd/sample-service/database",
				"labels":        []any{"CloudResource"},
				"provider":      "aws",
				"resource_type": "aws_ssm_parameter",
				"resource_id":   "/configd/sample-service/database",
				"tags":          map[string]any{"password": "must-not-leak"},
				"evidence":      []any{"must-not-leak"},
			},
			{
				"id":            "cloud-resource:secret",
				"name":          "sample-service/runtime",
				"labels":        []any{"CloudResource"},
				"provider":      "aws",
				"resource_type": "aws_secretsmanager_secret",
				"resource_id":   "sample-service/runtime",
			},
		},
	}
	handler := &InfraHandler{Neo4j: reader}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"query":"sample-service","category":"cloud","limit":5}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := int(resp["count"].(float64)), 2; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	results := resp["results"].([]any)
	for i, result := range results {
		resource := result.(map[string]any)
		if _, ok := resource["tags"]; ok {
			t.Fatalf("results[%d] exposed tags: %#v", i, resource)
		}
		if _, ok := resource["evidence"]; ok {
			t.Fatalf("results[%d] exposed evidence: %#v", i, resource)
		}
	}
}
