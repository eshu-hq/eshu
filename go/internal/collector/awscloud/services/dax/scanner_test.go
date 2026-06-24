// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dax

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestScannerEmitsDAXMetadataOnlyFactsAndRelationships(t *testing.T) {
	clusterARN := "arn:aws:dax:us-east-1:123456789012:cache/orders-dax"
	roleARN := "arn:aws:iam::123456789012:role/orders-dax-dynamodb"

	client := fakeClient{
		clusters: []Cluster{{
			ARN:                        clusterARN,
			Name:                       "orders-dax",
			Description:                "orders dax accelerator",
			Status:                     "available",
			NodeType:                   "dax.r5.large",
			ActiveNodes:                3,
			TotalNodes:                 3,
			NetworkType:                "ipv4",
			EndpointEncryptionType:     "TLS",
			IAMRoleARN:                 roleARN,
			ParameterGroupName:         "default.dax1.0",
			SubnetGroupName:            "orders-dax-subnets",
			SecurityGroupIDs:           []string{"sg-aaa", "sg-bbb"},
			SSEStatus:                  "ENABLED",
			PreferredMaintenanceWindow: "sun:05:00-sun:06:00",
			DiscoveryEndpointAddress:   "orders-dax.abc123.dax-clusters.us-east-1.amazonaws.com",
			DiscoveryEndpointPort:      8111,
			Tags:                       map[string]string{"Environment": "prod"},
		}},
		subnetGroups: []SubnetGroup{{
			Name:        "orders-dax-subnets",
			Description: "orders dax subnets",
			VPCID:       "vpc-123",
			SubnetIDs:   []string{"subnet-a", "subnet-b"},
		}},
		parameterGroups: []ParameterGroup{{
			Name:        "default.dax1.0",
			Description: "default dax parameter group",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	cluster := resourceByType(t, envelopes, awscloud.ResourceTypeDAXCluster)
	if got, want := cluster.Payload["arn"], clusterARN; got != want {
		t.Fatalf("cluster arn = %#v, want %q", got, want)
	}
	if got, want := cluster.Payload["state"], "available"; got != want {
		t.Fatalf("cluster state = %#v, want %q", got, want)
	}
	clusterAttributes := attributesOf(t, cluster)
	assertAttribute(t, clusterAttributes, "node_type", "dax.r5.large")
	assertAttribute(t, clusterAttributes, "active_nodes", int32(3))
	assertAttribute(t, clusterAttributes, "total_nodes", int32(3))
	assertAttribute(t, clusterAttributes, "endpoint_encryption_type", "TLS")
	assertAttribute(t, clusterAttributes, "sse_status", "ENABLED")
	assertAttribute(t, clusterAttributes, "subnet_group_name", "orders-dax-subnets")
	assertAttribute(t, clusterAttributes, "security_group_ids", []string{"sg-aaa", "sg-bbb"})
	assertAttribute(t, clusterAttributes, "iam_role_arn", roleARN)
	// DAX exposes no server-side-encryption KMS key ARN, so no key id/ARN is
	// persisted and no KMS edge is emitted. Cached item data, query results, and
	// node endpoint payloads stay outside the contract.
	for _, forbidden := range []string{
		"kms_key_id",
		"kms_key_arn",
		"sse_kms_key_arn",
		"item_data",
		"cache_data",
		"query_results",
		"node_endpoints",
	} {
		if _, exists := clusterAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; DAX scanner must stay metadata-only", forbidden)
		}
	}

	subnetGroup := resourceByType(t, envelopes, awscloud.ResourceTypeDAXSubnetGroup)
	if got, want := subnetGroup.Payload["resource_id"], "orders-dax-subnets"; got != want {
		t.Fatalf("subnet group resource_id = %#v, want %q (keyed by name, no ARN)", got, want)
	}
	subnetGroupAttributes := attributesOf(t, subnetGroup)
	assertAttribute(t, subnetGroupAttributes, "vpc_id", "vpc-123")
	assertAttribute(t, subnetGroupAttributes, "subnet_ids", []string{"subnet-a", "subnet-b"})

	parameterGroup := resourceByType(t, envelopes, awscloud.ResourceTypeDAXParameterGroup)
	if got, want := parameterGroup.Payload["resource_id"], "default.dax1.0"; got != want {
		t.Fatalf("parameter group resource_id = %#v, want %q", got, want)
	}
	parameterGroupAttributes := attributesOf(t, parameterGroup)
	assertAttribute(t, parameterGroupAttributes, "description", "default dax parameter group")

	// Cluster edges: subnet group (by name), each security group (bare id), IAM role (ARN).
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipDAXClusterInSubnetGroup, "orders-dax-subnets")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipDAXClusterUsesSecurityGroup, "sg-aaa")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipDAXClusterUsesSecurityGroup, "sg-bbb")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipDAXClusterAssumesIAMRole, roleARN)
	// Subnet group edges: VPC (bare id) and each member subnet (bare id).
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipDAXSubnetGroupInVPC, "vpc-123")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipDAXSubnetGroupHasSubnet, "subnet-a")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipDAXSubnetGroupHasSubnet, "subnet-b")

	assertRelationshipSourceRecordID(t, envelopes, awscloud.RelationshipDAXClusterAssumesIAMRole, clusterARN+"->dax_cluster_assumes_iam_role:"+roleARN)
	assertRelationshipSourceRecordID(t, envelopes, awscloud.RelationshipDAXSubnetGroupHasSubnet, "orders-dax-subnets->dax_subnet_group_has_subnet:subnet-a")
}

func TestScannerSkipsRelationshipsWithoutTargets(t *testing.T) {
	client := fakeClient{clusters: []Cluster{{
		ARN:  "arn:aws:dax:us-east-1:123456789012:cache/orders",
		Name: "orders",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes); got != 0 {
		t.Fatalf("relationship count = %d, want 0 without direct target identity", got)
	}
}

func TestScannerEmitsSubnetEdgesFromSubnetGroupNotCluster(t *testing.T) {
	client := fakeClient{
		clusters: []Cluster{{
			ARN:             "arn:aws:dax:us-east-1:123456789012:cache/orders",
			Name:            "orders",
			SubnetGroupName: "orders-subnets",
		}},
		subnetGroups: []SubnetGroup{{
			Name:      "orders-subnets",
			VPCID:     "vpc-9",
			SubnetIDs: []string{"subnet-x"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	// The VPC and subnet edges originate from the subnet group, keyed by the
	// subnet group name (not the cluster).
	vpcEdge := relationshipByType(t, envelopes, awscloud.RelationshipDAXSubnetGroupInVPC)
	if got, want := vpcEdge.Payload["source_resource_id"], "orders-subnets"; got != want {
		t.Fatalf("vpc edge source = %#v, want %q (subnet group, not cluster)", got, want)
	}
	subnetEdge := relationshipByType(t, envelopes, awscloud.RelationshipDAXSubnetGroupHasSubnet)
	if got, want := subnetEdge.Payload["source_resource_id"], "orders-subnets"; got != want {
		t.Fatalf("subnet edge source = %#v, want %q (subnet group, not cluster)", got, want)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceRDS

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerDefaultsServiceKindWhenEmpty(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = ""

	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("envelopes = %d, want 0 for empty input", len(envelopes))
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{Client: nil}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}
