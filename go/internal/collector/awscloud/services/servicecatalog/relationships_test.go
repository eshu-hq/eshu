// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
)

// TestEmittedRelationshipsSatisfyGraphJoinContract feeds every relationship the
// Service Catalog scanner builds through the shared relguard runtime helper,
// asserting each edge carries a declared target_type and a join-mode-consistent
// ARN-keyed target.
func TestEmittedRelationshipsSatisfyGraphJoinContract(t *testing.T) {
	boundary := boundaryFor(awscloud.ServiceServiceCatalog)

	stack := provisionedProductStackRelationship(boundary, ProvisionedProduct{
		ID:         "pp-stack001",
		ARN:        "arn:aws:servicecatalog:us-east-1:123456789012:stack/team/pp-stack001",
		Type:       "CFN_STACK",
		PhysicalID: "arn:aws:cloudformation:us-east-1:123456789012:stack/SC-team/abcd-1234",
	})
	if stack == nil {
		t.Fatal("provisionedProductStackRelationship returned nil for a CFN_STACK with a stack ARN")
	}

	product := productInPortfolioRelationships(
		boundary,
		Product{ID: "prod-xyz789", ARN: "arn:aws:catalog:us-east-1:123456789012:product/prod-xyz789"},
		[]Portfolio{{ID: "port-abc123", ARN: "arn:aws:catalog:us-east-1:123456789012:portfolio/port-abc123"}},
	)
	if len(product) == 0 {
		t.Fatal("productInPortfolioRelationships returned no edges for an associated portfolio")
	}

	portfolio := portfolioPrincipalRelationships(
		boundary,
		Portfolio{ID: "port-abc123", ARN: "arn:aws:catalog:us-east-1:123456789012:portfolio/port-abc123"},
		[]Principal{{ARN: "arn:aws:iam::123456789012:role/LaunchRole", Type: "IAM"}},
	)
	if len(portfolio) == 0 {
		t.Fatal("portfolioPrincipalRelationships returned no edges for an IAM role principal")
	}

	all := append([]awscloud.RelationshipObservation{*stack}, product...)
	all = append(all, portfolio...)
	relguard.AssertObservations(t, all...)
}

// TestProvisionedProductStackEdgePreservesARNPartition pins the GovCloud/China
// graph-join contract. The provisioned-product-to-stack edge keys on the
// CloudFormation stack ARN reported by AWS, so the target ARN must carry the
// real partition (aws-us-gov, aws-cn) verbatim. The CloudFormation scanner
// publishes a stack node's resource_id as the stack ARN, so a partition-altered
// edge would dangle in non-commercial partitions.
func TestProvisionedProductStackEdgePreservesARNPartition(t *testing.T) {
	cases := []struct {
		name     string
		region   string
		stackARN string
	}{
		{
			name:     "commercial",
			region:   "us-east-1",
			stackARN: "arn:aws:cloudformation:us-east-1:123456789012:stack/SC-team/abcd-1234",
		},
		{
			name:     "govcloud",
			region:   "us-gov-west-1",
			stackARN: "arn:aws-us-gov:cloudformation:us-gov-west-1:123456789012:stack/SC-team/abcd-1234",
		},
		{
			name:     "china",
			region:   "cn-north-1",
			stackARN: "arn:aws-cn:cloudformation:cn-north-1:123456789012:stack/SC-team/abcd-1234",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := awscloud.Boundary{Region: tc.region}
			obs := provisionedProductStackRelationship(boundary, ProvisionedProduct{
				ID:         "pp-stack001",
				ARN:        "arn:" + partitionOf(tc.stackARN) + ":servicecatalog:" + tc.region + ":123456789012:stack/team/pp-stack001",
				Type:       "CFN_STACK",
				PhysicalID: tc.stackARN,
			})
			if obs == nil {
				t.Fatalf("provisionedProductStackRelationship returned nil for %s stack ARN", tc.name)
			}
			if obs.TargetResourceID != tc.stackARN {
				t.Fatalf("target_resource_id = %q, want %q", obs.TargetResourceID, tc.stackARN)
			}
			if obs.TargetARN != tc.stackARN {
				t.Fatalf("target_arn = %q, want %q", obs.TargetARN, tc.stackARN)
			}
			if obs.TargetType != awscloud.ResourceTypeCloudFormationStack {
				t.Fatalf("target_type = %q, want %q", obs.TargetType, awscloud.ResourceTypeCloudFormationStack)
			}
		})
	}
}

// TestPortfolioPrincipalEdgeRejectsNonRoleARNs confirms the helper only emits
// IAM role edges. The substring guard must parse the ARN service segment
// exactly so an identifier that merely contains a service-looking substring
// cannot be misclassified.
func TestPortfolioPrincipalEdgeRejectsNonRoleARNs(t *testing.T) {
	boundary := boundaryFor(awscloud.ServiceServiceCatalog)
	portfolio := Portfolio{ID: "port-abc123", ARN: "arn:aws:catalog:us-east-1:123456789012:portfolio/port-abc123"}
	rejected := []Principal{
		{ARN: "arn:aws:iam::123456789012:user/alice", Type: "IAM"},
		{ARN: "arn:aws:iam::123456789012:group/admins", Type: "IAM"},
		{ARN: "arn:aws:iam:::role/*", Type: "IAM_PATTERN"},
		{ARN: "arn:aws:sts::123456789012:assumed-role/iam/session", Type: "IAM"},
		{ARN: "", Type: "IAM"},
	}
	if edges := portfolioPrincipalRelationships(boundary, portfolio, rejected); len(edges) != 0 {
		t.Fatalf("portfolioPrincipalRelationships emitted %d edges for non-role principals, want 0", len(edges))
	}
}

func partitionOf(arn string) string {
	return awscloud.PartitionFromARN(arn)
}
