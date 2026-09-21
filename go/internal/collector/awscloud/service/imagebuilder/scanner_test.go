// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imagebuilder

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testAccountID    = "123456789012"
	testRegion       = "us-east-1"
	testPipelineARN  = "arn:aws:imagebuilder:us-east-1:123456789012:image-pipeline/web-pipeline"
	testImageRecipe  = "arn:aws:imagebuilder:us-east-1:123456789012:image-recipe/web/1.0.0"
	testContRecipe   = "arn:aws:imagebuilder:us-east-1:123456789012:container-recipe/api/2.0.0"
	testInfraConfig  = "arn:aws:imagebuilder:us-east-1:123456789012:infrastructure-configuration/builders"
	testDistConfig   = "arn:aws:imagebuilder:us-east-1:123456789012:distribution-configuration/multi-region"
	testExecRoleARN  = "arn:aws:iam::123456789012:role/imagebuilder-exec"
	testSNSTopicARN  = "arn:aws:sns:us-east-1:123456789012:imagebuilder-events"
	testKMSKeyARN    = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
	testInstanceProf = "ImageBuilderInstanceProfile"
	testWantProfARN  = "arn:aws:iam::123456789012:instance-profile/ImageBuilderInstanceProfile"
	testLogBucket    = "imagebuilder-logs"
	testWantLogARN   = "arn:aws:s3:::imagebuilder-logs"
	testECRRepoName  = "app-images"
	testWantECRARN   = "arn:aws:ecr:us-east-1:123456789012:repository/app-images"
)

func TestScannerEmitsPipelineResourceAndEdges(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Pipelines: []ImagePipeline{{
		ARN:                            testPipelineARN,
		Name:                           "web-pipeline",
		Status:                         "ENABLED",
		Platform:                       "Linux",
		ImageRecipeARN:                 testImageRecipe,
		InfrastructureConfigurationARN: testInfraConfig,
		DistributionConfigurationARN:   testDistConfig,
		ExecutionRoleARN:               testExecRoleARN,
		DateCreated:                    "2026-05-14T12:00:00Z",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	pipeline := resourceByType(t, envelopes, awscloud.ResourceTypeImageBuilderImagePipeline)
	if got, want := pipeline.Payload["resource_id"], testPipelineARN; got != want {
		t.Fatalf("pipeline resource_id = %#v, want %q", got, want)
	}
	if got, want := pipeline.Payload["state"], "ENABLED"; got != want {
		t.Fatalf("pipeline state = %#v, want %q", got, want)
	}

	recipeEdge := relationshipByType(t, envelopes, awscloud.RelationshipImageBuilderPipelineUsesImageRecipe)
	assertEdgeTarget(t, recipeEdge, awscloud.ResourceTypeImageBuilderImageRecipe, testImageRecipe)
	if got, want := recipeEdge.Payload["target_arn"], testImageRecipe; got != want {
		t.Fatalf("pipeline->image-recipe target_arn = %#v, want %q", got, want)
	}

	infraEdge := relationshipByType(t, envelopes, awscloud.RelationshipImageBuilderPipelineUsesInfrastructureConfiguration)
	assertEdgeTarget(t, infraEdge, awscloud.ResourceTypeImageBuilderInfrastructureConfiguration, testInfraConfig)

	distEdge := relationshipByType(t, envelopes, awscloud.RelationshipImageBuilderPipelineUsesDistributionConfiguration)
	assertEdgeTarget(t, distEdge, awscloud.ResourceTypeImageBuilderDistributionConfiguration, testDistConfig)

	roleEdge := relationshipByType(t, envelopes, awscloud.RelationshipImageBuilderPipelineUsesExecutionRole)
	assertEdgeTarget(t, roleEdge, awscloud.ResourceTypeIAMRole, testExecRoleARN)

	// A pipeline with an image recipe must not emit a container recipe edge.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if envelope.Payload["relationship_type"] == awscloud.RelationshipImageBuilderPipelineUsesContainerRecipe {
			t.Fatalf("unexpected container-recipe edge for image-recipe pipeline")
		}
	}
}

func TestScannerEmitsImageRecipeWithParentImageAttribute(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{ImageRecipes: []ImageRecipe{{
		ARN:           testImageRecipe,
		Name:          "web",
		Platform:      "Linux",
		Version:       "1.0.0",
		Owner:         "Self",
		ParentImage:   "ami-0123456789abcdef0",
		ComponentARNs: []string{"arn:aws:imagebuilder:us-east-1:aws:component/update-linux/1.0.0"},
		DateCreated:   "2026-05-14T12:00:00Z",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	recipe := resourceByType(t, envelopes, awscloud.ResourceTypeImageBuilderImageRecipe)
	attrs := attributesOf(t, recipe)
	assertAttribute(t, attrs, "parent_image", "ami-0123456789abcdef0")
	assertAttribute(t, attrs, "version", "1.0.0")
	assertAttribute(t, attrs, "component_count", 1)

	// No AMI edge is emitted (there is no EC2 AMI resource type).
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("image recipe must not emit relationship edges, got %#v", envelope.Payload)
		}
	}
}

func TestScannerEmitsContainerRecipeECRAndKMSEdges(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{ContainerRecipes: []ContainerRecipe{{
		ARN:                     testContRecipe,
		Name:                    "api",
		Platform:                "Linux",
		Version:                 "2.0.0",
		ContainerType:           "DOCKER",
		ParentImage:             "amazonlinux:latest",
		TargetRepositoryName:    testECRRepoName,
		TargetRepositoryService: "ECR",
		KMSKeyID:                testKMSKeyARN,
		Encrypted:               true,
		DateCreated:             "2026-05-14T12:00:00Z",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	recipe := resourceByType(t, envelopes, awscloud.ResourceTypeImageBuilderContainerRecipe)
	attrs := attributesOf(t, recipe)
	assertAttribute(t, attrs, "container_type", "DOCKER")
	if _, leaked := attrs["dockerfile_template_data"]; leaked {
		t.Fatalf("container recipe leaked Dockerfile template body")
	}

	ecrEdge := relationshipByType(t, envelopes, awscloud.RelationshipImageBuilderContainerRecipeUsesECRRepository)
	assertEdgeTarget(t, ecrEdge, awscloud.ResourceTypeECRRepository, testWantECRARN)
	if got, want := ecrEdge.Payload["target_arn"], testWantECRARN; got != want {
		t.Fatalf("container-recipe->ecr target_arn = %#v, want %q", got, want)
	}

	kmsEdge := relationshipByType(t, envelopes, awscloud.RelationshipImageBuilderContainerRecipeUsesKMSKey)
	assertEdgeTarget(t, kmsEdge, awscloud.ResourceTypeKMSKey, testKMSKeyARN)
	if got, want := kmsEdge.Payload["target_arn"], testKMSKeyARN; got != want {
		t.Fatalf("container-recipe->kms target_arn = %#v, want %q", got, want)
	}
}

func TestScannerOmitsKMSEdgeForNonARNKeyButKeepsValue(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{ContainerRecipes: []ContainerRecipe{{
		ARN:      testContRecipe,
		Name:     "api",
		KMSKeyID: "alias/imagebuilder",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	kmsEdge := relationshipByType(t, envelopes, awscloud.RelationshipImageBuilderContainerRecipeUsesKMSKey)
	if got, want := kmsEdge.Payload["target_resource_id"], "alias/imagebuilder"; got != want {
		t.Fatalf("kms target_resource_id = %#v, want %q", got, want)
	}
	if got := kmsEdge.Payload["target_arn"]; got != "" {
		t.Fatalf("kms target_arn = %#v, want empty for non-ARN key identifier", got)
	}
}

func TestScannerEmitsInfraConfigEdges(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{InfrastructureConfigurations: []InfrastructureConfiguration{{
		ARN:                 testInfraConfig,
		Name:                "builders",
		InstanceProfileName: testInstanceProf,
		InstanceTypes:       []string{"m5.large"},
		SubnetID:            "subnet-0abc123",
		SecurityGroupIDs:    []string{"sg-0aaa111", "sg-0bbb222"},
		SNSTopicARN:         testSNSTopicARN,
		LoggingS3BucketName: testLogBucket,
		LoggingS3KeyPrefix:  "builds/",
		DateCreated:         "2026-05-14T12:00:00Z",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	profEdge := relationshipByType(t, envelopes, awscloud.RelationshipImageBuilderInfraConfigUsesInstanceProfile)
	assertEdgeTarget(t, profEdge, awscloud.ResourceTypeIAMInstanceProfile, testWantProfARN)
	if got, want := profEdge.Payload["target_arn"], testWantProfARN; got != want {
		t.Fatalf("infra->instance-profile target_arn = %#v, want %q", got, want)
	}

	subnetEdge := relationshipByType(t, envelopes, awscloud.RelationshipImageBuilderInfraConfigUsesSubnet)
	assertEdgeTarget(t, subnetEdge, awscloud.ResourceTypeEC2Subnet, "subnet-0abc123")
	if got := subnetEdge.Payload["target_arn"]; got != "" {
		t.Fatalf("infra->subnet target_arn = %#v, want empty (bare id target)", got)
	}

	sgEdges := relationshipsByType(envelopes, awscloud.RelationshipImageBuilderInfraConfigUsesSecurityGroup)
	if len(sgEdges) != 2 {
		t.Fatalf("got %d security-group edges, want 2", len(sgEdges))
	}
	for _, edge := range sgEdges {
		if edge.Payload["target_type"] != awscloud.ResourceTypeEC2SecurityGroup {
			t.Fatalf("sg edge target_type = %#v", edge.Payload["target_type"])
		}
		if got := edge.Payload["target_arn"]; got != "" {
			t.Fatalf("infra->sg target_arn = %#v, want empty (bare id target)", got)
		}
	}

	snsEdge := relationshipByType(t, envelopes, awscloud.RelationshipImageBuilderInfraConfigUsesSNSTopic)
	assertEdgeTarget(t, snsEdge, awscloud.ResourceTypeSNSTopic, testSNSTopicARN)

	s3Edge := relationshipByType(t, envelopes, awscloud.RelationshipImageBuilderInfraConfigLogsToS3)
	assertEdgeTarget(t, s3Edge, awscloud.ResourceTypeS3Bucket, testWantLogARN)
	if got, want := s3Edge.Payload["target_arn"], testWantLogARN; got != want {
		t.Fatalf("infra->s3 target_arn = %#v, want %q", got, want)
	}
}

func TestScannerSynthesizesGovCloudARNs(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	client := fakeClient{snapshot: Snapshot{
		InfrastructureConfigurations: []InfrastructureConfiguration{{
			ARN:                 "arn:aws-us-gov:imagebuilder:us-gov-west-1:123456789012:infrastructure-configuration/gov",
			Name:                "gov",
			InstanceProfileName: testInstanceProf,
			LoggingS3BucketName: "gov-logs",
		}},
		ContainerRecipes: []ContainerRecipe{{
			ARN:                  "arn:aws-us-gov:imagebuilder:us-gov-west-1:123456789012:container-recipe/gov/1.0.0",
			Name:                 "gov",
			TargetRepositoryName: "gov-repo",
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	profEdge := relationshipByType(t, envelopes, awscloud.RelationshipImageBuilderInfraConfigUsesInstanceProfile)
	if got, want := profEdge.Payload["target_arn"], "arn:aws-us-gov:iam::123456789012:instance-profile/ImageBuilderInstanceProfile"; got != want {
		t.Fatalf("GovCloud instance-profile ARN = %#v, want %q", got, want)
	}
	s3Edge := relationshipByType(t, envelopes, awscloud.RelationshipImageBuilderInfraConfigLogsToS3)
	if got, want := s3Edge.Payload["target_arn"], "arn:aws-us-gov:s3:::gov-logs"; got != want {
		t.Fatalf("GovCloud S3 ARN = %#v, want %q", got, want)
	}
	ecrEdge := relationshipByType(t, envelopes, awscloud.RelationshipImageBuilderContainerRecipeUsesECRRepository)
	if got, want := ecrEdge.Payload["target_arn"], "arn:aws-us-gov:ecr:us-gov-west-1:123456789012:repository/gov-repo"; got != want {
		t.Fatalf("GovCloud ECR ARN = %#v, want %q", got, want)
	}
}

func TestScannerOmitsEdgesWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Pipelines:                    []ImagePipeline{{ARN: testPipelineARN, Name: "web"}},
		ImageRecipes:                 []ImageRecipe{{ARN: testImageRecipe, Name: "web"}},
		ContainerRecipes:             []ContainerRecipe{{ARN: testContRecipe, Name: "api"}},
		InfrastructureConfigurations: []InfrastructureConfiguration{{ARN: testInfraConfig, Name: "builders"}},
		DistributionConfigurations:   []DistributionConfiguration{{ARN: testDistConfig, Name: "multi"}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted with no dependencies: %#v", envelope.Payload)
		}
	}
	// All five resource types are present.
	for _, rt := range []string{
		awscloud.ResourceTypeImageBuilderImagePipeline,
		awscloud.ResourceTypeImageBuilderImageRecipe,
		awscloud.ResourceTypeImageBuilderContainerRecipe,
		awscloud.ResourceTypeImageBuilderInfrastructureConfiguration,
		awscloud.ResourceTypeImageBuilderDistributionConfiguration,
	} {
		_ = resourceByType(t, envelopes, rt)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	var observations []awscloud.RelationshipObservation
	observations = append(observations, pipelineRelationships(boundary, ImagePipeline{
		ARN:                            testPipelineARN,
		ContainerRecipeARN:             testContRecipe,
		InfrastructureConfigurationARN: testInfraConfig,
		DistributionConfigurationARN:   testDistConfig,
		ExecutionRoleARN:               testExecRoleARN,
	})...)
	observations = append(observations, infraConfigRelationships(boundary, InfrastructureConfiguration{
		ARN:                 testInfraConfig,
		InstanceProfileName: testInstanceProf,
		SubnetID:            "subnet-0abc123",
		SecurityGroupIDs:    []string{"sg-0aaa111"},
		SNSTopicARN:         testSNSTopicARN,
		LoggingS3BucketName: testLogBucket,
	})...)
	observations = append(observations, containerRecipeRelationships(boundary, ContainerRecipe{
		ARN:                     testContRecipe,
		TargetRepositoryName:    testECRRepoName,
		TargetRepositoryService: "ECR",
		KMSKeyID:                testKMSKeyARN,
	})...)
	if len(observations) == 0 {
		t.Fatalf("expected relationships for fully populated fixture")
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing-client error")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Pipelines: []ImagePipeline{{ARN: testPipelineARN, Name: "web"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Image Builder ListImageRecipes throttled after SDK retries; recipe metadata omitted for this scan",
			SourceRecordID: "imagebuilder_image_recipes_throttled",
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	warning := warningByKind(t, envelopes, awscloud.WarningThrottleSustained)
	if got := warning.Payload["error_class"]; got != "throttled" {
		t.Fatalf("warning error_class = %#v, want throttled", got)
	}
}
