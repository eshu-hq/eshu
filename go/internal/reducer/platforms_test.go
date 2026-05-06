package reducer

import (
	"testing"
)

func TestInferRuntimePlatformKindKubernetes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kinds []string
		want  string
	}{
		{[]string{"deployment"}, "kubernetes"},
		{[]string{"service"}, "kubernetes"},
		{[]string{"statefulset"}, "kubernetes"},
		{[]string{"daemonset"}, "kubernetes"},
		{[]string{"Deployment"}, "kubernetes"},
	}
	for _, tt := range tests {
		got := InferRuntimePlatformKind(tt.kinds)
		if got != tt.want {
			t.Errorf("InferRuntimePlatformKind(%v) = %q, want %q", tt.kinds, got, tt.want)
		}
	}
}

func TestInferRuntimePlatformKindEmpty(t *testing.T) {
	t.Parallel()

	if got := InferRuntimePlatformKind(nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := InferRuntimePlatformKind([]string{"configmap"}); got != "" {
		t.Errorf("got %q for configmap, want empty", got)
	}
}

func TestCanonicalPlatformIDFullSegments(t *testing.T) {
	t.Parallel()

	got := CanonicalPlatformID(CanonicalPlatformInput{
		Kind:        "kubernetes",
		Provider:    "aws",
		Name:        "prod-cluster",
		Environment: "production",
		Region:      "us-east-1",
	})
	want := "platform:kubernetes:aws:prod-cluster:production:us-east-1"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCanonicalPlatformIDWithLocator(t *testing.T) {
	t.Parallel()

	got := CanonicalPlatformID(CanonicalPlatformInput{
		Kind:     "ecs",
		Provider: "aws",
		Name:     "my-cluster",
		Locator:  "cluster/my-cluster",
	})
	want := "platform:ecs:aws:cluster/my-cluster:none:none"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCanonicalPlatformIDMinimalWithEnvironmentAndRegion(t *testing.T) {
	t.Parallel()

	got := CanonicalPlatformID(CanonicalPlatformInput{
		Kind:        "kubernetes",
		Environment: "staging",
		Region:      "eu-west-1",
	})
	want := "platform:kubernetes:none:none:staging:eu-west-1"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCanonicalPlatformIDReturnsEmptyWhenMissingDiscriminator(t *testing.T) {
	t.Parallel()

	got := CanonicalPlatformID(CanonicalPlatformInput{
		Kind: "kubernetes",
	})
	if got != "" {
		t.Errorf("got %q, want empty (no discriminator, no env+region)", got)
	}
}

func TestCanonicalPlatformIDNormalizesWhitespace(t *testing.T) {
	t.Parallel()

	got := CanonicalPlatformID(CanonicalPlatformInput{
		Kind:        "  Kubernetes  ",
		Name:        " Prod ",
		Environment: " production ",
	})
	want := "platform:kubernetes:none:prod:production:none"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInferInfrastructurePlatformDescriptor(t *testing.T) {
	t.Parallel()

	d := InferInfrastructurePlatformDescriptor(InfrastructurePlatformInput{
		ResourceTypes: []string{"aws_ecs_cluster"},
		ResourceNames: []string{"payments-cluster"},
		RepoName:      "infra-ecs",
	})
	if d == nil {
		t.Fatal("expected non-nil descriptor")
	}
	if d.PlatformKind != "ecs" {
		t.Errorf("PlatformKind = %q", d.PlatformKind)
	}
	if d.PlatformProvider != "aws" {
		t.Errorf("PlatformProvider = %q", d.PlatformProvider)
	}
	if d.PlatformName != "payments-cluster" {
		t.Errorf("PlatformName = %q", d.PlatformName)
	}
}

func TestInferInfrastructurePlatformDescriptorNoMatch(t *testing.T) {
	t.Parallel()

	d := InferInfrastructurePlatformDescriptor(InfrastructurePlatformInput{
		ResourceTypes: []string{"aws_s3_bucket"},
		RepoName:      "my-repo",
	})
	if d != nil {
		t.Errorf("expected nil, got %+v", d)
	}
}

func TestInferInfrastructurePlatformDescriptorFallsBackToRepoName(t *testing.T) {
	t.Parallel()

	d := InferInfrastructurePlatformDescriptor(InfrastructurePlatformInput{
		ResourceTypes: []string{"aws_ecs_cluster"},
		RepoName:      "infra-platform",
	})
	if d == nil {
		t.Fatal("expected non-nil")
	}
	if d.PlatformName != "infra-platform" {
		t.Errorf("PlatformName = %q, want infra-platform", d.PlatformName)
	}
}

func TestInferInfrastructurePlatformDescriptorSkipsNonPlatformNames(t *testing.T) {
	t.Parallel()

	d := InferInfrastructurePlatformDescriptor(InfrastructurePlatformInput{
		ResourceTypes: []string{"aws_ecs_cluster"},
		ResourceNames: []string{"default", "main"},
		RepoName:      "my-infra",
	})
	if d == nil {
		t.Fatal("expected non-nil")
	}
	if d.PlatformName != "my-infra" {
		t.Errorf("PlatformName = %q, want my-infra", d.PlatformName)
	}
}

func TestInferInfrastructurePlatformDescriptorAWSProviderFallback(t *testing.T) {
	t.Parallel()

	d := InferInfrastructurePlatformDescriptor(InfrastructurePlatformInput{
		ResourceTypes: []string{"aws_lambda_function"},
		DataTypes:     []string{"aws_iam_policy"},
		ResourceNames: []string{"my-function"},
		RepoName:      "lambda-infra",
	})
	if d == nil {
		t.Fatal("expected non-nil")
	}
	if d.PlatformProvider != "aws" {
		t.Errorf("PlatformProvider = %q, want aws", d.PlatformProvider)
	}
}
