package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractInfrastructurePlatformRowsFromTerraformFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "fact-repo-1",
			ScopeID:  "scope-infra-eks",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":   "repo:infra-eks",
				"repo_name": "infra-eks",
			},
		},
		{
			FactID:   "fact-file-1",
			ScopeID:  "scope-infra-eks",
			FactKind: "parsed_file_data",
			Payload: map[string]any{
				"terraform_resources": []any{
					map[string]any{
						"name":          "aws_eks_cluster.prod",
						"resource_type": "aws_eks_cluster",
						"resource_name": "prod",
					},
				},
				"terraform_modules": []any{
					map[string]any{
						"name":   "cluster",
						"source": "terraform-aws-modules/eks/aws",
					},
				},
				"terraform_data_sources": []any{
					map[string]any{
						"name":      "aws_caller_identity.current",
						"data_type": "aws_caller_identity",
						"data_name": "current",
					},
				},
			},
		},
	}

	rows := ExtractInfrastructurePlatformRows(envelopes)

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.RepoID != "repo:infra-eks" {
		t.Fatalf("RepoID = %q, want repo:infra-eks", row.RepoID)
	}
	if row.PlatformKind != "eks" {
		t.Fatalf("PlatformKind = %q, want eks", row.PlatformKind)
	}
	if row.PlatformProvider != "aws" {
		t.Fatalf("PlatformProvider = %q, want aws", row.PlatformProvider)
	}
	if row.PlatformID == "" {
		t.Fatal("PlatformID is empty, want non-empty")
	}
}

func TestExtractInfrastructurePlatformRowsECSCluster(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "fact-repo-1",
			ScopeID:  "scope-infra-ecs",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":   "repo:infra-ecs",
				"repo_name": "infra-ecs",
			},
		},
		{
			FactID:   "fact-file-1",
			ScopeID:  "scope-infra-ecs",
			FactKind: "parsed_file_data",
			Payload: map[string]any{
				"terraform_resources": []any{
					map[string]any{
						"name":          "aws_ecs_cluster.payments",
						"resource_type": "aws_ecs_cluster",
						"resource_name": "payments",
					},
				},
			},
		},
	}

	rows := ExtractInfrastructurePlatformRows(envelopes)

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].PlatformKind != "ecs" {
		t.Fatalf("PlatformKind = %q, want ecs", rows[0].PlatformKind)
	}
	if rows[0].PlatformName != "payments" {
		t.Fatalf("PlatformName = %q, want payments", rows[0].PlatformName)
	}
}

func TestExtractInfrastructurePlatformRowsIgnoresServiceResourceNamesForPlatformName(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "fact-repo-1",
			ScopeID:  "scope-infra-ecs",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":   "repo:infra-ecs",
				"repo_name": "infra-ecs",
			},
		},
		{
			FactID:   "fact-file-1",
			ScopeID:  "scope-infra-ecs",
			FactKind: "parsed_file_data",
			Payload: map[string]any{
				"terraform_resources": []any{
					map[string]any{
						"resource_type": "aws_ecs_cluster",
						"resource_name": "default",
					},
					map[string]any{
						"resource_type": "aws_lb_listener_rule",
						"resource_name": "forward_http80_service_requests",
					},
				},
				"terraform_locals": []any{
					map[string]any{
						"name":  "cluster",
						"value": `"runtime-main"`,
					},
				},
				"terraform_modules": []any{
					map[string]any{
						"name":   "service_api",
						"source": "registry.example.com/platform/ecs-application/aws",
					},
				},
				"terraform_data_sources": []any{
					map[string]any{
						"data_type": "aws_iam_policy_document",
						"data_name": "service_api",
					},
				},
			},
		},
	}

	rows := ExtractInfrastructurePlatformRows(envelopes)

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0].PlatformName, "runtime-main"; got != want {
		t.Fatalf("PlatformName = %q, want %q", got, want)
	}
}

func TestExtractInfrastructurePlatformRowsFromServiceModuleClusterDataSource(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "fact-repo-1",
			ScopeID:  "scope-service-stack",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":   "repo:service-stack",
				"repo_name": "service-stack",
			},
		},
		{
			FactID:   "fact-file-1",
			ScopeID:  "scope-service-stack",
			FactKind: "parsed_file_data",
			Payload: map[string]any{
				"terraform_modules": []any{
					map[string]any{
						"name":         "api_service",
						"source":       "registry.example.com/platform/ecs-application/aws",
						"cluster_name": "data.aws_ecs_cluster.runtime-main.cluster_name",
					},
				},
				"terraform_data_sources": []any{
					map[string]any{
						"data_type": "aws_ecs_cluster",
						"data_name": "runtime-main",
					},
				},
			},
		},
	}

	rows := ExtractInfrastructurePlatformRows(envelopes)

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].PlatformKind != "ecs" {
		t.Fatalf("PlatformKind = %q, want ecs", rows[0].PlatformKind)
	}
	if got, want := rows[0].PlatformName, "runtime-main"; got != want {
		t.Fatalf("PlatformName = %q, want %q", got, want)
	}
}

func TestExtractInfrastructurePlatformRowsSkipsNonTerraformFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "fact-repo-1",
			ScopeID:  "scope-python-app",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":   "repo:python-app",
				"repo_name": "python-app",
			},
		},
		{
			FactID:   "fact-file-1",
			ScopeID:  "scope-python-app",
			FactKind: "parsed_file_data",
			Payload: map[string]any{
				"classes": []any{
					map[string]any{"name": "MyClass"},
				},
				"functions": []any{
					map[string]any{"name": "main"},
				},
			},
		},
	}

	rows := ExtractInfrastructurePlatformRows(envelopes)

	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 (no Terraform signals)", len(rows))
	}
}

func TestExtractInfrastructurePlatformRowsEmpty(t *testing.T) {
	t.Parallel()

	rows := ExtractInfrastructurePlatformRows(nil)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}

	rows = ExtractInfrastructurePlatformRows([]facts.Envelope{})
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
}

func TestExtractInfrastructurePlatformRowsAggregatesAcrossFiles(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "fact-repo-1",
			ScopeID:  "scope-infra",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":   "repo:infra-multi",
				"repo_name": "infra-multi",
			},
		},
		{
			FactID:   "fact-file-1",
			ScopeID:  "scope-infra",
			FactKind: "parsed_file_data",
			Payload: map[string]any{
				"terraform_resources": []any{
					map[string]any{
						"name":          "aws_eks_cluster.staging",
						"resource_type": "aws_eks_cluster",
						"resource_name": "staging",
					},
				},
			},
		},
		{
			FactID:   "fact-file-2",
			ScopeID:  "scope-infra",
			FactKind: "parsed_file_data",
			Payload: map[string]any{
				"terraform_modules": []any{
					map[string]any{
						"name":   "networking",
						"source": "terraform-aws-modules/vpc/aws",
					},
				},
			},
		},
	}

	rows := ExtractInfrastructurePlatformRows(envelopes)

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (aggregated across files)", len(rows))
	}
	if rows[0].RepoID != "repo:infra-multi" {
		t.Fatalf("RepoID = %q, want repo:infra-multi", rows[0].RepoID)
	}
}

func TestExtractInfrastructurePlatformRowsNoDescriptorForS3Only(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "fact-repo-1",
			ScopeID:  "scope-storage",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":   "repo:storage-infra",
				"repo_name": "storage-infra",
			},
		},
		{
			FactID:   "fact-file-1",
			ScopeID:  "scope-storage",
			FactKind: "parsed_file_data",
			Payload: map[string]any{
				"terraform_resources": []any{
					map[string]any{
						"name":          "aws_s3_bucket.logs",
						"resource_type": "aws_s3_bucket",
						"resource_name": "logs",
					},
				},
			},
		},
	}

	rows := ExtractInfrastructurePlatformRows(envelopes)

	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 (S3-only repos have no platform)", len(rows))
	}
}

func TestExtractInfrastructurePlatformRowsSupportsCurrentCollectorFactShape(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "fact-repo-1",
			ScopeID:  "scope-infra-eks",
			FactKind: "repository",
			Payload: map[string]any{
				"graph_id": "repo:infra-eks",
				"name":     "infra-eks",
			},
		},
		{
			FactID:   "fact-file-1",
			ScopeID:  "scope-infra-eks",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id": "repo:infra-eks",
				"parsed_file_data": map[string]any{
					"terraform_resources": []any{
						map[string]any{
							"name":          "aws_eks_cluster.prod",
							"resource_type": "aws_eks_cluster",
							"resource_name": "prod",
						},
					},
				},
			},
		},
	}

	rows := ExtractInfrastructurePlatformRows(envelopes)

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0].RepoID, "repo:infra-eks"; got != want {
		t.Fatalf("RepoID = %q, want %q", got, want)
	}
	if got, want := rows[0].PlatformKind, "eks"; got != want {
		t.Fatalf("PlatformKind = %q, want %q", got, want)
	}
}
