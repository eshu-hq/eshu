// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
)

// ecsBindingTestContext builds a resolution context with declarations
// spread across two repos: app (root module) and infra (child module dir
// plus a terragrunt stack).
func ecsBindingTestContext() ecsBindingResolutionContext {
	return ecsBindingResolutionContext{
		Declarations: []ECSBindingDeclaration{
			{
				RepoID: "repo-app", Path: "main.tf",
				EntityType: "TerraformResource", EntityName: "aws_ecs_service.api",
				Metadata: map[string]any{
					"resource_type": "aws_ecs_service",
					"resource_name": "api",
					"attributes":    map[string]any{"name": "api", "cluster": "arn:aws:ecs:us-east-1:111122223333:cluster/prod"},
				},
			},
			{
				RepoID: "repo-app", Path: "variables.tf",
				EntityType: "TerraformVariable", EntityName: "service_name",
				Metadata: map[string]any{"name": "service_name", "default": `"api-var"`},
			},
			{
				RepoID: "repo-app", Path: "main.tf",
				EntityType: "TerraformLocal", EntityName: "cluster_arn",
				Metadata: map[string]any{
					"name":  "cluster_arn",
					"value": `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"`,
				},
			},
			{
				RepoID: "repo-infra", Path: "modules/svc/main.tf",
				EntityType: "TerraformResource", EntityName: "aws_ecs_service.svc",
				Metadata: map[string]any{
					"resource_type":      "aws_ecs_service",
					"resource_name":      "svc",
					"attributes":         map[string]any{"name": "api", "cluster": "arn:aws:ecs:us-east-1:111122223333:cluster/prod"},
					"unknown_attributes": []string{"name", "cluster"},
				},
			},
		},
	}
}

func TestECSBindingResolveLiteral(t *testing.T) {
	t.Parallel()

	got, ok := ecsBindingResolveReference(`"api"`, ecsBindingTestContext(), "repo-app", "main.tf")
	if !ok || got != "api" {
		t.Fatalf("resolve literal = %q, %v; want %q, true", got, ok, "api")
	}
}

func TestECSBindingResolveRejectsInterpolation(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{`"api-${var.env}"`, `"${local.name}"`, `format("%s", var.name)`} {
		if got, ok := ecsBindingResolveReference(raw, ecsBindingTestContext(), "repo-app", "main.tf"); ok {
			t.Fatalf("resolve %q = %q, true; want unknown", raw, got)
		}
	}
}

func TestECSBindingResolveVariableDefaultSameDir(t *testing.T) {
	t.Parallel()

	ctx := ecsBindingTestContext()
	got, ok := ecsBindingResolveReference("var.service_name", ctx, "repo-app", "main.tf")
	if !ok || got != "api-var" {
		t.Fatalf("resolve var.service_name = %q, %v; want %q, true", got, ok, "api-var")
	}
}

func TestECSBindingResolveVariableDefaultCrossDirStaysUnknown(t *testing.T) {
	t.Parallel()

	// A child-module resource must not resolve a root variable default:
	// real Terraform would require a module-call argument, which the
	// parser does not capture (hook covered below). Same-dir only.
	ctx := ecsBindingTestContext()
	if got, ok := ecsBindingResolveReference("var.service_name", ctx, "repo-app", "modules/svc/main.tf"); ok {
		t.Fatalf("resolve cross-dir var.service_name = %q, true; want unknown", got)
	}
}

func TestECSBindingResolveLocalSameDir(t *testing.T) {
	t.Parallel()

	ctx := ecsBindingTestContext()
	got, ok := ecsBindingResolveReference("local.cluster_arn", ctx, "repo-app", "main.tf")
	if !ok || got != "arn:aws:ecs:us-east-1:111122223333:cluster/prod" {
		t.Fatalf("resolve local.cluster_arn = %q, %v; want cluster ARN, true", got, ok)
	}
}

func TestECSBindingResolveModuleCallArgumentsHook(t *testing.T) {
	t.Parallel()

	// The parser emits no generic module-call arguments today, so a
	// module-boundary var stays unknown. When a future parser records
	// metadata["arguments"], the hook resolves without a code change.
	ctx := ecsBindingResolutionContext{
		Declarations: []ECSBindingDeclaration{
			{
				RepoID: "repo-infra", Path: "envs/prod/main.tf",
				EntityType: "TerraformModule", EntityName: "svc",
				Metadata: map[string]any{
					"name":      "svc",
					"source":    "../../modules/svc",
					"arguments": map[string]any{"service_name": `"api-mod"`},
				},
			},
		},
	}
	got, ok := ecsBindingResolveReference("var.service_name", ctx, "repo-infra", "modules/svc/main.tf")
	if !ok || got != "api-mod" {
		t.Fatalf("resolve module arg var.service_name = %q, %v; want %q, true", got, ok, "api-mod")
	}

	if got, ok := ecsBindingResolveReference("var.service_name", ecsBindingTestContext(), "repo-infra", "modules/svc/main.tf"); ok {
		t.Fatalf("resolve var.service_name without call args = %q, true; want unknown", got)
	}
}

func TestECSBindingResolveTerragruntInputBridging(t *testing.T) {
	t.Parallel()

	// Two stacks calling the same module with different literal inputs
	// make the bridged variable ambiguous: fail closed.
	ctx := ecsBindingResolutionContext{
		Declarations: []ECSBindingDeclaration{
			{
				RepoID: "repo-infra", Path: "envs/prod/terragrunt.hcl",
				EntityType: "TerragruntConfig", EntityName: "terragrunt",
				Metadata: map[string]any{
					"terraform_source": "../../modules/svc",
				},
			},
			{
				RepoID: "repo-infra", Path: "envs/prod/terragrunt.hcl",
				EntityType: "TerragruntInput", EntityName: "service_name",
				Metadata: map[string]any{"name": "service_name", "value": `"api-tg"`},
			},
			{
				RepoID: "repo-infra", Path: "envs/staging/terragrunt.hcl",
				EntityType: "TerragruntConfig", EntityName: "terragrunt",
				Metadata: map[string]any{
					"terraform_source": "../../modules/svc",
				},
			},
			{
				RepoID: "repo-infra", Path: "envs/staging/terragrunt.hcl",
				EntityType: "TerragruntInput", EntityName: "service_name",
				Metadata: map[string]any{"name": "service_name", "value": `"api-staging"`},
			},
		},
	}
	if got, ok := ecsBindingResolveReference("var.service_name", ctx, "repo-infra", "modules/svc/main.tf"); ok {
		t.Fatalf("resolve var.service_name with two bridging stacks = %q, true; want unknown (ambiguous)", got)
	}
}

func TestECSBindingResolveTerragruntInputSingleStack(t *testing.T) {
	t.Parallel()

	ctx := ecsBindingResolutionContext{
		Declarations: []ECSBindingDeclaration{
			{
				RepoID: "repo-infra", Path: "envs/prod/terragrunt.hcl",
				EntityType: "TerragruntConfig", EntityName: "terragrunt",
				Metadata: map[string]any{
					"terraform_source": "../../modules/svc",
				},
			},
			{
				RepoID: "repo-infra", Path: "envs/prod/terragrunt.hcl",
				EntityType: "TerragruntInput", EntityName: "service_name",
				Metadata: map[string]any{"name": "service_name", "value": `"api-tg"`},
			},
		},
	}
	got, ok := ecsBindingResolveReference("var.service_name", ctx, "repo-infra", "modules/svc/main.tf")
	if !ok || got != "api-tg" {
		t.Fatalf("resolve var.service_name = %q, %v; want %q, true", got, ok, "api-tg")
	}
}

func TestECSBindingIsECRImageParity(t *testing.T) {
	t.Parallel()

	// Mirrors the query-side ecsECRHost gate: only canonical ECR hosts
	// ground digest corroboration. Adversarial hosts fail closed.
	for _, image := range []string{
		"111122223333.dkr.ecr.us-east-1.amazonaws.com/api:latest",
		"111122223333.dkr.ecr.us-gov-west-1.amazonaws.com/api@sha256:abcd",
	} {
		if !ecsBindingIsECRImage(image) {
			t.Errorf("ecsBindingIsECRImage(%q) = false, want true", image)
		}
	}
	// The China-partition host matches neither this gate nor the
	// query-side ecsECRHost twin (both anchor `.amazonaws.com`); that
	// shared limitation is out of this lane.
	for _, image := range []string{
		"",
		"api:latest",
		"docker.io/library/api:latest",
		"11112222333.dkr.ecr.us-east-1.amazonaws.com/api:latest",
		"111122223333.dkr.ecr.us-east-1.amazonaws.com.evil.com/api:latest",
		"111122223333.dkr.ecr./api:latest",
		"11112222333A.dkr.ecr.us-east-1.amazonaws.com/api:latest",
		"111122223333.dkr.ecr.cn-north-1.amazonaws.com.cn/api:1",
	} {
		if ecsBindingIsECRImage(image) {
			t.Errorf("ecsBindingIsECRImage(%q) = true, want false", image)
		}
	}
}

func TestECSBindingResolveTerragruntInputUnrelatedSource(t *testing.T) {
	t.Parallel()

	// A terragrunt stack calling a different module must not resolve
	// vars for declarations outside its target directory.
	ctx := ecsBindingResolutionContext{
		Declarations: []ECSBindingDeclaration{
			{
				RepoID: "repo-infra", Path: "envs/prod/terragrunt.hcl",
				EntityType: "TerragruntConfig", EntityName: "terragrunt",
				Metadata: map[string]any{
					"terraform_source": "../../modules/other",
				},
			},
			{
				RepoID: "repo-infra", Path: "envs/prod/terragrunt.hcl",
				EntityType: "TerragruntInput", EntityName: "service_name",
				Metadata: map[string]any{"name": "service_name", "value": `"api-tg"`},
			},
		},
	}
	if got, ok := ecsBindingResolveReference("var.service_name", ctx, "repo-infra", "modules/svc/main.tf"); ok {
		t.Fatalf("resolve var.service_name via unrelated stack = %q, true; want unknown", got)
	}
}
