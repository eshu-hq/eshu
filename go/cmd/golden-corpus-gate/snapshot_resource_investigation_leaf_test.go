// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const (
	goldenResourceInvestigationRepoID     = "repository:r_d963dd31"
	goldenResourceInvestigationResourceID = "content-entity:e_1130fc33095d"
	goldenResourceInvestigationName       = "aws_s3_bucket.data"
)

func TestGoldenResourceInvestigationIdentityComesFromTerraformFixture(t *testing.T) {
	t.Parallel()

	repoID, err := repositoryidentity.CanonicalRepositoryID("https://github.com/acme/terraform_comprehensive", "")
	if err != nil {
		t.Fatalf("CanonicalRepositoryID() error = %v", err)
	}
	if repoID != goldenResourceInvestigationRepoID {
		t.Fatalf("repo ID = %q, want %q", repoID, goldenResourceInvestigationRepoID)
	}
	resourceID := content.CanonicalEntityID(repoID, "main.tf", "TerraformResource", goldenResourceInvestigationName, 58)
	if resourceID != goldenResourceInvestigationResourceID {
		t.Fatalf("resource ID = %q, want %q", resourceID, goldenResourceInvestigationResourceID)
	}

	fixturePath := filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems", "terraform_comprehensive", "main.tf")
	body, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read %s: %v", fixturePath, err)
	}
	lines := strings.Split(string(body), "\n")
	if len(lines) < 58 || lines[57] != `resource "aws_s3_bucket" "data" {` {
		t.Fatalf("fixture line 58 = %q, want aws_s3_bucket.data declaration", lines[57])
	}
}

func TestGoldenSnapshotResourceInvestigationPinsResolvedProvenance(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape := snapshot.QueryShapes.MCP["investigate_resource"]
	if got, want := shape.MinimumResults, 1; got != want {
		t.Errorf("MinimumResults = %d, want %d", got, want)
	}
	if got, want := shape.MaximumResults, 1; got != want {
		t.Errorf("MaximumResults = %d, want %d", got, want)
	}
	if got, want := shape.ResultsField, "provisioning_paths"; got != want {
		t.Errorf("ResultsField = %q, want %q", got, want)
	}
	wantArguments := map[string]any{
		"resource_id":   goldenResourceInvestigationResourceID,
		"resource_type": "terraform_resource",
		"max_depth":     float64(4),
		"limit":         float64(10),
	}
	if !reflect.DeepEqual(shape.Arguments, wantArguments) {
		t.Errorf("Arguments = %#v, want %#v", shape.Arguments, wantArguments)
	}
	for _, field := range []string{"resource", "story", "provisioning_paths", "source_handles", "target_resolution"} {
		if !slices.Contains(shape.RequiredResponseFields, field) {
			t.Errorf("RequiredResponseFields missing %q", field)
		}
	}
	for path, want := range map[string]any{
		"target_resolution.status": "resolved",
		"resource.id":              goldenResourceInvestigationResourceID,
		"resource.name":            goldenResourceInvestigationName,
		"resource.repo_id":         goldenResourceInvestigationRepoID,
		"coverage.query_shape":     "resolved_resource_investigation",
		"coverage.path_count":      float64(1),
		"workload_count":           float64(0),
		"source_backend":           "graph",
		"truncated":                false,
		"missing_evidence[]":       "resource_usage_relationship_missing",
	} {
		if got := shape.RequiredJSONValues[path]; !reflect.DeepEqual(got, want) {
			t.Errorf("RequiredJSONValues[%q] = %#v, want %#v", path, got, want)
		}
	}
	wantMatches := map[string][]map[string]any{
		"target_resolution.candidates[]": {{
			"id": goldenResourceInvestigationResourceID, "name": goldenResourceInvestigationName,
			"repo_id": goldenResourceInvestigationRepoID,
		}},
		"provisioning_paths[]": {{
			"repo_id": goldenResourceInvestigationRepoID, "repo_name": "terraform_comprehensive",
			"direction": "incoming", "depth": float64(2),
		}},
		"source_handles[]": {{"repo_id": goldenResourceInvestigationRepoID, "reason": "repository_path"}},
		"recommended_next_calls[]": {{
			"tool":      "trace_resource_to_code",
			"arguments": map[string]any{"start": goldenResourceInvestigationResourceID},
		}},
	}
	if !reflect.DeepEqual(shape.RequiredJSONObjectMatches, wantMatches) {
		t.Errorf("RequiredJSONObjectMatches = %#v, want %#v", shape.RequiredJSONObjectMatches, wantMatches)
	}
}

func TestGoldenSnapshotResourceInvestigationBITES(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape := snapshot.QueryShapes.MCP["investigate_resource"]
	positive := []byte(`{"scope":{"query":"","resource_id":"content-entity:e_1130fc33095d","resource_type":"terraform_resource","environment":""},"target_resolution":{"input":"content-entity:e_1130fc33095d","resource_type":"terraform_resource","status":"resolved","candidates":[{"id":"content-entity:e_1130fc33095d","name":"aws_s3_bucket.data","labels":["TerraformResource"],"repo_id":"repository:r_d963dd31"}],"truncated":false},"resource":{"id":"content-entity:e_1130fc33095d","name":"aws_s3_bucket.data","labels":["TerraformResource"],"repo_id":"repository:r_d963dd31"},"story":"aws_s3_bucket.data resolves to committed Terraform source.","workloads":[],"workload_count":0,"provisioning_paths":[{"repo_id":"repository:r_d963dd31","repo_name":"terraform_comprehensive","direction":"incoming","depth":2,"hops":[{"type":"CONTAINS"},{"type":"CONTAINS"}]}],"source_handles":[{"repo_id":"repository:r_d963dd31","reason":"repository_path"}],"recommended_next_calls":[{"tool":"trace_resource_to_code","arguments":{"start":"content-entity:e_1130fc33095d"}},{"tool":"get_repo_context","arguments":{"repo_id":"repository:r_d963dd31"}}],"missing_evidence":["resource_usage_relationship_missing"],"limitations":["repository paths are graph provenance handles; read source files for exact line citations","resource resolved, but no workload usage relationship is materialized"],"coverage":{"query_shape":"resolved_resource_investigation","max_depth":4,"limit":10,"truncated":false,"workload_count":0,"path_count":1},"limit":10,"max_depth":4,"truncated":false,"source_backend":"graph"}`)
	if finding := EvaluateQueryShape("resource-positive", shape, positive); !finding.OK {
		t.Fatalf("positive resource investigation failed: %+v", finding)
	}
	noMatch := []byte(strings.Replace(string(positive), `"status":"resolved"`, `"status":"no_match"`, 1))
	if finding := EvaluateQueryShape("resource-no-match", shape, noMatch); finding.OK {
		t.Fatalf("no-match resource response passed: %+v", finding)
	}
	emptyPaths := []byte(strings.Replace(string(positive), `"provisioning_paths":[{"repo_id":"repository:r_d963dd31","repo_name":"terraform_comprehensive","direction":"incoming","depth":2,"hops":[{"type":"CONTAINS"},{"type":"CONTAINS"}]}]`, `"provisioning_paths":[]`, 1))
	if finding := EvaluateQueryShape("resource-empty-provenance", shape, emptyPaths); finding.OK {
		t.Fatalf("resource response without provenance passed: %+v", finding)
	}
}
