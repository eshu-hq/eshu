package reducer

import (
	"testing"
)

func TestRuntimeFamiliesReturnsEightFamilies(t *testing.T) {
	t.Parallel()

	families := RuntimeFamilies()
	if len(families) != 8 {
		t.Fatalf("RuntimeFamilies() len = %d, want 8", len(families))
	}
}

func TestRuntimeFamiliesKindsAreUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	for _, f := range RuntimeFamilies() {
		if _, ok := seen[f.Kind]; ok {
			t.Errorf("duplicate kind %q", f.Kind)
		}
		seen[f.Kind] = struct{}{}
	}
}

func TestLookupRuntimeFamilyFound(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"ecs", "eks", "lambda", "cloudflare_workers", "gke", "aks", "cloud_run", "container_apps"} {
		f := LookupRuntimeFamily(kind)
		if f == nil {
			t.Errorf("LookupRuntimeFamily(%q) = nil", kind)
		}
	}
}

func TestLookupRuntimeFamilyNormalizesCase(t *testing.T) {
	t.Parallel()

	f := LookupRuntimeFamily("  ECS  ")
	if f == nil || f.Kind != "ecs" {
		t.Fatal("LookupRuntimeFamily(ECS) should return ecs family")
	}
}

func TestLookupRuntimeFamilyNotFound(t *testing.T) {
	t.Parallel()

	if f := LookupRuntimeFamily("unknown"); f != nil {
		t.Errorf("LookupRuntimeFamily(unknown) = %v, want nil", f)
	}
}

func TestInferTerraformRuntimeFamilyKindByResourceType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		content string
		want    string
	}{
		{`resource "aws_ecs_cluster" "main" {}`, "ecs"},
		{`resource "aws_eks_cluster" "main" {}`, "eks"},
		{`resource "aws_lambda_function" "main" {}`, "lambda"},
		{`resource "cloudflare_workers_script" "main" {}`, "cloudflare_workers"},
		{`resource "google_container_cluster" "main" {}`, "gke"},
		{`resource "azurerm_kubernetes_cluster" "main" {}`, "aks"},
		{`resource "google_cloud_run_service" "main" {}`, "cloud_run"},
		{`resource "azurerm_container_app" "main" {}`, "container_apps"},
	}
	for _, tt := range tests {
		got := InferTerraformRuntimeFamilyKind(tt.content)
		if got != tt.want {
			t.Errorf("InferTerraformRuntimeFamilyKind(%q) = %q, want %q", tt.content, got, tt.want)
		}
	}
}

func TestInferTerraformRuntimeFamilyKindByModulePattern(t *testing.T) {
	t.Parallel()

	content := `module "eks" { source = "terraform-aws-modules/eks/aws" }`
	if got := InferTerraformRuntimeFamilyKind(content); got != "eks" {
		t.Errorf("got %q, want eks", got)
	}
}

func TestInferTerraformRuntimeFamilyKindNoMatch(t *testing.T) {
	t.Parallel()

	if got := InferTerraformRuntimeFamilyKind("resource aws_s3_bucket"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestInferRuntimeFamilyKindFromIdentifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		values []string
		want   string
	}{
		{[]string{"my-ecs-service"}, "ecs"},
		{[]string{"prod-eks-cluster"}, "eks"},
		{[]string{"serverless-api"}, "lambda"},
		{[]string{"cloudflare-edge"}, "cloudflare_workers"},
		{[]string{"nomatch"}, ""},
	}
	for _, tt := range tests {
		got := InferRuntimeFamilyKindFromIdentifiers(tt.values)
		if got != tt.want {
			t.Errorf("InferRuntimeFamilyKindFromIdentifiers(%v) = %q, want %q", tt.values, got, tt.want)
		}
	}
}

func TestInferInfrastructureRuntimeFamilyKind(t *testing.T) {
	t.Parallel()

	got := InferInfrastructureRuntimeFamilyKind(
		[]string{"aws_ecs_cluster"},
		[]string{},
	)
	if got != "ecs" {
		t.Errorf("got %q, want ecs", got)
	}
}

func TestInferInfrastructureRuntimeFamilyKindKeepsExplicitClusterWithServiceModules(t *testing.T) {
	t.Parallel()

	got := InferInfrastructureRuntimeFamilyKind(
		[]string{"aws_ecs_cluster"},
		[]string{"registry.example.com/platform/ecs-application/aws"},
	)
	if got != "ecs" {
		t.Errorf("got %q, want ecs for explicit cluster resource with service modules", got)
	}
}

func TestInferInfrastructureRuntimeFamilyKindSkipsNonCluster(t *testing.T) {
	t.Parallel()

	got := InferInfrastructureRuntimeFamilyKind(
		[]string{"aws_eks_cluster"},
		[]string{"iam-role-for-service-accounts-eks"},
	)
	if got != "" {
		t.Errorf("got %q, want empty (non-cluster exclusion)", got)
	}
}

func TestInferInfrastructureRuntimeFamilyKindByModuleSource(t *testing.T) {
	t.Parallel()

	got := InferInfrastructureRuntimeFamilyKind(
		[]string{},
		[]string{"terraform-google-modules/kubernetes-engine"},
	)
	if got != "gke" {
		t.Errorf("got %q, want gke", got)
	}
}

func TestMatchesServiceModuleSourceTrue(t *testing.T) {
	t.Parallel()

	if !MatchesServiceModuleSource("ecs-application/aws", "ecs") {
		t.Error("expected true for ecs-application/aws matching ecs")
	}
}

func TestMatchesServiceModuleSourceFalse(t *testing.T) {
	t.Parallel()

	if MatchesServiceModuleSource("random-module", "ecs") {
		t.Error("expected false for random-module matching ecs")
	}
}

func TestMatchesServiceModuleSourceUnknownKind(t *testing.T) {
	t.Parallel()

	if MatchesServiceModuleSource("anything", "unknown") {
		t.Error("expected false for unknown kind")
	}
}

func TestTerraformPlatformEvidenceKind(t *testing.T) {
	t.Parallel()

	got := TerraformPlatformEvidenceKind("ecs", "cluster")
	if got != "TERRAFORM_ECS_CLUSTER" {
		t.Errorf("got %q, want TERRAFORM_ECS_CLUSTER", got)
	}
}

func TestFormatPlatformKindLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind string
		want string
	}{
		{"ecs", "ECS"},
		{"eks", "EKS"},
		{"kubernetes", "Kubernetes"},
		{"lambda", "Lambda"},
		{"cloudflare_workers", "Cloudflare Workers"},
		{"unknown_platform", "UNKNOWN_PLATFORM"},
		{"", ""},
	}
	for _, tt := range tests {
		got := FormatPlatformKindLabel(tt.kind)
		if got != tt.want {
			t.Errorf("FormatPlatformKindLabel(%q) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}
