// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codebuild

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceCodeBuild,
		ScopeID:             "scope-1",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-aws-1",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, time.May, 28, 0, 0, 0, 0, time.UTC),
	}
}

func testKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("codebuild-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	return key
}

type fakeClient struct {
	projects     []Project
	reportGroups []ReportGroup
	builds       []Build
}

func (f fakeClient) ListProjects(context.Context) ([]Project, error) { return f.projects, nil }
func (f fakeClient) ListReportGroups(context.Context) ([]ReportGroup, error) {
	return f.reportGroups, nil
}
func (f fakeClient) ListRecentBuilds(context.Context) ([]Build, error) { return f.builds, nil }

func resourcesByType(t *testing.T, envelopes []facts.Envelope, resourceType string) []map[string]any {
	t.Helper()
	var matched []map[string]any
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if env.Payload["resource_type"] == resourceType {
			matched = append(matched, env.Payload)
		}
	}
	return matched
}

func relationshipsByType(t *testing.T, envelopes []facts.Envelope, relType string) []map[string]any {
	t.Helper()
	var matched []map[string]any
	for _, env := range envelopes {
		if env.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if env.Payload["relationship_type"] == relType {
			matched = append(matched, env.Payload)
		}
	}
	return matched
}

func sampleClient() fakeClient {
	return fakeClient{
		projects: []Project{{
			Name:             "checkout-build",
			ARN:              "arn:aws:codebuild:us-east-1:123456789012:project/checkout-build",
			Description:      "checkout service build",
			ServiceRoleARN:   "arn:aws:iam::123456789012:role/CodeBuildServiceRole",
			EncryptionKeyID:  "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
			TimeoutInMinutes: 60,
			Created:          time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC),
			Source: ProjectSource{
				Type:     "GITHUB",
				Location: "https://github.com/example/checkout.git",
			},
			SecondarySources: []ProjectSource{{
				Type:             "S3",
				Location:         "checkout-source-bucket/input.zip",
				SourceIdentifier: "extra",
			}},
			Environment: ProjectEnvironment{
				Type:        "LINUX_CONTAINER",
				Image:       "aws/codebuild/standard:7.0",
				ComputeType: "BUILD_GENERAL1_SMALL",
				EnvironmentVariables: []EnvironmentVariable{
					{Name: "PLAIN", Type: "PLAINTEXT", ValueMarker: map[string]any{"marker": "rdt:plain"}},
					{Name: "DB_SECRET", Type: "SECRETS_MANAGER", Reference: "arn:aws:secretsmanager:us-east-1:123456789012:secret:db-creds-AbCdEf"},
					{Name: "API_HOST", Type: "PARAMETER_STORE", Reference: "/checkout/api-host"},
				},
			},
			Artifacts: ProjectArtifacts{
				Type:     "S3",
				Location: "checkout-artifacts/builds",
			},
			VPCConfig: VPCConfig{
				VPCID:            "vpc-0abc",
				SubnetIDs:        []string{"subnet-01"},
				SecurityGroupIDs: []string{"sg-01"},
			},
			Tags: map[string]string{"Team": "payments"},
		}},
		reportGroups: []ReportGroup{{
			Name:           "checkout-reports",
			ARN:            "arn:aws:codebuild:us-east-1:123456789012:report-group/checkout-reports",
			Type:           "TEST",
			Status:         "ACTIVE",
			ExportType:     "S3",
			ExportS3Bucket: "checkout-reports-bucket",
			Created:        time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC),
		}},
		builds: []Build{{
			ID:            "checkout-build:abcd-1234",
			ARN:           "arn:aws:codebuild:us-east-1:123456789012:build/checkout-build:abcd-1234",
			ProjectName:   "checkout-build",
			BuildNumber:   42,
			Status:        "SUCCEEDED",
			CurrentPhase:  "COMPLETED",
			Initiator:     "user",
			BuildComplete: true,
			StartTime:     time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC),
			EndTime:       time.Date(2026, time.May, 1, 0, 5, 0, 0, time.UTC),
		}},
	}
}

func TestScannerEmitsProjectsReportGroupsAndBuilds(t *testing.T) {
	envelopes, err := Scanner{Client: sampleClient(), RedactionKey: testKey(t)}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	projects := resourcesByType(t, envelopes, awscloud.ResourceTypeCodeBuildProject)
	if len(projects) != 1 {
		t.Fatalf("project resources = %d, want 1", len(projects))
	}
	if got := projects[0]["name"]; got != "checkout-build" {
		t.Fatalf("project name = %v, want checkout-build", got)
	}
	attrs := projects[0]["attributes"].(map[string]any)
	if got := attrs["timeout_in_minutes"]; got != int32(60) {
		t.Fatalf("timeout_in_minutes = %v, want 60", got)
	}
	environment := attrs["environment"].(map[string]any)
	if environment["compute_type"] != "BUILD_GENERAL1_SMALL" {
		t.Fatalf("compute_type = %v, want BUILD_GENERAL1_SMALL", environment["compute_type"])
	}

	groups := resourcesByType(t, envelopes, awscloud.ResourceTypeCodeBuildReportGroup)
	if len(groups) != 1 {
		t.Fatalf("report-group resources = %d, want 1", len(groups))
	}
	groupAttrs := groups[0]["attributes"].(map[string]any)
	if groupAttrs["type"] != "TEST" {
		t.Fatalf("report-group type = %v, want TEST", groupAttrs["type"])
	}

	builds := resourcesByType(t, envelopes, awscloud.ResourceTypeCodeBuildBuild)
	if len(builds) != 1 {
		t.Fatalf("build resources = %d, want 1", len(builds))
	}
	buildAttrs := builds[0]["attributes"].(map[string]any)
	if buildAttrs["status"] != "SUCCEEDED" {
		t.Fatalf("build status = %v, want SUCCEEDED", buildAttrs["status"])
	}
	if buildAttrs["duration_seconds"] != int64(300) {
		t.Fatalf("build duration_seconds = %v, want 300", buildAttrs["duration_seconds"])
	}
}

func TestScannerEmitsProjectRelationshipsWithJoinKeys(t *testing.T) {
	envelopes, err := Scanner{Client: sampleClient(), RedactionKey: testKey(t)}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	role := relationshipsByType(t, envelopes, awscloud.RelationshipCodeBuildProjectUsesIAMRole)
	if len(role) != 1 {
		t.Fatalf("project->IAM-role relationships = %d, want 1", len(role))
	}
	if role[0]["target_type"] != awscloud.ResourceTypeIAMRole {
		t.Fatalf("project->IAM-role target_type = %v", role[0]["target_type"])
	}
	if role[0]["target_resource_id"] != "arn:aws:iam::123456789012:role/CodeBuildServiceRole" {
		t.Fatalf("project->IAM-role target_resource_id = %v", role[0]["target_resource_id"])
	}

	kms := relationshipsByType(t, envelopes, awscloud.RelationshipCodeBuildProjectUsesKMSKey)
	if len(kms) != 1 {
		t.Fatalf("project->KMS relationships = %d, want 1", len(kms))
	}
	if kms[0]["target_type"] != awscloud.ResourceTypeKMSKey {
		t.Fatalf("project->KMS target_type = %v", kms[0]["target_type"])
	}
	if kms[0]["target_resource_id"] != "arn:aws:kms:us-east-1:123456789012:key/abcd-1234" {
		t.Fatalf("project->KMS target_resource_id = %v", kms[0]["target_resource_id"])
	}

	vpc := relationshipsByType(t, envelopes, awscloud.RelationshipCodeBuildProjectUsesVPC)
	if len(vpc) != 1 {
		t.Fatalf("project->VPC relationships = %d, want 1", len(vpc))
	}
	if vpc[0]["target_type"] != awscloud.ResourceTypeEC2VPC {
		t.Fatalf("project->VPC target_type = %v", vpc[0]["target_type"])
	}
	if vpc[0]["target_resource_id"] != "vpc-0abc" {
		t.Fatalf("project->VPC target_resource_id = %v", vpc[0]["target_resource_id"])
	}

	subnet := relationshipsByType(t, envelopes, awscloud.RelationshipCodeBuildProjectUsesSubnet)
	if len(subnet) != 1 || subnet[0]["target_type"] != awscloud.ResourceTypeEC2Subnet {
		t.Fatalf("project->subnet relationships = %#v", subnet)
	}

	sg := relationshipsByType(t, envelopes, awscloud.RelationshipCodeBuildProjectUsesSecurityGroup)
	if len(sg) != 1 || sg[0]["target_type"] != awscloud.ResourceTypeEC2SecurityGroup {
		t.Fatalf("project->security-group relationships = %#v", sg)
	}

	repo := relationshipsByType(t, envelopes, awscloud.RelationshipCodeBuildProjectSourcedFromRepository)
	if len(repo) != 1 {
		t.Fatalf("project->repository relationships = %d, want 1", len(repo))
	}
	if repo[0]["target_resource_id"] != "https://github.com/example/checkout.git" {
		t.Fatalf("project->repository target_resource_id = %v", repo[0]["target_resource_id"])
	}
	if repo[0]["target_type"] != repositorySourceTargetType {
		t.Fatalf("project->repository target_type = %v, want %v", repo[0]["target_type"], repositorySourceTargetType)
	}

	s3Source := relationshipsByType(t, envelopes, awscloud.RelationshipCodeBuildProjectSourcedFromS3)
	if len(s3Source) != 1 {
		t.Fatalf("project->S3-source relationships = %d, want 1", len(s3Source))
	}
	const wantSourceBucketARN = "arn:aws:s3:::checkout-source-bucket"
	if s3Source[0]["target_resource_id"] != wantSourceBucketARN {
		t.Fatalf("project->S3-source target_resource_id = %v, want %v", s3Source[0]["target_resource_id"], wantSourceBucketARN)
	}
	if s3Source[0]["target_type"] != awscloud.ResourceTypeS3Bucket {
		t.Fatalf("project->S3-source target_type = %v", s3Source[0]["target_type"])
	}

	s3Artifact := relationshipsByType(t, envelopes, awscloud.RelationshipCodeBuildProjectArtifactsToS3)
	if len(s3Artifact) != 1 {
		t.Fatalf("project->S3-artifact relationships = %d, want 1", len(s3Artifact))
	}
	if s3Artifact[0]["target_resource_id"] != "arn:aws:s3:::checkout-artifacts" {
		t.Fatalf("project->S3-artifact target_resource_id = %v", s3Artifact[0]["target_resource_id"])
	}

	secret := relationshipsByType(t, envelopes, awscloud.RelationshipCodeBuildProjectReferencesSecret)
	if len(secret) != 1 {
		t.Fatalf("project->Secrets-Manager relationships = %d, want 1", len(secret))
	}
	if secret[0]["target_type"] != awscloud.ResourceTypeSecretsManagerSecret {
		t.Fatalf("project->Secrets-Manager target_type = %v", secret[0]["target_type"])
	}
	if secret[0]["target_resource_id"] != "arn:aws:secretsmanager:us-east-1:123456789012:secret:db-creds-AbCdEf" {
		t.Fatalf("project->Secrets-Manager target_resource_id = %v", secret[0]["target_resource_id"])
	}

	ssm := relationshipsByType(t, envelopes, awscloud.RelationshipCodeBuildProjectReferencesSSMParameter)
	if len(ssm) != 1 {
		t.Fatalf("project->SSM relationships = %d, want 1", len(ssm))
	}
	if ssm[0]["target_type"] != awscloud.ResourceTypeSSMParameter {
		t.Fatalf("project->SSM target_type = %v", ssm[0]["target_type"])
	}
	if ssm[0]["target_resource_id"] != "/checkout/api-host" {
		t.Fatalf("project->SSM target_resource_id = %v", ssm[0]["target_resource_id"])
	}
}

// TestScannerKeepsEnvVarNamesAndRedactsPlaintextValue confirms PLAINTEXT
// environment variables surface name+type plus a redaction marker, never a raw
// value, and that PARAMETER_STORE / SECRETS_MANAGER references are kept for
// relationship derivation.
func TestScannerKeepsEnvVarNamesAndRedactsPlaintextValue(t *testing.T) {
	envelopes, err := Scanner{Client: sampleClient(), RedactionKey: testKey(t)}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	projects := resourcesByType(t, envelopes, awscloud.ResourceTypeCodeBuildProject)
	if len(projects) != 1 {
		t.Fatalf("project resources = %d, want 1", len(projects))
	}
	attrs := projects[0]["attributes"].(map[string]any)
	environment := attrs["environment"].(map[string]any)
	variables := environment["environment_variables"].([]map[string]any)
	if len(variables) != 3 {
		t.Fatalf("environment_variables = %d, want 3", len(variables))
	}
	plain := variables[0]
	if plain["name"] != "PLAIN" || plain["type"] != "PLAINTEXT" {
		t.Fatalf("plaintext variable name/type = %#v", plain)
	}
	marker, ok := plain["value"].(map[string]any)
	if !ok || marker["marker"] != "rdt:plain" {
		t.Fatalf("plaintext variable value not a redaction marker: %#v", plain["value"])
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := Scanner{RedactionKey: testKey(t)}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func TestScannerRequiresRedactionKey(t *testing.T) {
	_, err := Scanner{Client: sampleClient()}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want redaction-key-required error")
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceIAM
	_, err := Scanner{Client: sampleClient(), RedactionKey: testKey(t)}.Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service-kind mismatch error")
	}
}

func TestScannerDefaultsServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = ""
	envelopes, err := Scanner{Client: sampleClient(), RedactionKey: testKey(t)}.Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, env := range envelopes {
		if env.Payload["service_kind"] != awscloud.ServiceCodeBuild {
			t.Fatalf("service_kind = %v, want codebuild", env.Payload["service_kind"])
		}
	}
}

// TestProjectSourceHasNoBuildspecField proves the scanner-owned ProjectSource
// struct has no field able to hold a buildspec body, so the scanner can never
// persist buildspec.yml content regardless of adapter behavior.
func TestProjectSourceHasNoBuildspecField(t *testing.T) {
	sourceType := reflect.TypeOf(ProjectSource{})
	for i := 0; i < sourceType.NumField(); i++ {
		name := strings.ToLower(sourceType.Field(i).Name)
		if strings.Contains(name, "buildspec") || strings.Contains(name, "spec") {
			t.Fatalf("ProjectSource exposes field %q able to hold buildspec content", sourceType.Field(i).Name)
		}
	}
}
