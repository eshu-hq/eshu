// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testImpactECSTaskARN    = "arn:aws:ecs:us-east-1:123456789012:task/demo/aaaaaaaa"
	testImpactLambdaARN     = "arn:aws:lambda:us-east-1:123456789012:function:demo"
	testImpactRunningImgRef = "123456789012.dkr.ecr.us-east-1.amazonaws.com/api:latest"
)

// awsResourceECSTaskRunningImageFact builds the aws_resource fact the AWS
// collector emits for one running ECS task carrying a single container image
// whose digest is `digest`. It mirrors the #5450 golden fixture shape
// (aws_resource_running_image_test.go) so #5452's join exercises the identical
// running-image decode the CloudResource node projection uses.
func awsResourceECSTaskRunningImageFact(factID, arn, digest string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.AWSResourceFactKind,
		Payload: map[string]any{
			"account_id":    "123456789012",
			"region":        "us-east-1",
			"resource_type": "aws_ecs_task",
			"resource_id":   arn,
			"attributes": map[string]any{
				"containers": []any{
					map[string]any{
						"image":        testImpactRunningImgRef,
						"image_digest": digest,
						"name":         "api",
					},
				},
			},
		},
	}
}

// awsResourceLambdaRunningImageFact builds the aws_resource fact the AWS
// collector emits for one image-package Lambda function whose resolved image
// digest is `digest`.
func awsResourceLambdaRunningImageFact(factID, arn, digest string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.AWSResourceFactKind,
		Payload: map[string]any{
			"account_id":    "123456789012",
			"region":        "us-east-1",
			"resource_type": "aws_lambda_function",
			"resource_id":   arn,
			"attributes": map[string]any{
				"package_type":       "Image",
				"image_uri":          testImpactRunningImgRef,
				"resolved_image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/api@" + digest,
			},
		},
	}
}

// supplyChainImpactRuntimeBaseFacts returns the vulnerability + image-identity
// fact set that resolves a finding for testImpactSubjectDigest, WITHOUT any
// cloud runtime observation. Each cloud-runtime test appends its own
// aws_resource fact so the delta under test is exactly the runtime observation.
func supplyChainImpactRuntimeBaseFacts(cve string) []facts.Envelope {
	return []facts.Envelope{
		vulnerabilityCVEFact("cve-1", cve, 9.1),
		vulnerabilityAffectedPackageFact("affected-1", cve, testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithChain("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3", []string{"api", "example"}, 2, false),
		sbomComponentImpactFact("component-1", "doc-1", testImpactPURL),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFactWithOutcome(
			"image-1",
			testImpactSubjectDigest,
			testImpactRepositoryID,
			"registry.example/api@"+testImpactSubjectDigest,
			string(ContainerImageIdentityExactDigest),
		),
	}
}

// TestBuildSupplyChainImpactFindingsAttachesECSRunningImageObservation is the
// #5452 non-vacuity proof: a finding whose subject digest matches the running
// image digest of an observed ECS task attaches that task as runtime-observed
// deployment evidence — naming which running resource carries the affected
// digest — distinct from CI-declared cicd_run_correlation evidence.
func TestBuildSupplyChainImpactFindingsAttachesECSRunningImageObservation(t *testing.T) {
	t.Parallel()

	factSet := append(supplyChainImpactRuntimeBaseFacts("CVE-2026-5452"),
		awsResourceECSTaskRunningImageFact("aws-ecs-1", testImpactECSTaskARN, testImpactSubjectDigest),
	)
	findings := BuildSupplyChainImpactFindings(factSet)

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5452"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	assertContainsString(t, got.CloudRuntimeResourceRefs, testImpactECSTaskARN)
	assertContainsString(t, got.EvidencePath, facts.AWSResourceFactKind)
	assertContainsString(t, got.EvidenceFactIDs, "aws-ecs-1")
}

// TestBuildSupplyChainImpactFindingsAttachesLambdaRunningImageObservation proves
// the same runtime-observed join fires for an image-package Lambda function.
func TestBuildSupplyChainImpactFindingsAttachesLambdaRunningImageObservation(t *testing.T) {
	t.Parallel()

	factSet := append(supplyChainImpactRuntimeBaseFacts("CVE-2026-5453"),
		awsResourceLambdaRunningImageFact("aws-lambda-1", testImpactLambdaARN, testImpactSubjectDigest),
	)
	findings := BuildSupplyChainImpactFindings(factSet)

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5453"]
	assertContainsString(t, got.CloudRuntimeResourceRefs, testImpactLambdaARN)
	assertContainsString(t, got.EvidencePath, facts.AWSResourceFactKind)
	assertContainsString(t, got.EvidenceFactIDs, "aws-lambda-1")
}

// TestBuildSupplyChainImpactFindingsIgnoresNonMatchingRunningImageDigest proves
// a running image digest that does NOT match the finding's subject digest is
// never fabricated onto the finding — no cross-digest false runtime evidence.
func TestBuildSupplyChainImpactFindingsIgnoresNonMatchingRunningImageDigest(t *testing.T) {
	t.Parallel()

	otherDigest := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	factSet := append(supplyChainImpactRuntimeBaseFacts("CVE-2026-5454"),
		awsResourceECSTaskRunningImageFact("aws-ecs-1", testImpactECSTaskARN, otherDigest),
	)
	findings := BuildSupplyChainImpactFindings(factSet)

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5454"]
	if len(got.CloudRuntimeResourceRefs) != 0 {
		t.Fatalf("CloudRuntimeResourceRefs = %#v, want none for a non-matching running image digest", got.CloudRuntimeResourceRefs)
	}
	assertNotContainsString(t, got.EvidencePath, facts.AWSResourceFactKind)
}

// TestBuildSupplyChainImpactFindingsIgnoresNonRunningImageResourceType proves a
// non-gated cloud resource_type (an ECS service, no single running image) never
// contributes runtime-observed evidence even when it shares the repository.
func TestBuildSupplyChainImpactFindingsIgnoresNonRunningImageResourceType(t *testing.T) {
	t.Parallel()

	ecsService := facts.Envelope{
		FactID:   "aws-svc-1",
		FactKind: facts.AWSResourceFactKind,
		Payload: map[string]any{
			"account_id":    "123456789012",
			"region":        "us-east-1",
			"resource_type": "ecs.service",
			"resource_id":   "arn:aws:ecs:us-east-1:123456789012:service/demo/demo",
			"attributes": map[string]any{
				"cluster_arn": "arn:aws:ecs:us-east-1:123456789012:cluster/demo",
			},
		},
	}
	factSet := append(supplyChainImpactRuntimeBaseFacts("CVE-2026-5455"), ecsService)
	findings := BuildSupplyChainImpactFindings(factSet)

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5455"]
	if len(got.CloudRuntimeResourceRefs) != 0 {
		t.Fatalf("CloudRuntimeResourceRefs = %#v, want none for a non-running-image resource_type", got.CloudRuntimeResourceRefs)
	}
}
