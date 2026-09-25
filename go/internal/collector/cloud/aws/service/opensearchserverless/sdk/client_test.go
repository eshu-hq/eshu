// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/opensearchserverless/document"
	awsaosstypes "github.com/aws/aws-sdk-go-v2/service/opensearchserverless/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: aws.ServiceOpenSearchServerless,
	}
}

func TestClientSnapshotsMetadataAndParsesEncryptionKey(t *testing.T) {
	collectionARN := "arn:aws:aoss:us-east-1:123456789012:collection/abc123"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/1234abcd"

	api := &fakeAPI{
		collectionSummaries: []awsaosstypes.CollectionSummary{{
			Id:   awsv2.String("abc123"),
			Name: awsv2.String("orders"),
		}},
		collectionDetails: map[string]awsaosstypes.CollectionDetail{
			"abc123": {
				Arn:             awsv2.String(collectionARN),
				Id:              awsv2.String("abc123"),
				Name:            awsv2.String("orders"),
				Type:            awsaosstypes.CollectionTypeSearch,
				Status:          awsaosstypes.CollectionStatusActive,
				StandbyReplicas: awsaosstypes.StandbyReplicasEnabled,
				KmsKeyArn:       awsv2.String(kmsARN),
				CreatedDate:     awsv2.Int64(1747224000000),
			},
		},
		encryptionPolicies: []awsaosstypes.SecurityPolicySummary{{
			Name:        awsv2.String("orders-encryption"),
			Description: awsv2.String("orders policy"),
		}},
		encryptionDetail: map[string]awsaosstypes.SecurityPolicyDetail{
			"orders-encryption": {
				Name: awsv2.String("orders-encryption"),
				Type: awsaosstypes.SecurityPolicyTypeEncryption,
				Policy: document.NewLazyDocument(map[string]any{
					"Rules": []any{map[string]any{
						"ResourceType": "collection",
						"Resource":     []any{"collection/orders", "collection/order*"},
					}},
					"AWSOwnedKey": false,
					"KmsARN":      kmsARN,
				}),
			},
		},
		vpcEndpointSummaries: []awsaosstypes.VpcEndpointSummary{{
			Id:   awsv2.String("vpce-aoss-123"),
			Name: awsv2.String("orders-endpoint"),
		}},
		vpcEndpointDetails: map[string]awsaosstypes.VpcEndpointDetail{
			"vpce-aoss-123": {
				Id:               awsv2.String("vpce-aoss-123"),
				Name:             awsv2.String("orders-endpoint"),
				Status:           awsaosstypes.VpcEndpointStatusActive,
				VpcId:            awsv2.String("vpc-0a1b2c3d"),
				SubnetIds:        []string{"subnet-1111", "subnet-2222"},
				SecurityGroupIds: []string{"sg-aaaa"},
			},
		},
		tags: map[string][]awsaosstypes.Tag{
			collectionARN: {{Key: awsv2.String("Environment"), Value: awsv2.String("prod")}},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if len(snapshot.Collections) != 1 {
		t.Fatalf("len(Collections) = %d, want 1", len(snapshot.Collections))
	}
	collection := snapshot.Collections[0]
	if collection.ARN != collectionARN {
		t.Fatalf("collection ARN = %q, want %q", collection.ARN, collectionARN)
	}
	if collection.Type != "SEARCH" || collection.Status != "ACTIVE" {
		t.Fatalf("collection type/status = %q/%q, want SEARCH/ACTIVE", collection.Type, collection.Status)
	}
	if collection.StandbyReplicas != "ENABLED" {
		t.Fatalf("standby replicas = %q, want ENABLED", collection.StandbyReplicas)
	}
	if collection.CreatedDate.IsZero() {
		t.Fatal("collection CreatedDate is zero; epoch-millis conversion failed")
	}
	if collection.Tags["Environment"] != "prod" {
		t.Fatalf("collection tag Environment = %q, want prod", collection.Tags["Environment"])
	}

	if len(snapshot.SecurityPolicies) != 1 {
		t.Fatalf("len(SecurityPolicies) = %d, want 1", len(snapshot.SecurityPolicies))
	}
	if snapshot.SecurityPolicies[0].Type != "encryption" {
		t.Fatalf("policy type = %q, want encryption", snapshot.SecurityPolicies[0].Type)
	}

	if len(snapshot.EncryptionKeyBindings) != 1 {
		t.Fatalf("len(EncryptionKeyBindings) = %d, want 1", len(snapshot.EncryptionKeyBindings))
	}
	binding := snapshot.EncryptionKeyBindings[0]
	if binding.KMSKeyARN != kmsARN {
		t.Fatalf("binding KMSKeyARN = %q, want %q", binding.KMSKeyARN, kmsARN)
	}
	if len(binding.CollectionPatterns) != 2 {
		t.Fatalf("binding patterns = %#v, want [orders order*]", binding.CollectionPatterns)
	}

	if len(snapshot.VPCEndpoints) != 1 {
		t.Fatalf("len(VPCEndpoints) = %d, want 1", len(snapshot.VPCEndpoints))
	}
	endpoint := snapshot.VPCEndpoints[0]
	if endpoint.VPCID != "vpc-0a1b2c3d" {
		t.Fatalf("endpoint VPCID = %q, want vpc-0a1b2c3d", endpoint.VPCID)
	}
	if len(endpoint.SubnetIDs) != 2 || len(endpoint.SecurityGroupIDs) != 1 {
		t.Fatalf("endpoint subnets/sgs = %#v/%#v", endpoint.SubnetIDs, endpoint.SecurityGroupIDs)
	}
}

func TestClientSnapshotEmptyAccount(t *testing.T) {
	client := &Client{client: &fakeAPI{}, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Collections) != 0 || len(snapshot.SecurityPolicies) != 0 || len(snapshot.VPCEndpoints) != 0 {
		t.Fatalf("empty account produced non-empty snapshot: %#v", snapshot)
	}
}

func TestClientSkipsAWSOwnedKeyBinding(t *testing.T) {
	api := &fakeAPI{
		encryptionPolicies: []awsaosstypes.SecurityPolicySummary{{Name: awsv2.String("owned")}},
		encryptionDetail: map[string]awsaosstypes.SecurityPolicyDetail{
			"owned": {
				Name: awsv2.String("owned"),
				Type: awsaosstypes.SecurityPolicyTypeEncryption,
				Policy: document.NewLazyDocument(map[string]any{
					"Rules": []any{map[string]any{
						"ResourceType": "collection",
						"Resource":     []any{"collection/orders"},
					}},
					"AWSOwnedKey": true,
				}),
			},
		},
	}
	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.SecurityPolicies) != 1 {
		t.Fatalf("len(SecurityPolicies) = %d, want 1", len(snapshot.SecurityPolicies))
	}
	if len(snapshot.EncryptionKeyBindings) != 0 {
		t.Fatalf("AWS-owned-key policy produced %d bindings, want 0", len(snapshot.EncryptionKeyBindings))
	}
}
