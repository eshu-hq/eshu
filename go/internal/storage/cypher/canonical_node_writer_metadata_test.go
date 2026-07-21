// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterProjectsInfrastructureIdentityMetadata(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-infra-1",
		GenerationID: "gen-infra-1",
		RepoID:       "repo-infra-1",
		RepoPath:     "/repos/infra",
		Repository: &projector.RepositoryRow{
			RepoID: "repo-infra-1",
			Name:   "infra-repo",
			Path:   "/repos/infra",
		},
		Entities: []projector.EntityRow{
			{
				EntityID:     "claim-1",
				Label:        "CrossplaneClaim",
				EntityName:   "database",
				FilePath:     "/repos/infra/control-plane/claim.yaml",
				RelativePath: "control-plane/claim.yaml",
				StartLine:    7,
				EndLine:      20,
				Language:     "yaml",
				RepoID:       "repo-infra-1",
				Metadata: map[string]any{
					"kind":        "SQLInstance",
					"api_version": "database.example.org/v1alpha1",
					"namespace":   "platform",
				},
			},
			{
				EntityID:     "deployment-1",
				Label:        "K8sResource",
				EntityName:   "api",
				FilePath:     "/repos/infra/deploy/deployment.yaml",
				RelativePath: "deploy/deployment.yaml",
				StartLine:    3,
				EndLine:      40,
				Language:     "yaml",
				RepoID:       "repo-infra-1",
				Metadata: map[string]any{
					"kind":           "Deployment",
					"api_version":    "apps/v1",
					"namespace":      "prod",
					"qualified_name": "prod/Deployment/api",
				},
			},
			{
				EntityID:     "terraform-rds-1",
				Label:        "TerraformResource",
				EntityName:   "aws_rds_cluster.primary",
				FilePath:     "/repos/infra/terraform/rds.tf",
				RelativePath: "terraform/rds.tf",
				StartLine:    1,
				EndLine:      12,
				Language:     "hcl",
				RepoID:       "repo-infra-1",
				Metadata: map[string]any{
					"provider":          "aws",
					"resource_type":     "aws_rds_cluster",
					"resource_service":  "rds",
					"resource_category": "data",
				},
			},
		},
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	propsByLabel := map[string]map[string]any{}
	for _, call := range exec.calls {
		if call.Operation != OperationCanonicalUpsert {
			continue
		}
		for _, label := range []string{"CrossplaneClaim", "K8sResource", "TerraformResource"} {
			if !strings.Contains(call.Cypher, "MERGE (n:"+label) {
				continue
			}
			rows, ok := call.Parameters["rows"].([]map[string]any)
			if !ok {
				t.Fatalf("%s rows type = %T, want []map[string]any", label, call.Parameters["rows"])
			}
			if got, want := len(rows), 1; got != want {
				t.Fatalf("%s row count = %d, want %d", label, got, want)
			}
			props, ok := rows[0]["props"].(map[string]any)
			if !ok {
				t.Fatalf("%s props type = %T, want map[string]any", label, rows[0]["props"])
			}
			propsByLabel[label] = props
		}
	}

	claimProps := propsByLabel["CrossplaneClaim"]
	if len(claimProps) == 0 {
		t.Fatal("missing CrossplaneClaim properties")
	}
	if got, want := claimProps["kind"], "SQLInstance"; got != want {
		t.Fatalf("CrossplaneClaim kind = %#v, want %#v", got, want)
	}
	if got, want := claimProps["api_version"], "database.example.org/v1alpha1"; got != want {
		t.Fatalf("CrossplaneClaim api_version = %#v, want %#v", got, want)
	}
	if got, want := claimProps["namespace"], "platform"; got != want {
		t.Fatalf("CrossplaneClaim namespace = %#v, want %#v", got, want)
	}

	resourceProps := propsByLabel["K8sResource"]
	if len(resourceProps) == 0 {
		t.Fatal("missing K8sResource properties")
	}
	if got, want := resourceProps["kind"], "Deployment"; got != want {
		t.Fatalf("K8sResource kind = %#v, want %#v", got, want)
	}
	if got, want := resourceProps["qualified_name"], "prod/Deployment/api"; got != want {
		t.Fatalf("K8sResource qualified_name = %#v, want %#v", got, want)
	}

	terraformProps := propsByLabel["TerraformResource"]
	if len(terraformProps) == 0 {
		t.Fatal("missing TerraformResource properties")
	}
	for key, want := range map[string]any{
		"provider":          "aws",
		"resource_type":     "aws_rds_cluster",
		"resource_service":  "rds",
		"resource_category": "data",
	} {
		if got := terraformProps[key]; got != want {
			t.Fatalf("TerraformResource %s = %#v, want %#v", key, got, want)
		}
	}
}

func TestCanonicalNodeWriterKeepsDeadCodeRootKindsOutOfGraphHotPath(t *testing.T) {
	t.Parallel()

	props := canonicalEntityProperties(
		projector.EntityRow{
			EntityID:     "function-root",
			Label:        "Function",
			EntityName:   "ExecuteGroup",
			FilePath:     "/repo/wiring.go",
			RelativePath: "wiring.go",
			StartLine:    27,
			EndLine:      35,
			Language:     "go",
			RepoID:       "repo-1",
			Metadata: map[string]any{
				"dead_code_root_kinds": []string{
					"go.interface_method_implementation",
					"go.function_value_reference",
				},
				"exactness_blockers": []string{
					"macro_expansion_unavailable",
				},
				"method_kind": "method",
			},
		},
		"scope-1",
		"gen-1",
	)

	if _, ok := props["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds graph property present, want content-store-only metadata")
	}
	if _, ok := props["exactness_blockers"]; ok {
		t.Fatalf("exactness_blockers graph property present, want content-store-only metadata")
	}
	if got, want := props["method_kind"], "method"; got != want {
		t.Fatalf("method_kind = %#v, want %#v", got, want)
	}
}

func TestCanonicalNodeWriterBatching(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 2, nil) // batch size = 2

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Repository: &projector.RepositoryRow{
			RepoID: "repo-1",
			Name:   "my-repo",
			Path:   "/repos/my-repo",
		},
		Files: []projector.FileRow{
			{Path: "/f1.go", RelativePath: "f1.go", Name: "f1.go", Language: "go", RepoID: "repo-1", DirPath: "/src"},
			{Path: "/f2.go", RelativePath: "f2.go", Name: "f2.go", Language: "go", RepoID: "repo-1", DirPath: "/src"},
			{Path: "/f3.go", RelativePath: "f3.go", Name: "f3.go", Language: "go", RepoID: "repo-1", DirPath: "/src"},
		},
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Collect file-phase calls
	var fileCalls []Statement
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalUpsert && call.Parameters[StatementMetadataPhaseKey] == CanonicalPhaseFiles {
			fileCalls = append(fileCalls, call)
		}
	}

	// 3 files with batch size 2 => 2 batches (2 + 1), each with
	// update-existing and create-missing statements.
	if len(fileCalls) != 4 {
		t.Fatalf("file statements = %d, want 4", len(fileCalls))
	}

	batch1Rows := fileCalls[0].Parameters["rows"].([]map[string]any)
	batch2Rows := fileCalls[2].Parameters["rows"].([]map[string]any)
	if len(batch1Rows) != 2 {
		t.Fatalf("batch 1 rows = %d, want 2", len(batch1Rows))
	}
	if len(batch2Rows) != 1 {
		t.Fatalf("batch 2 rows = %d, want 1", len(batch2Rows))
	}
	if got, want := fileCalls[0].Parameters[StatementMetadataPhaseKey], CanonicalPhaseFiles; got != want {
		t.Fatalf("file statement phase = %#v, want %#v", got, want)
	}
	if summary, _ := fileCalls[0].Parameters[StatementMetadataSummaryKey].(string); !strings.Contains(summary, "phase=files rows=2") {
		t.Fatalf("file statement summary = %q, want row count", summary)
	}
	if !strings.Contains(fileCalls[0].Cypher, "MATCH (f:File {path: row.path})") {
		t.Fatalf("first file statement = %q, want existing-file update", fileCalls[0].Cypher)
	}
	if strings.Contains(fileCalls[0].Cypher, "MERGE (f:File") {
		t.Fatalf("existing-file update statement must not MERGE File: %s", fileCalls[0].Cypher)
	}
	if !strings.Contains(fileCalls[1].Cypher, "WHERE NOT EXISTS { MATCH (:File {path: row.path}) }") {
		t.Fatalf("second file statement = %q, want missing-file guard", fileCalls[1].Cypher)
	}
}

func TestCanonicalNodeWriterFileBatchSizeOverride(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil).WithFileBatchSize(3)

	files := make([]projector.FileRow, 0, 7)
	for i := range 7 {
		name := fmt.Sprintf("file-%d.go", i)
		files = append(files, projector.FileRow{
			Path:         "/repo/" + name,
			RelativePath: name,
			Name:         name,
			Language:     "go",
			RepoID:       "repo-1",
			DirPath:      "/repo",
		})
	}

	err := writer.Write(context.Background(), projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repo",
		Repository: &projector.RepositoryRow{
			RepoID: "repo-1",
			Name:   "repo",
			Path:   "/repo",
		},
		Files: files,
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var fileCalls []Statement
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalUpsert && call.Parameters[StatementMetadataPhaseKey] == CanonicalPhaseFiles {
			fileCalls = append(fileCalls, call)
		}
	}
	if len(fileCalls) != 6 {
		t.Fatalf("file statements = %d, want 6", len(fileCalls))
	}
	for i, wantRows := range []int{3, 3, 1} {
		rows := fileCalls[i*2].Parameters["rows"].([]map[string]any)
		if len(rows) != wantRows {
			t.Fatalf("file batch %d rows = %d, want %d", i, len(rows), wantRows)
		}
		if got, want := fileCalls[i*2].Parameters[StatementMetadataPhaseKey], CanonicalPhaseFiles; got != want {
			t.Fatalf("file batch %d phase = %#v, want %#v", i, got, want)
		}
	}
}
