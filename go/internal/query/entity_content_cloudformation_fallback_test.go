package query

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetEntityContextFallsBackToCloudFormationNestedStackResource(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"cloudformation-resource-1", "repo-1", "infra/stack.yaml", "CloudFormationResource", "NestedStack",
					int64(1), int64(20), "yaml", "Type: AWS::CloudFormation::Stack", []byte(`{"resource_type":"AWS::CloudFormation::Stack","template_url":"https://example.com/nested-stack.yaml","condition":"EnableNested"}`),
				},
			},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/cloudformation-resource-1/context", nil)
	req.SetPathValue("entity_id", "cloudformation-resource-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["semantic_summary"], "CloudFormationResource NestedStack is an AWS::CloudFormation::Stack nested stack sourced from https://example.com/nested-stack.yaml and guarded by condition EnableNested."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("resp[relationships] type = %T, want []any", resp["relationships"])
	}
	if len(relationships) != 1 {
		t.Fatalf("len(resp[relationships]) = %d, want 1", len(relationships))
	}
	relationship, ok := relationships[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[relationships][0] type = %T, want map[string]any", relationships[0])
	}
	if got, want := relationship["type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_name"], "https://example.com/nested-stack.yaml"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "cloudformation_nested_stack_template"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextLinksNestedStackTemplateURLToRepoLocalTemplate(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"cloudformation-resource-1", "repo-1", "infra/root/stack.yaml", "CloudFormationResource", "NestedStack",
					int64(1), int64(20), "yaml", "Type: AWS::CloudFormation::Stack", []byte(`{"resource_type":"AWS::CloudFormation::Stack","template_url":"https://example.com/templates/nested/network.yaml"}`),
				},
			},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-1", "infra/templates/nested/network.yaml", "", "", "hash-1", int64(25), "yaml", "cloudformation_template",
				},
				{
					"repo-1", "infra/root/stack.yaml", "", "", "hash-2", int64(40), "yaml", "cloudformation_template",
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/cloudformation-resource-1/context", nil)
	req.SetPathValue("entity_id", "cloudformation-resource-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	relationships, ok := resp["relationships"].([]any)
	if !ok || len(relationships) != 1 {
		t.Fatalf("resp[relationships] = %#v, want one relationship", resp["relationships"])
	}
	relationship, ok := relationships[0].(map[string]any)
	if !ok {
		t.Fatalf("relationship type = %T, want map[string]any", relationships[0])
	}
	if got, want := relationship["target_name"], "infra/templates/nested/network.yaml"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "cloudformation_nested_stack_template_local"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextLeavesRemoteNestedStackTemplateURLUnlinked(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"cloudformation-resource-1", "repo-1", "infra/root/stack.yaml", "CloudFormationResource", "NestedStack",
					int64(1), int64(20), "yaml", "Type: AWS::CloudFormation::Stack", []byte(`{"resource_type":"AWS::CloudFormation::Stack","template_url":"https://example.com/templates/nested/network.yaml"}`),
				},
			},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-1", "infra/templates/other.yaml", "", "", "hash-1", int64(25), "yaml", "cloudformation_template",
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/cloudformation-resource-1/context", nil)
	req.SetPathValue("entity_id", "cloudformation-resource-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	relationships, ok := resp["relationships"].([]any)
	if !ok || len(relationships) != 1 {
		t.Fatalf("resp[relationships] = %#v, want one relationship", resp["relationships"])
	}
	relationship, ok := relationships[0].(map[string]any)
	if !ok {
		t.Fatalf("relationship type = %T, want map[string]any", relationships[0])
	}
	if got, want := relationship["target_name"], "https://example.com/templates/nested/network.yaml"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "cloudformation_nested_stack_template"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}
