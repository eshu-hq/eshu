// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opensearchserverless

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
)

const (
	testCollectionARN = "arn:aws:aoss:us-east-1:123456789012:collection/abc123"
	testKMSKeyARN     = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceOpenSearchServerless,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:opensearchserverless:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	snapshot Snapshot
	err      error
}

func (f fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return f.snapshot, f.err
}

func TestScannerEmitsCollectionsPoliciesEndpoints(t *testing.T) {
	created := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	snapshot := Snapshot{
		Collections: []Collection{{
			ARN:              testCollectionARN,
			ID:               "abc123",
			Name:             "orders",
			Type:             "SEARCH",
			Status:           "ACTIVE",
			StandbyReplicas:  "ENABLED",
			KMSKeyARN:        testKMSKeyARN,
			CreatedDate:      created,
			LastModifiedDate: created,
			Tags:             map[string]string{"Environment": "prod"},
		}},
		SecurityPolicies: []SecurityPolicy{{
			Name:          "orders-encryption",
			Type:          "encryption",
			PolicyVersion: "v1",
			Description:   "orders encryption policy",
			CreatedDate:   created,
		}},
		VPCEndpoints: []VPCEndpoint{{
			ID:               "vpce-aoss-123",
			Name:             "orders-endpoint",
			Status:           "ACTIVE",
			VPCID:            "vpc-0a1b2c3d",
			SubnetIDs:        []string{"subnet-1111", "subnet-2222"},
			SecurityGroupIDs: []string{"sg-aaaa"},
			CreatedDate:      created,
		}},
		EncryptionKeyBindings: []EncryptionKeyBinding{{
			PolicyName:         "orders-encryption",
			KMSKeyARN:          testKMSKeyARN,
			CollectionPatterns: []string{"orders"},
		}},
	}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	counts := map[string]int{}
	for _, envelope := range envelopes {
		if rt, ok := envelope.Payload["resource_type"].(string); ok {
			counts[rt]++
		}
		if relType, ok := envelope.Payload["relationship_type"].(string); ok {
			counts[relType]++
		}
	}
	wants := map[string]int{
		awscloud.ResourceTypeOpenSearchServerlessAOSSCollection:               1,
		awscloud.ResourceTypeOpenSearchServerlessSecurityPolicy:               1,
		awscloud.ResourceTypeOpenSearchServerlessAOSSVPCEndpoint:              1,
		awscloud.RelationshipOpenSearchServerlessCollectionUsesKMSKey:         1,
		awscloud.RelationshipOpenSearchServerlessVPCEndpointInVPC:             1,
		awscloud.RelationshipOpenSearchServerlessVPCEndpointInSubnet:          2,
		awscloud.RelationshipOpenSearchServerlessVPCEndpointUsesSecurityGroup: 1,
	}
	for key, want := range wants {
		if counts[key] != want {
			t.Errorf("count[%s] = %d, want %d", key, counts[key], want)
		}
	}
}

func TestScannerCollectionKMSUsesMostSpecificPolicy(t *testing.T) {
	specificKey := "arn:aws:kms:us-east-1:123456789012:key/specific"
	broadKey := "arn:aws:kms:us-east-1:123456789012:key/broad"
	snapshot := Snapshot{
		Collections: []Collection{{
			ARN:  "arn:aws:aoss:us-east-1:123456789012:collection/logspecial",
			ID:   "logspecial",
			Name: "logspecial",
		}},
		EncryptionKeyBindings: []EncryptionKeyBinding{
			{PolicyName: "broad", KMSKeyARN: broadKey, CollectionPatterns: []string{"log*"}},
			{PolicyName: "specific", KMSKeyARN: specificKey, CollectionPatterns: []string{"logspecial"}},
		},
	}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	var found bool
	for _, envelope := range envelopes {
		if envelope.Payload["relationship_type"] != awscloud.RelationshipOpenSearchServerlessCollectionUsesKMSKey {
			continue
		}
		found = true
		if got := envelope.Payload["target_resource_id"]; got != specificKey {
			t.Fatalf("collection KMS target = %v, want most-specific %q", got, specificKey)
		}
	}
	if !found {
		t.Fatal("no collection-to-KMS edge emitted")
	}
}

func TestScannerSkipsKMSEdgeForAWSOwnedKey(t *testing.T) {
	snapshot := Snapshot{
		Collections: []Collection{{
			ARN:  testCollectionARN,
			ID:   "abc123",
			Name: "orders",
		}},
		EncryptionKeyBindings: []EncryptionKeyBinding{{
			PolicyName:         "orders-encryption",
			KMSKeyARN:          "", // AWS-owned key.
			CollectionPatterns: []string{"orders"},
		}},
	}
	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	for _, envelope := range envelopes {
		if envelope.Payload["relationship_type"] == awscloud.RelationshipOpenSearchServerlessCollectionUsesKMSKey {
			t.Fatal("AWS-owned-key policy must not emit a collection-to-KMS edge")
		}
	}
}

func TestScannerEmptySnapshotReturnsNil(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("Scan() returned %d envelopes for empty account, want 0", len(envelopes))
	}
}

func TestScannerRejectsUnexpectedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = "not-opensearchserverless"
	if _, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary); err == nil {
		t.Fatal("Scan() error = nil, want error for unexpected service_kind")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	if _, err := (Scanner{}).Scan(context.Background(), testBoundary()); err == nil {
		t.Fatal("Scan() error = nil, want error for missing client")
	}
}

// TestEmittedRelationshipsSatisfyGraphJoinContract feeds every relationship this
// scanner builds through the shared relguard runtime helper, enforcing the #804
// graph-join contract: each target_type is a declared ResourceType and each
// target_resource_id is keyed the way the target scanner publishes it (KMS key
// ARN; bare vpc-/subnet-/sg- ids).
func TestEmittedRelationshipsSatisfyGraphJoinContract(t *testing.T) {
	boundary := testBoundary()
	collection := Collection{ARN: testCollectionARN, ID: "abc123", Name: "orders"}
	bindings := []EncryptionKeyBinding{{
		PolicyName:         "orders-encryption",
		KMSKeyARN:          testKMSKeyARN,
		CollectionPatterns: []string{"orders"},
	}}
	kms := collectionKMSRelationship(boundary, collection, bindings)
	if kms == nil {
		t.Fatal("collectionKMSRelationship returned nil for a matching encryption policy")
	}
	relguard.AssertObservations(t, *kms)

	endpoint := VPCEndpoint{
		ID:               "vpce-aoss-123",
		VPCID:            "vpc-0a1b2c3d",
		SubnetIDs:        []string{"subnet-1111"},
		SecurityGroupIDs: []string{"sg-aaaa"},
	}
	edges := vpcEndpointRelationships(boundary, endpoint)
	if len(edges) != 3 {
		t.Fatalf("vpcEndpointRelationships len = %d, want 3", len(edges))
	}
	relguard.AssertObservations(t, edges...)
}
